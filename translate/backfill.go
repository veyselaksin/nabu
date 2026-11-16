package translate

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/veyselaksin/nabu/data/corpus"
)

// English is a machine translation of a corpus sentence's German
// translation, one line of en.jsonl.
type English struct {
	ID      string `json:"id"`
	English string `json:"en"`
	Model   string `json:"model"`
	Prompt  string `json:"prompt"`
	// MachineTranslated is always true: these are not the editors' words.
	MachineTranslated bool `json:"machine_translated"`
}

// backfillPromptVersion changes whenever backfillPrompt does.
const backfillPromptVersion = "de-en-v1"

const backfillPrompt = `You translate German translations of Ancient Egyptian texts into English.
Translate each numbered German sentence faithfully into natural English. Keep Egyptian names and
terms as they are. Keep the editors' brackets and ellipses: [...] for restorations, (...) for
additions, "..." for gaps. Do not explain or add anything.
Reply with a JSON object mapping each number to its English translation: {"1": "...", "2": "..."}.`

// BackfillOptions configures BackfillEnglish.
type BackfillOptions struct {
	Model    string
	Batch    int // sentences per request (default 20)
	Limit    int // stop after this many new translations (0: all)
	Progress io.Writer
}

// StageOrder sorts sentences for the backfill: Middle Egyptian first (what
// the translator uses), then undated, Late Egyptian and Demotic.
func StageOrder(sents []corpus.Sentence) []corpus.Sentence {
	rank := map[string]int{"earlier": 0, "": 1, "late": 2, "demotic": 3}
	out := append([]corpus.Sentence(nil), sents...)
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Stage] < rank[out[j].Stage] })
	return out
}

// ReadEnglish reads en.jsonl; a missing file is empty. A truncated last line
// (an interrupted write) is ignored.
func ReadEnglish(path string) (map[string]English, error) {
	out := map[string]English{}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var e English
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.ID != "" && e.English != "" {
			out[e.ID] = e
		}
	}
	return out, sc.Err()
}

// BackfillEnglish translates the German translation of every sentence that
// path does not have yet and appends the results to path. It can be
// stopped and rerun at any time. Sentences the model skips in a batch are
// retried one by one; one that still fails is left for the next run.
func BackfillEnglish(ctx context.Context, llm Chatter, sents []corpus.Sentence, path string, opts BackfillOptions) (int, error) {
	if opts.Batch <= 0 {
		opts.Batch = 20
	}
	done, err := ReadEnglish(path)
	if err != nil {
		return 0, err
	}
	var todo []corpus.Sentence
	seen := map[string]bool{}
	for _, s := range sents {
		if s.German != "" && done[s.ID].ID == "" && !seen[s.ID] {
			seen[s.ID] = true
			todo = append(todo, s)
		}
	}
	if opts.Limit > 0 && len(todo) > opts.Limit {
		todo = todo[:opts.Limit]
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	// Start on a fresh line if an interrupted run left a partial one.
	if fi, err := f.Stat(); err == nil && fi.Size() > 0 {
		if last := lastByte(path); last != '\n' {
			if _, err := f.WriteString("\n"); err != nil {
				return 0, err
			}
		}
	}

	written, failed := 0, 0
	start := time.Now()
	write := func(s corpus.Sentence, en string) error {
		line, _ := json.Marshal(English{ID: s.ID, English: en, Model: opts.Model, Prompt: backfillPromptVersion, MachineTranslated: true})
		if _, err := f.Write(append(line, '\n')); err != nil {
			return err
		}
		written++
		return nil
	}
	for i := 0; i < len(todo); i += opts.Batch {
		batch := todo[i:min(i+opts.Batch, len(todo))]
		got, err := translateBatch(ctx, llm, batch)
		if err != nil && !errors.Is(err, ErrModelOutput) {
			return written, err
		}
		for j, s := range batch {
			en := got[j]
			if en == "" && len(batch) > 1 {
				one, err := translateBatch(ctx, llm, batch[j:j+1])
				if err != nil && !errors.Is(err, ErrModelOutput) {
					return written, err
				}
				en = one[0]
			}
			if en == "" {
				failed++
				continue
			}
			if err := write(s, en); err != nil {
				return written, err
			}
		}
		if opts.Progress != nil {
			n := i + len(batch)
			rate := float64(n) / time.Since(start).Seconds()
			eta := time.Duration(float64(len(todo)-n)/rate) * time.Second
			fmt.Fprintf(opts.Progress, "\rtranslated %d/%d (%.1f/s, %d failed, %s left)   ", n, len(todo), rate, failed, eta.Round(time.Second))
		}
	}
	if opts.Progress != nil {
		fmt.Fprintln(opts.Progress)
	}
	return written, nil
}

// translateBatch returns one English sentence per input ("" where the
// reply has none).
func translateBatch(ctx context.Context, llm Chatter, batch []corpus.Sentence) ([]string, error) {
	var user strings.Builder
	for i, s := range batch {
		fmt.Fprintf(&user, "%d. %s\n", i+1, oneLine(s.German))
	}
	reply, err := llm.Chat(ctx, []Message{
		{Role: "system", Content: backfillPrompt},
		{Role: "user", Content: user.String()},
	}, ChatOptions{JSON: true, MaxTokens: 120 * len(batch)})
	out := make([]string, len(batch))
	if err != nil {
		return out, err
	}
	var m map[string]any
	if err := decodeObject(reply, &m); err != nil {
		return out, fmt.Errorf("%w: %v", ErrModelOutput, err)
	}
	for k, v := range m {
		n, err := strconv.Atoi(strings.TrimSpace(k))
		s, ok := v.(string)
		if err != nil || !ok || n < 1 || n > len(batch) {
			continue
		}
		out[n-1] = oneLine(s)
	}
	return out, nil
}

func lastByte(path string) byte {
	f, err := os.Open(path)
	if err != nil {
		return '\n'
	}
	defer f.Close()
	b := make([]byte, 1)
	fi, err := f.Stat()
	if err != nil || fi.Size() == 0 {
		return '\n'
	}
	if _, err := f.ReadAt(b, fi.Size()-1); err != nil {
		return '\n'
	}
	return b[0]
}

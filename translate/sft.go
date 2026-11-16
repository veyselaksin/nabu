package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/veyselaksin/nabu/data/corpus"
)

// Splits of the fine-tuning data.
const (
	SplitTrain = "train"
	SplitValid = "valid"
	SplitTest  = "test"
)

// SplitOf assigns a sentence group (corpus.Sentence.Group: a text, or a
// formula repeated across texts) to a split: 90% train, 5% valid, 5% test.
// The split depends only on the group, so a text never straddles splits and
// the assignment is stable across corpus rebuilds.
func SplitOf(group string) string {
	h := fnv.New32a()
	h.Write([]byte(group))
	switch b := h.Sum32() % 100; {
	case b < 90:
		return SplitTrain
	case b < 95:
		return SplitValid
	}
	return SplitTest
}

// SFTExample is one fine-tuning record in the chat format of mlx-lm (and
// most trainers): {"messages": [system, user, assistant]}.
type SFTExample struct {
	Messages []Message `json:"messages"`
}

// SFTMeta describes one exported example, for evaluation.
type SFTMeta struct {
	ID        string `json:"id"`
	Split     string `json:"split"`
	Lang      string `json:"lang"` // "German" or "English"
	Input     string `json:"input"`
	Reference string `json:"reference"`
	Group     string `json:"group"`
	// MachineInput marks English inputs translated by a model from the
	// editors' German.
	MachineInput bool `json:"machine_input,omitempty"`
}

// SFTOptions configures ExportSFT.
type SFTOptions struct {
	// Max examples per split (0: all). Selection is a stable pseudo-random
	// sample over groups.
	MaxTrain, MaxValid, MaxTest int
	Batch                       int // queries per embeddings request (default 64)
	Progress                    io.Writer
}

// SFTManifest records an export.
type SFTManifest struct {
	Built    time.Time      `json:"built"`
	Counts   map[string]int `json:"counts"`
	Langs    map[string]int `json:"langs"`
	Model    string         `json:"embed_model,omitempty"`
	Examples int            `json:"examples_per_prompt"`
	Glossary int            `json:"glossary_per_prompt"`
	License  string         `json:"license"`
}

type sftItem struct {
	s     corpus.Sentence
	lang  string
	input string
	split string
	mt    bool
	order uint32
}

// ExportSFT writes fine-tuning data to dir: {train,valid,test}.jsonl in chat
// format, {train,valid,test}-meta.jsonl, and manifest.json.
//
// Each Middle-Egyptian-usable sentence (stage "earlier" or unknown, at most
// maxExampleWords words) yields a German-input example (the editors'
// translation) and, if english has it, an English-input one. The prompt is
// built exactly as at inference (Translator.Prompt), with retrieval run on
// the input (the German text stands in for the model-generated keywords),
// and the sentence's own text and its duplicates excluded, so that the
// model learns to use the retrieved material rather than to copy the
// answer.
func ExportSFT(ctx context.Context, t *Translator, sents []corpus.Sentence, english map[string]English, dir string, opts SFTOptions) (*SFTManifest, error) {
	if opts.Batch <= 0 {
		opts.Batch = 64
	}
	var items []sftItem
	for _, s := range sents {
		n := len(strings.Fields(s.Translit))
		if n == 0 || n > maxExampleWords || s.German == "" || !t.stageOK(s.Stage) {
			continue
		}
		split := SplitOf(s.Group)
		items = append(items, sftItem{s: s, lang: "German", input: s.German, split: split, order: hash32(s.ID + "|de")})
		if e, ok := english[s.ID]; ok && e.English != "" {
			items = append(items, sftItem{s: s, lang: "English", input: e.English, split: split, mt: e.MachineTranslated, order: hash32(s.ID + "|en")})
		}
	}
	// Stable pseudo-random order, then cap each split.
	sort.Slice(items, func(i, j int) bool { return items[i].order < items[j].order })
	limit := map[string]int{SplitTrain: opts.MaxTrain, SplitValid: opts.MaxValid, SplitTest: opts.MaxTest}
	kept := map[string]int{}
	sel := items[:0]
	for _, it := range items {
		if l := limit[it.split]; l > 0 && kept[it.split] >= l {
			continue
		}
		kept[it.split]++
		sel = append(sel, it)
	}
	items = sel

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	files := map[string]*os.File{}
	encs := map[string]*json.Encoder{}
	for _, split := range []string{SplitTrain, SplitValid, SplitTest} {
		for _, name := range []string{split, split + "-meta"} {
			f, err := os.Create(filepath.Join(dir, name+".jsonl"))
			if err != nil {
				return nil, err
			}
			defer f.Close()
			files[name] = f
			enc := json.NewEncoder(f)
			enc.SetEscapeHTML(false)
			encs[name] = enc
		}
	}

	m := &SFTManifest{Counts: map[string]int{}, Langs: map[string]int{}, Examples: t.opts.Examples, Glossary: t.opts.Glossary,
		License: "CC-BY-SA-4.0 (derived from the corpus sources; keep local, see DATA_LICENSES.md)"}
	if t.opts.Vectors != nil {
		m.Model = t.opts.Vectors.Manifest.Model
	}
	start := time.Now()
	for i := 0; i < len(items); i += opts.Batch {
		batch := items[i:min(i+opts.Batch, len(items))]
		var vecs [][]float32
		if t.opts.Vectors != nil && t.opts.Embedder != nil {
			queries := make([]string, len(batch))
			for j, it := range batch {
				queries[j] = QueryText(it.input)
			}
			var err error
			if vecs, err = t.opts.Embedder.Embed(ctx, queries); err != nil {
				return nil, fmt.Errorf("embedding queries: %w", err)
			}
		}
		for j, it := range batch {
			exclude := sameText(it.s)
			keywords := []string{it.s.German} // the BM25 pivot is the German translation
			var hits []Hit
			var glossary []Gloss
			if vecs != nil {
				hits, glossary = t.retrieveVector(vecs[j], it.input, nil, t.bm25(keywords, exclude, 2*t.opts.Examples), exclude)
			} else {
				hits = t.bm25(keywords, exclude, t.opts.Examples)
			}
			msgs := t.Prompt(it.lang, it.input, hits, glossary)
			answer, _ := json.Marshal(map[string]string{"transliteration": promptForm(it.s.Translit), "notes": ""})
			msgs = append(msgs, Message{Role: "assistant", Content: string(answer)})
			if err := encs[it.split].Encode(SFTExample{Messages: msgs}); err != nil {
				return nil, err
			}
			meta := SFTMeta{ID: it.s.ID, Split: it.split, Lang: it.lang, Input: it.input,
				Reference: promptForm(it.s.Translit), Group: it.s.Group, MachineInput: it.mt}
			if err := encs[it.split+"-meta"].Encode(meta); err != nil {
				return nil, err
			}
			m.Counts[it.split]++
			m.Langs[it.lang]++
		}
		if opts.Progress != nil {
			n := i + len(batch)
			rate := float64(n) / time.Since(start).Seconds()
			fmt.Fprintf(opts.Progress, "\rexported %d/%d (%.0f/s)   ", n, len(items), rate)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if opts.Progress != nil {
		fmt.Fprintln(opts.Progress)
	}
	for name, f := range files {
		if err := f.Sync(); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	m.Built = time.Now().UTC().Truncate(time.Second)
	raw, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), append(raw, '\n'), 0o644); err != nil {
		return nil, err
	}
	if m.Counts[SplitTrain] == 0 {
		return m, errors.New("no training examples exported")
	}
	return m, nil
}

// sameText excludes the sentence's own text (same text ID) and any record
// with the same transliteration: retrieving them would leak the answer.
func sameText(s corpus.Sentence) func(*Doc) bool {
	form := promptForm(s.Translit)
	return func(d *Doc) bool {
		if d.Kind != KindSentence {
			return false
		}
		return (s.TextID != "" && d.TextID == s.TextID) || promptForm(d.Translit) == form
	}
}

func hash32(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

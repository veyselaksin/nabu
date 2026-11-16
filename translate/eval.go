package translate

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/veyselaksin/nabu/data/corpus"
)

// chrF parameters (Popović 2015, as in sacrebleu): character n-grams up to
// 6, recall weighted twice as much as precision.
const (
	chrfOrder = 6
	chrfBeta  = 2
)

// ChrF accumulates corpus-level chrF statistics.
type ChrF struct {
	match, hyp, ref [chrfOrder]float64
}

// Add adds one hypothesis/reference pair. Spaces are ignored, as in chrF.
func (c *ChrF) Add(hyp, ref string) {
	h := []rune(strings.Join(strings.Fields(hyp), ""))
	r := []rune(strings.Join(strings.Fields(ref), ""))
	for n := 1; n <= chrfOrder; n++ {
		hc, rc := ngrams(h, n), ngrams(r, n)
		for g, k := range hc {
			c.hyp[n-1] += float64(k)
			c.match[n-1] += float64(min(k, rc[g]))
		}
		for _, k := range rc {
			c.ref[n-1] += float64(k)
		}
	}
}

// Score returns chrF in [0, 100]: precision and recall averaged over the
// n-gram orders that occur, combined as an F-beta score.
func (c *ChrF) Score() float64 {
	var p, r float64
	np, nr := 0, 0
	for i := range chrfOrder {
		if c.hyp[i] > 0 {
			p += c.match[i] / c.hyp[i]
			np++
		}
		if c.ref[i] > 0 {
			r += c.match[i] / c.ref[i]
			nr++
		}
	}
	if np == 0 || nr == 0 {
		return 0
	}
	p, r = p/float64(np), r/float64(nr)
	if p+r == 0 {
		return 0
	}
	b2 := float64(chrfBeta * chrfBeta)
	return 100 * (1 + b2) * p * r / (b2*p + r)
}

func ngrams(s []rune, n int) map[string]int {
	out := map[string]int{}
	for i := 0; i+n <= len(s); i++ {
		out[string(s[i:i+n])]++
	}
	return out
}

// EvalRow is one evaluated sentence.
type EvalRow struct {
	ID         string  `json:"id"`
	Lang       string  `json:"lang"`
	Input      string  `json:"input"`
	Reference  string  `json:"reference"`
	Output     string  `json:"output"`
	ChrF       float64 `json:"chrf"`
	Confidence float64 `json:"attested"`
	Foreign    int     `json:"foreign_words"`
	Error      string  `json:"error,omitempty"`
}

// EvalReport summarizes an evaluation.
type EvalReport struct {
	Label     string    `json:"label"`
	Model     string    `json:"model"`
	Retrieval string    `json:"retrieval"`
	Split     string    `json:"split"`
	Lang      string    `json:"lang"`
	N         int       `json:"n"`
	Errors    int       `json:"errors"`
	ChrF      float64   `json:"chrf"`
	Attested  float64   `json:"attested_share"`
	Foreign   float64   `json:"foreign_word_share"`
	Exact     float64   `json:"exact_match"`
	Started   time.Time `json:"started"`
	Seconds   float64   `json:"seconds"`
	Rows      []EvalRow `json:"rows"`
}

// ReadSFTMeta reads a *-meta.jsonl file written by ExportSFT.
func ReadSFTMeta(r io.Reader) ([]SFTMeta, error) {
	var out []SFTMeta
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var m SFTMeta
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			return nil, fmt.Errorf("meta line %d: %w", len(out)+1, err)
		}
		out = append(out, m)
	}
	return out, sc.Err()
}

// EvalOptions configures Evaluate.
type EvalOptions struct {
	Label    string
	Lang     string // only inputs in this language ("" = all)
	Limit    int    // at most this many sentences (0: all)
	Progress io.Writer
}

// Evaluate translates held-out sentences (with their own text hidden from
// retrieval) and scores the output against the reference transliteration.
// A failed translation counts as an empty output.
func Evaluate(ctx context.Context, t *Translator, metas []SFTMeta, opts EvalOptions) (*EvalReport, error) {
	rep := &EvalReport{Label: opts.Label, Model: t.Model(), Lang: opts.Lang, Started: time.Now().UTC().Truncate(time.Second)}
	if t.opts.Vectors != nil && t.opts.Embedder != nil {
		rep.Retrieval = "hybrid"
	} else {
		rep.Retrieval = "bm25"
	}
	var total ChrF
	var attested, foreign, words, exact float64
	for _, m := range metas {
		if opts.Lang != "" && m.Lang != opts.Lang {
			continue
		}
		if opts.Limit > 0 && rep.N >= opts.Limit {
			break
		}
		rep.Split = m.Split
		rep.N++
		row := EvalRow{ID: m.ID, Lang: m.Lang, Input: m.Input, Reference: m.Reference}
		textID, _ := strings.CutPrefix(m.Group, "x:")
		if !strings.HasPrefix(m.Group, "x:") {
			textID = ""
		}
		exclude := sameText(sentenceRef(m.Reference, textID))
		res, err := t.TranslateFrom(ctx, m.Lang, m.Input, exclude)
		switch {
		case err != nil && ctx.Err() != nil:
			return nil, ctx.Err()
		case err != nil && !errors.Is(err, ErrModelOutput):
			return nil, fmt.Errorf("%s: %w", m.ID, err)
		case err != nil:
			row.Error = err.Error()
			rep.Errors++
		default:
			row.Output = res.Transliteration
			row.Confidence = res.Confidence
			for _, w := range res.Words {
				words++
				if w.Attested {
					attested++
				}
				if len(w.Foreign) > 0 {
					row.Foreign++
					foreign++
				}
			}
		}
		var one ChrF
		one.Add(row.Output, row.Reference)
		row.ChrF = one.Score()
		total.Add(row.Output, row.Reference)
		if row.Output == row.Reference {
			exact++
		}
		rep.Rows = append(rep.Rows, row)
		if opts.Progress != nil {
			fmt.Fprintf(opts.Progress, "\r%d evaluated, chrF %.1f   ", rep.N, total.Score())
		}
	}
	if opts.Progress != nil {
		fmt.Fprintln(opts.Progress)
	}
	if rep.N == 0 {
		return nil, errors.New("no sentences to evaluate")
	}
	rep.ChrF = total.Score()
	rep.Exact = exact / float64(rep.N)
	if words > 0 {
		rep.Attested, rep.Foreign = attested/words, foreign/words
	}
	rep.Seconds = time.Since(rep.Started).Seconds()
	return rep, nil
}

func sentenceRef(translit, textID string) corpus.Sentence {
	return corpus.Sentence{Translit: translit, TextID: textID}
}

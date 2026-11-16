package translate

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// LemmaKey reduces a transliterated lemma to a form that is the same in the
// TLA conventions (ꞽ, i̯, u̯, ".", "ʾ") and in those of English Wiktionary
// (j, no separators), so that the two can be compared: "mri̯" and "mrj",
// "ḥnq.t" and "ḥnqt", "tʾ" and "t".
func LemmaKey(s string) string {
	s = norm.NFC.String(strings.ToLower(s))
	s = strings.NewReplacer("i̯", "j", "ꞽ", "j", "ı͗", "j", "u̯", "w", ".", "", "-", "", "ʾ", "", "=", "", " ", "").Replace(s)
	return s
}

// GlossQuery is one line of a dictionary-retrieval test set: an English
// query and the lemmas (as LemmaKey) that answer it.
type GlossQuery struct {
	Query  string   `json:"query"`
	Lemmas []string `json:"lemmas"`
	// Count is the corpus frequency of the most frequent answer.
	Count int `json:"count"`
}

// wiktEntry is the part of a Wiktextract (kaikki.org) record used here.
type wiktEntry struct {
	Word   string `json:"word"`
	POS    string `json:"pos"`
	Senses []struct {
		Glosses []string `json:"glosses"`
	} `json:"senses"`
}

// BuildGlossQueries derives a dictionary-retrieval test set from English
// Wiktionary's Egyptian entries (Wiktextract JSON lines): every noun, verb
// or adjective whose first gloss is one English word (or "to" + one word)
// and whose headword is a lemma of the lexicon that occurs at least
// minCount times in the lemmatized corpus. Headwords that share a gloss are
// all accepted as answers. The glosses come from Wiktionary, not from the
// lexicon being searched, but nobody has reviewed the pairs: treat the set
// as a regression measure, not as ground truth. It is CC BY-SA 4.0
// (Wiktionary) and stays local.
func BuildGlossQueries(wiktionary io.Reader, lexicon []Doc, freq func(id string) int, minCount, limit int) ([]GlossQuery, error) {
	best := map[string]int{} // LemmaKey → highest frequency of a lexicon lemma with that key
	for _, d := range lexicon {
		if d.Kind != KindLexicon || d.Source != "aed" {
			continue
		}
		if n := freq(d.ID); n > best[LemmaKey(d.Translit)] {
			best[LemmaKey(d.Translit)] = n
		}
	}
	byGloss := map[string]*GlossQuery{}
	sc := bufio.NewScanner(wiktionary)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	line := 0
	for sc.Scan() {
		line++
		var e wiktEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("wiktionary line %d: %w", line, err)
		}
		if e.POS != "noun" && e.POS != "verb" && e.POS != "adj" {
			continue
		}
		if len(e.Senses) == 0 || len(e.Senses[0].Glosses) == 0 {
			continue
		}
		gloss := strings.TrimSpace(e.Senses[0].Glosses[0])
		word := strings.TrimPrefix(gloss, "to ")
		if word == "" || strings.IndexFunc(word, func(r rune) bool { return !unicode.IsLower(r) }) >= 0 {
			continue
		}
		key := LemmaKey(e.Word)
		n := best[key]
		if n < minCount {
			continue
		}
		q := byGloss[gloss]
		if q == nil {
			q = &GlossQuery{Query: gloss}
			byGloss[gloss] = q
		}
		if !contains(q.Lemmas, key) {
			q.Lemmas = append(q.Lemmas, key)
		}
		q.Count = max(q.Count, n)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	out := make([]GlossQuery, 0, len(byGloss))
	for _, q := range byGloss {
		sort.Strings(q.Lemmas)
		out = append(out, *q)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Query < out[j].Query
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// ReadGlossQueries reads a JSON-lines test set written from BuildGlossQueries.
func ReadGlossQueries(r io.Reader) ([]GlossQuery, error) {
	var out []GlossQuery
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var q GlossQuery
		if err := json.Unmarshal(sc.Bytes(), &q); err != nil {
			return nil, fmt.Errorf("query line %d: %w", len(out)+1, err)
		}
		out = append(out, q)
	}
	return out, sc.Err()
}

// Glossary returns the dictionary entries a translation of text would be
// given (vector index required).
func (t *Translator) Glossary(ctx context.Context, text string, keywords []string) ([]Gloss, error) {
	if t.opts.Vectors == nil || t.opts.Embedder == nil {
		return nil, errors.New("translate: the glossary needs the vector index")
	}
	vecs, err := t.opts.Embedder.Embed(ctx, []string{QueryText(text)})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, errors.New("translate: no embedding returned")
	}
	_, glossary := t.retrieveVector(vecs[0], text, keywords, nil, t.opts.Exclude)
	return glossary, nil
}

// GlossEvalRow is one evaluated query; Rank is the 1-based position of the
// first right answer in the glossary, 0 when it is absent.
type GlossEvalRow struct {
	Query  string   `json:"query"`
	Lemmas []string `json:"lemmas"`
	Rank   int      `json:"rank"`
	Got    []string `json:"got"`
}

// GlossEvalReport summarizes dictionary retrieval on a test set.
type GlossEvalReport struct {
	Label      string         `json:"label"`
	Mode       string         `json:"mode"` // "exact+similar" or "similar"
	EmbedModel string         `json:"embed_model"`
	N          int            `json:"n"`
	Recall1    float64        `json:"recall_at_1"`
	Recall5    float64        `json:"recall_at_5"`
	Recall10   float64        `json:"recall_at_10"`
	MRR        float64        `json:"mrr"`
	Started    time.Time      `json:"started"`
	Rows       []GlossEvalRow `json:"rows"`
}

// EvaluateGlossary measures where the right lemma lands in the glossary
// (first 10 entries) for each query.
func EvaluateGlossary(ctx context.Context, t *Translator, queries []GlossQuery, label string) (*GlossEvalReport, error) {
	rep := &GlossEvalReport{Label: label, Mode: "exact+similar", Started: time.Now().UTC().Truncate(time.Second)}
	if t.opts.NoExactGloss {
		rep.Mode = "similar"
	}
	if t.opts.Vectors != nil {
		rep.EmbedModel = t.opts.Vectors.Manifest.Model
	}
	for _, q := range queries {
		glossary, err := t.Glossary(ctx, q.Query, nil)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", q.Query, err)
		}
		row := GlossEvalRow{Query: q.Query, Lemmas: q.Lemmas}
		for i, g := range glossary {
			if i == 10 {
				break
			}
			row.Got = append(row.Got, g.Lemma)
			if row.Rank == 0 && contains(q.Lemmas, LemmaKey(g.Lemma)) {
				row.Rank = i + 1
			}
		}
		rep.Rows = append(rep.Rows, row)
		rep.N++
		if row.Rank > 0 {
			rep.MRR += 1 / float64(row.Rank)
			rep.Recall10++
			if row.Rank <= 5 {
				rep.Recall5++
			}
			if row.Rank == 1 {
				rep.Recall1++
			}
		}
	}
	if rep.N == 0 {
		return nil, errors.New("no queries to evaluate")
	}
	n := float64(rep.N)
	rep.Recall1, rep.Recall5, rep.Recall10, rep.MRR = rep.Recall1/n, rep.Recall5/n, rep.Recall10/n, rep.MRR/n
	return rep, nil
}

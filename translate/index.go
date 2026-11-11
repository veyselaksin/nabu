package translate

import (
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/veyselaksin/nabu/data/tla"
)

// Example is a corpus sentence shown to the model as a reference translation.
type Example struct {
	// Transliteration is the sentence as published (editor's brackets kept).
	Transliteration string `json:"transliteration"`
	// Translation is the editors' German translation.
	Translation   string `json:"translation"`
	DateNotBefore string `json:"date_not_before,omitempty"`
	DateNotAfter  string `json:"date_not_after,omitempty"`
}

// Hit is a retrieved example and its relevance score.
type Hit struct {
	Example
	Score float64 `json:"score"`
}

// maxExampleWords drops very long sentences from retrieval: they would crowd
// the prompt of a small local model. It does not affect the form dictionary.
const maxExampleWords = 40

// BM25 parameters (the common defaults).
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// Prefix matching lets a search term match inflected and compound German
// words that start with it ("Brot" matches "Brotes"); shorter terms match
// whole words only.
const minPrefixRunes = 4

type posting struct {
	doc int
	tf  int
}

// Index is an in-memory search index over the German translations of a TLA
// corpus, plus a dictionary of every transliterated form in it.
type Index struct {
	examples []Example
	postings map[string][]posting
	vocab    []string // sorted keys of postings
	docLen   []int
	avgLen   float64
	forms    map[string]int
}

// NewIndex indexes sentences. Sentences without a translation are kept in
// the form dictionary but cannot be retrieved; duplicate sentences are
// indexed once.
func NewIndex(sentences []tla.Sentence) *Index {
	ix := &Index{postings: map[string][]posting{}, forms: map[string]int{}}
	seen := map[[2]string]bool{}
	total := 0
	for _, s := range sentences {
		words := strings.Fields(s.Transliteration)
		for _, w := range words {
			if f := tla.NormalizeForm(w); f != "" {
				ix.forms[f]++
			}
		}
		de := strings.TrimSpace(s.Translation)
		if de == "" || len(words) == 0 || len(words) > maxExampleWords {
			continue
		}
		key := [2]string{s.Transliteration, de}
		if seen[key] {
			continue
		}
		seen[key] = true

		doc := len(ix.examples)
		ix.examples = append(ix.examples, Example{
			Transliteration: s.Transliteration, Translation: de,
			DateNotBefore: s.DateNotBefore, DateNotAfter: s.DateNotAfter,
		})
		tokens := tokenize(de)
		ix.docLen = append(ix.docLen, len(tokens))
		total += len(tokens)
		tf := map[string]int{}
		for _, t := range tokens {
			tf[t]++
		}
		for t, n := range tf {
			ix.postings[t] = append(ix.postings[t], posting{doc, n})
		}
	}
	ix.vocab = make([]string, 0, len(ix.postings))
	for t := range ix.postings {
		ix.vocab = append(ix.vocab, t)
	}
	sort.Strings(ix.vocab)
	if len(ix.examples) > 0 {
		ix.avgLen = float64(total) / float64(len(ix.examples))
	}
	return ix
}

// Len returns the number of retrievable examples.
func (ix *Index) Len() int { return len(ix.examples) }

// Forms returns the number of distinct forms in the dictionary.
func (ix *Index) Forms() int { return len(ix.forms) }

// FormCount returns how often a transliterated form occurs in the corpus
// (after tla.NormalizeForm).
func (ix *Index) FormCount(form string) int { return ix.forms[tla.NormalizeForm(form)] }

// Search returns up to k examples whose German translation best matches the
// search terms. Each term first contributes its own best example, so that a
// frequent term cannot crowd out the others; the remaining places go to the
// highest overall scores.
func (ix *Index) Search(terms []string, k int) []Hit {
	if k <= 0 || len(ix.examples) == 0 {
		return nil
	}
	var queries []string
	seenQ := map[string]bool{}
	for _, t := range terms {
		for _, q := range tokenize(t) {
			if !seenQ[q] {
				seenQ[q] = true
				queries = append(queries, q)
			}
		}
	}

	total := map[int]float64{}
	var bestPerQuery []int
	for _, q := range queries {
		// A document's score for q is its best score over q's expansions.
		perDoc := map[int]float64{}
		for _, term := range ix.expand(q) {
			ps := ix.postings[term]
			idf := math.Log(1 + (float64(len(ix.examples))-float64(len(ps))+0.5)/(float64(len(ps))+0.5))
			for _, p := range ps {
				tf := float64(p.tf)
				norm := tf * (bm25K1 + 1) / (tf + bm25K1*(1-bm25B+bm25B*float64(ix.docLen[p.doc])/ix.avgLen))
				if s := idf * norm; s > perDoc[p.doc] {
					perDoc[p.doc] = s
				}
			}
		}
		best, bestScore := -1, 0.0
		for doc, s := range perDoc {
			total[doc] += s
			if s > bestScore || (s == bestScore && doc < best) {
				best, bestScore = doc, s
			}
		}
		if best >= 0 {
			bestPerQuery = append(bestPerQuery, best)
		}
	}

	ranked := make([]int, 0, len(total))
	for doc := range total {
		ranked = append(ranked, doc)
	}
	sort.Slice(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if total[a] != total[b] {
			return total[a] > total[b]
		}
		return a < b
	})

	chosen := map[int]bool{}
	var order []int
	for _, doc := range append(bestPerQuery, ranked...) {
		if len(order) == k {
			break
		}
		if !chosen[doc] {
			chosen[doc] = true
			order = append(order, doc)
		}
	}
	// Present the chosen examples best first.
	sort.SliceStable(order, func(i, j int) bool { return total[order[i]] > total[order[j]] })
	hits := make([]Hit, len(order))
	for i, doc := range order {
		hits[i] = Hit{Example: ix.examples[doc], Score: total[doc]}
	}
	return hits
}

// expand returns the vocabulary terms a query token matches.
func (ix *Index) expand(q string) []string {
	if utf8.RuneCountInString(q) < minPrefixRunes {
		if _, ok := ix.postings[q]; ok {
			return []string{q}
		}
		return nil
	}
	i := sort.SearchStrings(ix.vocab, q)
	var out []string
	for ; i < len(ix.vocab) && strings.HasPrefix(ix.vocab[i], q); i++ {
		out = append(out, ix.vocab[i])
	}
	return out
}

// tokenize lower-cases s (NFC) and splits it into runs of letters and digits.
// Single-character tokens are dropped.
func tokenize(s string) []string {
	s = norm.NFC.String(strings.ToLower(s))
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.Is(unicode.Mn, r)
	})
	out := fields[:0]
	for _, f := range fields {
		if utf8.RuneCountInString(f) > 1 {
			out = append(out, f)
		}
	}
	return out
}

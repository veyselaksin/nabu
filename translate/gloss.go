package translate

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// How a dictionary entry was found.
const (
	MatchExact   = "exact"   // a word of the text equals one of the entry's glosses
	MatchSimilar = "similar" // nearest by embedding
)

// GlossIndex finds dictionary entries whose English or German gloss is
// exactly a word (or two adjacent words) of the text. Embeddings rank
// compounds and titles that merely contain a word ("ḥqꜣ-ꜥnḫ.w") next to or
// above the plain lemma ("ꜥnḫ: to live"); an exact lookup does not.
type GlossIndex struct {
	entries  []Doc
	byPhrase map[string][]glossRef // normalized gloss phrase → entries
	byID     map[string]int
	freq     func(id string) int
}

type glossRef struct {
	entry int
	first bool // the phrase is the entry's first gloss
}

// NewGlossIndex indexes the non-Coptic lexicon entries among docs. freq
// returns the corpus frequency of an entry ID (nil: all zero); it ranks
// entries that share a gloss.
func NewGlossIndex(docs []Doc, freq func(id string) int) *GlossIndex {
	if freq == nil {
		freq = func(string) int { return 0 }
	}
	g := &GlossIndex{byPhrase: map[string][]glossRef{}, byID: map[string]int{}, freq: freq}
	for _, d := range docs {
		if d.Kind != KindLexicon || d.Stage == "coptic" {
			continue
		}
		i := len(g.entries)
		g.entries = append(g.entries, d)
		g.byID[d.ID] = i
		seen := map[string]bool{}
		for _, text := range []string{d.English, d.German} {
			for n, ph := range glossPhrases(text) {
				if !seen[ph] {
					seen[ph] = true
					g.byPhrase[ph] = append(g.byPhrase[ph], glossRef{i, n == 0})
				}
			}
		}
	}
	return g
}

// Entry returns the entry with this ID.
func (g *GlossIndex) Entry(id string) (Doc, bool) {
	if g == nil {
		return Doc{}, false
	}
	i, ok := g.byID[id]
	if !ok {
		return Doc{}, false
	}
	return g.entries[i], true
}

// bracketGap marks where glossPhrases removed a bracketed part.
const bracketGap = '\x00'

// glossLeading are words dropped from the start of a gloss phrase, so that
// "to live", "the king" and "der König" are found as "live", "king", "könig".
var glossLeading = map[string]bool{
	"to": true, "the": true, "a": true, "an": true, "be": true,
	"der": true, "die": true, "das": true, "ein": true, "eine": true, "sich": true,
}

// glossPhrases splits a dictionary gloss ("to live; to be alive (of gods)")
// into normalized phrases ("live", "alive"): lower case, text in round or
// square brackets removed, split at ";" and ",", leading function words
// dropped. The first phrase of the gloss comes first.
func glossPhrases(gloss string) []string {
	var b strings.Builder
	depth := 0
	for _, r := range norm.NFC.String(strings.ToLower(gloss)) {
		switch r {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				if depth--; depth == 0 {
					b.WriteRune(bracketGap)
				}
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	var out []string
	for _, part := range strings.FieldsFunc(b.String(), func(r rune) bool { return r == ';' || r == ',' || r == '/' }) {
		// A part with removed brackets between its words ("das (NN) gibt")
		// is not a gloss on its own.
		if strings.ContainsRune(strings.Trim(part, " "+string(bracketGap)), bracketGap) {
			continue
		}
		words := strings.FieldsFunc(part, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.Is(unicode.Mn, r) && r != '-' })
		for len(words) > 1 && glossLeading[words[0]] {
			words = words[1:]
		}
		if len(words) == 0 || len(words) > 3 {
			continue
		}
		out = append(out, strings.Join(words, " "))
	}
	return out
}

// queryStop are words of the text that are not looked up on their own.
var queryStop = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "to": true, "and": true, "or": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true, "am": true,
	"may": true, "shall": true, "will": true, "let": true, "do": true, "does": true, "did": true,
	"that": true, "this": true, "it": true, "as": true, "at": true, "by": true,
}

// wordVariants returns w and simple English base forms of it ("kings" →
// "king", "loved" → "love", "giving" → "give"). The first variant that has
// dictionary entries is used.
func wordVariants(w string) []string {
	out := []string{w}
	add := func(s string) {
		if len([]rune(s)) >= 2 {
			out = append(out, s)
		}
	}
	switch {
	case strings.HasSuffix(w, "ies"):
		add(strings.TrimSuffix(w, "ies") + "y")
	case strings.HasSuffix(w, "es"):
		add(strings.TrimSuffix(w, "es"))
		add(strings.TrimSuffix(w, "s"))
	case strings.HasSuffix(w, "s"):
		add(strings.TrimSuffix(w, "s"))
	}
	if strings.HasSuffix(w, "ing") {
		add(strings.TrimSuffix(w, "ing"))
		add(strings.TrimSuffix(w, "ing") + "e")
	}
	if strings.HasSuffix(w, "ed") {
		add(strings.TrimSuffix(w, "ed"))
		add(strings.TrimSuffix(w, "d"))
	}
	return out
}

// ContentWords returns the words of text that carry meaning of their own
// (not queryStop), lower-cased, in order, each once.
func ContentWords(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range tokenize(text) {
		if !queryStop[w] && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

// Means reports whether the entry with this ID has word (or a base form of
// it, see wordVariants) among its glosses.
func (g *GlossIndex) Means(id, word string) bool {
	e, ok := g.Entry(id)
	if !ok {
		return false
	}
	for _, text := range []string{e.English, e.German} {
		for _, ph := range glossPhrases(text) {
			for _, v := range wordVariants(word) {
				if ph == v {
					return true
				}
			}
		}
	}
	return false
}

// Lookup returns the dictionary entries whose gloss is exactly a term: a
// word of text (or two adjacent words), or one of the extra terms (the
// German search keywords). For each term, entries are ranked by "the term
// is the entry's first gloss", then by corpus frequency, and at most
// perTerm are kept; entries that never occur in the corpus are used only
// when a term has no other. At most max entries are returned, in the order
// of the terms. Entries that exclude reports (may be nil) are skipped.
func (g *GlossIndex) Lookup(text string, extra []string, perTerm, max int, exclude func(*Doc) bool) []Gloss {
	if g == nil || perTerm <= 0 || max <= 0 {
		return nil
	}
	words := tokenize(text)
	var terms [][]string // each: variants of one term
	for i, w := range words {
		if i+1 < len(words) {
			terms = append(terms, []string{w + " " + words[i+1]})
		}
		if !queryStop[w] {
			terms = append(terms, wordVariants(w))
		}
	}
	for _, e := range extra {
		if ws := tokenize(e); len(ws) > 0 {
			terms = append(terms, []string{strings.Join(ws, " ")})
		}
	}
	var out []Gloss
	seen := map[int]bool{}
	for _, variants := range terms {
		var refs []glossRef
		term := ""
		for _, v := range variants {
			if refs = g.byPhrase[v]; len(refs) > 0 {
				term = v
				break
			}
		}
		if len(refs) == 0 {
			continue
		}
		kept0 := make([]glossRef, 0, len(refs))
		for _, r := range refs {
			if exclude == nil || !exclude(&g.entries[r.entry]) {
				kept0 = append(kept0, r)
			}
		}
		refs = kept0
		sort.SliceStable(refs, func(a, b int) bool {
			fa, fb := g.freq(g.entries[refs[a].entry].ID), g.freq(g.entries[refs[b].entry].ID)
			if (fa > 0) != (fb > 0) {
				return fa > 0
			}
			if refs[a].first != refs[b].first {
				return refs[a].first
			}
			if fa != fb {
				return fa > fb
			}
			return g.entries[refs[a].entry].ID < g.entries[refs[b].entry].ID
		})
		kept := 0
		for _, r := range refs {
			if kept == perTerm || len(out) == max {
				break
			}
			e := g.entries[r.entry]
			n := g.freq(e.ID)
			if n == 0 && kept > 0 {
				break
			}
			kept++
			if seen[r.entry] {
				continue
			}
			seen[r.entry] = true
			out = append(out, Gloss{ID: e.ID, Source: e.Source, Lemma: e.Translit, English: e.English, EnglishSource: e.EnglishSource,
				German: e.German, POS: e.POS, Match: MatchExact, Term: term, Count: n})
		}
	}
	return out
}

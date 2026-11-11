// Package tla reads the Thesaurus Linguae Aegyptiae (TLA) sentence datasets
// published on Hugging Face (JSON lines, one sentence per line) and derives a
// spelling lexicon and sign frequencies from them.
//
// In these datasets the "hieroglyphs" and "transliteration" fields are both
// split into sentence elements by single spaces, element for element.
package tla

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/veyselaksin/nabu/script"
)

// Sentence is one record of a TLA sentence dataset (fields Nabu uses).
type Sentence struct {
	Hieroglyphs     string `json:"hieroglyphs"`
	Transliteration string `json:"transliteration"`
	// Translation is the editors' translation, in German.
	Translation   string `json:"translation"`
	DateNotBefore string `json:"dateNotBefore"`
	DateNotAfter  string `json:"dateNotAfter"`
}

// Read parses a JSON-lines file of sentences.
func Read(r io.Reader) ([]Sentence, error) {
	var out []Sentence
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	line := 0
	for sc.Scan() {
		line++
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var s Sentence
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
			return nil, fmt.Errorf("tla: line %d: %w", line, err)
		}
		out = append(out, s)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("tla: %w", err)
	}
	return out, nil
}

// IsHieroglyphText reports whether s consists only of Egyptian hieroglyphs
// (U+13000–U+1345F, including format controls) and variation selectors.
// TLA marks signs not yet in Unicode as "<g>JSesh-code</g>"; such spellings
// cannot be displayed and fail this check.
func IsHieroglyphText(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 0x13000 && c <= 0x1345F:
		case c >= 0xFE00 && c <= 0xFE0F:
		default:
			return false
		}
	}
	return true
}

// NormalizeForm turns a TLA transliteration element into a lexicon form: NFC,
// with the editor's round brackets removed but their content kept (the TLA
// dataset card describes brackets as marking phonemes added by the editor
// and suggests exactly this for model training).
func NormalizeForm(s string) string {
	return norm.NFC.String(strings.NewReplacer("(", "", ")", "").Replace(s))
}

// Stats summarizes what Lexicon kept and skipped.
type Stats struct {
	Sentences          int
	MisalignedSentence int // element counts of hieroglyphs and transliteration differ
	Elements           int // aligned elements seen
	NonUnicodeSpelling int // elements whose spelling uses signs not in Unicode
	EmptyForm          int
	Forms              int // distinct forms in the lexicon
}

// Lexicon counts, for every transliterated form, its spellings, and returns
// the most frequent spelling of each (ties: the lexicographically smaller
// spelling), sorted by form.
func Lexicon(sentences []Sentence) ([]script.LexiconEntry, Stats) {
	var st Stats
	spellings := map[string]map[string]int{}
	for _, s := range sentences {
		st.Sentences++
		h := strings.Split(norm.NFC.String(s.Hieroglyphs), " ")
		t := strings.Split(s.Transliteration, " ")
		if len(h) != len(t) {
			st.MisalignedSentence++
			continue
		}
		for i := range t {
			st.Elements++
			form := NormalizeForm(t[i])
			if form == "" {
				st.EmptyForm++
				continue
			}
			if !IsHieroglyphText(h[i]) {
				st.NonUnicodeSpelling++
				continue
			}
			m := spellings[form]
			if m == nil {
				m = map[string]int{}
				spellings[form] = m
			}
			m[h[i]]++
		}
	}

	entries := make([]script.LexiconEntry, 0, len(spellings))
	for form, m := range spellings {
		var best string
		bestN, total := 0, 0
		for sp, n := range m {
			total += n
			if n > bestN || (n == bestN && sp < best) {
				best, bestN = sp, n
			}
		}
		entries = append(entries, script.LexiconEntry{
			Form:     form,
			Spelling: script.Spelling{Hieroglyphs: best, Count: bestN, Total: total, Variants: len(m)},
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Form < entries[j].Form })
	st.Forms = len(entries)
	return entries, st
}

// SignCounts counts how often each hieroglyph code point occurs in the
// sentences, ignoring "<g>…</g>" placeholders for signs not in Unicode.
func SignCounts(sentences []Sentence) map[rune]int {
	counts := map[rune]int{}
	for _, s := range sentences {
		text := s.Hieroglyphs
		for {
			start := strings.Index(text, "<g>")
			if start < 0 {
				break
			}
			end := strings.Index(text[start:], "</g>")
			if end < 0 {
				text = text[:start]
				break
			}
			text = text[:start] + " " + text[start+end+len("</g>"):]
		}
		for _, c := range text {
			if c >= 0x13000 && c <= 0x1342F {
				counts[c]++
			}
		}
	}
	return counts
}

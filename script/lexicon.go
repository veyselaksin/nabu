package script

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Spelling is an attested hieroglyphic spelling of one transliterated word
// form, as counted in a corpus.
type Spelling struct {
	// Hieroglyphs is the most frequent spelling of the form.
	Hieroglyphs string `json:"hieroglyphs"`
	// Count is how often Hieroglyphs spells the form in the corpus.
	Count int `json:"count"`
	// Total is how often the form occurs in the corpus, in any spelling.
	Total int `json:"total"`
	// Variants is the number of distinct spellings of the form.
	Variants int `json:"variants"`
}

// Lexicon maps transliterated word forms to attested spellings. It is safe
// for concurrent use.
type Lexicon struct {
	Language string
	Source   Source

	entries map[string]Spelling
}

// LexiconColumns are the columns of a lexicon file, in order.
var LexiconColumns = []string{"form", "hieroglyphs", "count", "total", "variants"}

// LexiconMeta are the metadata keys every lexicon file must set.
var LexiconMeta = []string{"id", "language", "source", "source_url", "source_sha256", "license", "attribution"}

// LexiconEntry is one record of a lexicon file.
type LexiconEntry struct {
	Form string
	Spelling
}

// LoadLexicon reads a lexicon file from disk.
func LoadLexicon(filename string) (*Lexicon, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("script: %w", err)
	}
	defer f.Close()
	return ParseLexicon(f, filename)
}

// ParseLexicon reads and validates a lexicon. source labels the data in error
// messages.
func ParseLexicon(r io.Reader, source string) (*Lexicon, error) {
	t, err := readTable(r, source, LexiconColumns, LexiconMeta)
	if err != nil {
		return nil, err
	}
	lx := &Lexicon{Language: t.meta["language"], Source: t.source(), entries: make(map[string]Spelling, len(t.rows))}
	var problems []string
	for i, row := range t.rows {
		where := fmt.Sprintf("line %d", t.lines[i])
		form, glyphs := row[0], row[1]
		if form == "" || strings.ContainsFunc(form, isSpace) {
			problems = append(problems, fmt.Sprintf("%s: form %q must be non-empty and contain no spaces", where, form))
			continue
		}
		if glyphs == "" || strings.ContainsFunc(glyphs, isSpace) {
			problems = append(problems, fmt.Sprintf("%s: hieroglyphs %q must be non-empty and contain no spaces", where, glyphs))
			continue
		}
		var n [3]int
		ok := true
		for j := range n {
			v, err := strconv.Atoi(row[2+j])
			if err != nil || v < 1 {
				problems = append(problems, fmt.Sprintf("%s: %s %q must be a positive integer", where, LexiconColumns[2+j], row[2+j]))
				ok = false
			}
			n[j] = v
		}
		if !ok {
			continue
		}
		if n[0] > n[1] || n[2] > n[1] {
			problems = append(problems, fmt.Sprintf("%s: count and variants must not exceed total", where))
			continue
		}
		if _, dup := lx.entries[form]; dup {
			problems = append(problems, fmt.Sprintf("%s: duplicate form %q", where, form))
			continue
		}
		lx.entries[form] = Spelling{Hieroglyphs: glyphs, Count: n[0], Total: n[1], Variants: n[2]}
	}
	if len(problems) > 0 {
		return nil, &FormatError{Source: source, Problems: problems}
	}
	return lx, nil
}

// WriteLexicon writes a lexicon file. meta must contain every key in
// LexiconMeta. Entries are written in the order given.
func WriteLexicon(w io.Writer, meta map[string]string, comments []string, entries []LexiconEntry) error {
	for _, k := range LexiconMeta {
		if meta[k] == "" {
			return fmt.Errorf("script: lexicon metadata %q is required", k)
		}
	}
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{
			e.Form, e.Hieroglyphs, strconv.Itoa(e.Count), strconv.Itoa(e.Total), strconv.Itoa(e.Variants),
		})
	}
	return writeTable(w, meta, LexiconMeta, comments, LexiconColumns, rows)
}

// Lookup returns the attested spelling of a word form.
func (lx *Lexicon) Lookup(form string) (Spelling, bool) {
	s, ok := lx.entries[form]
	return s, ok
}

// Len returns the number of word forms in the lexicon.
func (lx *Lexicon) Len() int { return len(lx.entries) }

package script

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Sign is one hieroglyph used to spell a phonetic value.
type Sign struct {
	// Value is the transliteration value the sign spells, e.g. "ꜥ".
	Value string `json:"value"`
	// Sign is the Unicode character itself.
	Sign string `json:"sign"`
	// Codepoint is the sign's code point, e.g. "U+1309D".
	Codepoint string `json:"codepoint"`
	// Gardiner is the sign's Gardiner/JSesh code (e.g. "D36"), if any.
	Gardiner string `json:"gardiner,omitempty"`
}

// SignList maps transliteration values to the sign that spells each one.
// It is safe for concurrent use.
type SignList struct {
	Language string
	Source   Source

	byValue  map[string]Sign
	maxRunes int
}

// SignListColumns are the columns of a sign list file, in order.
var SignListColumns = []string{"value", "codepoint", "sign", "gardiner", "function", "corpus_count", "candidates"}

// SignListMeta are the metadata keys every sign list file must set.
var SignListMeta = []string{"id", "language", "source", "source_url", "source_sha256", "license", "selection"}

// SignRow is one record of a sign list file.
type SignRow struct {
	Sign
	// Function is the upstream description of the sign's function.
	Function string
	// CorpusCount is how often the sign occurs in the corpus used to choose
	// between candidate signs for the same value (0 if none was used).
	CorpusCount int
	// Candidates is how many upstream signs share this value.
	Candidates int
}

// LoadSignList reads a sign list file from disk.
func LoadSignList(filename string) (*SignList, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("script: %w", err)
	}
	defer f.Close()
	return ParseSignList(f, filename)
}

// ParseSignList reads and validates a sign list. source labels the data in
// error messages.
func ParseSignList(r io.Reader, source string) (*SignList, error) {
	t, err := readTable(r, source, SignListColumns, SignListMeta)
	if err != nil {
		return nil, err
	}
	sl := &SignList{Language: t.meta["language"], Source: t.source(), byValue: make(map[string]Sign, len(t.rows))}
	var problems []string
	for i, row := range t.rows {
		where := fmt.Sprintf("line %d", t.lines[i])
		value, cp, sign := row[0], row[1], row[2]
		if value == "" || strings.TrimSpace(value) != value || strings.ContainsFunc(value, isSpace) {
			problems = append(problems, fmt.Sprintf("%s: value %q must be non-empty and contain no spaces", where, value))
			continue
		}
		if first, _ := utf8.DecodeRuneInString(value); isMark(first) {
			problems = append(problems, fmt.Sprintf("%s: value %q must not start with a combining mark", where, value))
		}
		if want := codepoints(sign); utf8.RuneCountInString(sign) != 1 || want != cp {
			problems = append(problems, fmt.Sprintf("%s: sign %q must be one character matching codepoint %s (got %s)", where, sign, cp, want))
		}
		for col, f := range map[int]string{5: row[5], 6: row[6]} {
			if n, err := strconv.Atoi(f); err != nil || n < 0 {
				problems = append(problems, fmt.Sprintf("%s: %s %q must be a non-negative integer", where, SignListColumns[col], f))
			}
		}
		if _, dup := sl.byValue[value]; dup {
			problems = append(problems, fmt.Sprintf("%s: duplicate value %q", where, value))
			continue
		}
		sl.byValue[value] = Sign{Value: value, Sign: sign, Codepoint: cp, Gardiner: row[3]}
		sl.maxRunes = max(sl.maxRunes, utf8.RuneCountInString(value))
	}
	if len(sl.byValue) == 0 && len(problems) == 0 {
		problems = append(problems, "no signs")
	}
	if len(problems) > 0 {
		return nil, &FormatError{Source: source, Problems: problems}
	}
	return sl, nil
}

// WriteSignList writes a sign list file. meta must contain every key in
// SignListMeta.
func WriteSignList(w io.Writer, meta map[string]string, comments []string, rows []SignRow) error {
	for _, k := range SignListMeta {
		if meta[k] == "" {
			return fmt.Errorf("script: sign list metadata %q is required", k)
		}
	}
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, []string{
			r.Value, r.Codepoint, r.Sign.Sign, r.Gardiner, r.Function,
			strconv.Itoa(r.CorpusCount), strconv.Itoa(r.Candidates),
		})
	}
	return writeTable(w, meta, SignListMeta, comments, SignListColumns, out)
}

// Lookup returns the sign for a transliteration value.
func (sl *SignList) Lookup(value string) (Sign, bool) {
	s, ok := sl.byValue[value]
	return s, ok
}

// Len returns the number of values in the list.
func (sl *SignList) Len() int { return len(sl.byValue) }

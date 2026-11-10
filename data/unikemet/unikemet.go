// Package unikemet parses the Unicode Unikemet database (Unikemet.txt, UAX
// #57) and derives Nabu's uniliteral sign list from it.
package unikemet

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/veyselaksin/nabu/script"
)

// Record holds the Unikemet properties of one code point that Nabu uses.
type Record struct {
	Rune     rune
	Core     string // kEH_Core: C (core), L (legacy) or N (default when absent)
	Func     string // kEH_Func, trimmed
	FVal     string // kEH_FVal, trimmed and NFC-normalized
	JSesh    string // kEH_JSesh (Gardiner-style code)
	Category string // kEH_Cat
}

// Parse reads Unikemet.txt: tab-separated "U+XXXXX<TAB>kEH_Prop<TAB>value"
// lines with "#" comments. The "# UnikemetNNN.txt" line gives the version.
func Parse(r io.Reader) (records map[rune]*Record, version string, err error) {
	records = map[rune]*Record{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if strings.HasPrefix(text, "#") {
			if v, ok := strings.CutPrefix(text, "# Unikemet-"); ok && version == "" {
				version = strings.TrimSuffix(strings.TrimSpace(v), ".txt")
			}
			continue
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		parts := strings.Split(text, "\t")
		if len(parts) != 3 || !strings.HasPrefix(parts[0], "U+") {
			return nil, "", fmt.Errorf("unikemet: line %d: want \"U+XXXX<TAB>property<TAB>value\"", line)
		}
		n, err := strconv.ParseUint(parts[0][2:], 16, 32)
		if err != nil || !utf8.ValidRune(rune(n)) {
			return nil, "", fmt.Errorf("unikemet: line %d: bad code point %q", line, parts[0])
		}
		rec := records[rune(n)]
		if rec == nil {
			rec = &Record{Rune: rune(n), Core: "N"}
			records[rune(n)] = rec
		}
		value := strings.TrimSpace(parts[2])
		switch parts[1] {
		case "kEH_Core":
			rec.Core = value
		case "kEH_Func":
			rec.Func = value
		case "kEH_FVal":
			rec.FVal = norm.NFC.String(value)
		case "kEH_JSesh":
			rec.JSesh = value
		case "kEH_Cat":
			rec.Category = value
		}
	}
	if err := sc.Err(); err != nil {
		return nil, "", fmt.Errorf("unikemet: %w", err)
	}
	if len(records) == 0 {
		return nil, "", fmt.Errorf("unikemet: no records")
	}
	return records, version, nil
}

// Phonemogram is the kEH_Func value of signs used for their sound.
const Phonemogram = "Phonemogram"

// IsUniliteral reports whether rec is a phonemogram whose value is a single
// transliteration character (a base character plus any combining marks).
func IsUniliteral(rec *Record) bool {
	if rec.Func != Phonemogram || rec.FVal == "" {
		return false
	}
	first, size := utf8.DecodeRuneInString(rec.FVal)
	if !unicode.IsLetter(first) {
		return false
	}
	for _, c := range rec.FVal[size:] {
		if !unicode.In(c, unicode.Mn, unicode.Mc, unicode.Me) {
			return false
		}
	}
	return true
}

// Skipped reports a value for which no sign could be chosen.
type Skipped struct {
	Value      string
	Candidates int
	Reason     string
}

// Uniliterals selects one sign per uniliteral value. Unikemet often lists
// several phonemograms for the same value (e.g. both S29 and O34 for "s"),
// so the sign that occurs most often in a corpus (counts, keyed by sign) is
// chosen; ties go to core signs, then to the lower code point. A value
// none of whose candidates occurs in the corpus is skipped rather than
// guessed. Rows are sorted by value.
func Uniliterals(records map[rune]*Record, counts map[rune]int) ([]script.SignRow, []Skipped) {
	byValue := map[string][]*Record{}
	for _, rec := range records {
		if IsUniliteral(rec) {
			byValue[rec.FVal] = append(byValue[rec.FVal], rec)
		}
	}
	var rows []script.SignRow
	var skipped []Skipped
	for value, cands := range byValue {
		sort.Slice(cands, func(i, j int) bool {
			a, b := cands[i], cands[j]
			if counts[a.Rune] != counts[b.Rune] {
				return counts[a.Rune] > counts[b.Rune]
			}
			if (a.Core == "C") != (b.Core == "C") {
				return a.Core == "C"
			}
			return a.Rune < b.Rune
		})
		best := cands[0]
		if counts[best.Rune] == 0 {
			skipped = append(skipped, Skipped{Value: value, Candidates: len(cands), Reason: "no candidate sign occurs in the corpus"})
			continue
		}
		rows = append(rows, script.SignRow{
			Sign: script.Sign{
				Value:     value,
				Sign:      string(best.Rune),
				Codepoint: fmt.Sprintf("U+%04X", best.Rune),
				Gardiner:  best.JSesh,
			},
			Function:    best.Func,
			CorpusCount: counts[best.Rune],
			Candidates:  len(cands),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Value < rows[j].Value })
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Value < skipped[j].Value })
	return rows, skipped
}

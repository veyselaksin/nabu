package tts

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// symbolsFile is tts/symbols.txt, the table shared with the Python trainer.
//
//go:embed symbols.txt
var symbolsFile []byte

const (
	padName   = "<pad>"
	spaceName = "<space>"
)

// SymbolTable maps IPA code points to model token ids. See the header of
// symbols.txt for the file format and the tokenization rules, which the
// Python trainer (tts/train/src/nabu_tts/symbols.py) implements identically.
type SymbolTable struct {
	entries [][]string // entries[id] = canonical symbol followed by aliases
	runes   map[rune]int64
	pad     int64
	space   int64
}

// DefaultSymbols returns the embedded tts/symbols.txt.
func DefaultSymbols() *SymbolTable {
	t, err := ParseSymbols(bytes.NewReader(symbolsFile), "symbols.txt")
	if err != nil {
		panic("tts: embedded symbols.txt is invalid: " + err.Error())
	}
	return t
}

// ParseSymbols reads a symbol table. source labels errors.
func ParseSymbols(r io.Reader, source string) (*SymbolTable, error) {
	t := &SymbolTable{runes: make(map[rune]int64), pad: -1, space: -1}
	sc := bufio.NewScanner(r)
	lineno := 0
	for sc.Scan() {
		lineno++
		var fields []string
		for _, f := range strings.Fields(sc.Text()) {
			if strings.HasPrefix(f, "#") {
				break
			}
			fields = append(fields, f)
		}
		if len(fields) == 0 {
			continue
		}
		id := int64(len(t.entries))
		entry := make([]string, 0, len(fields))
		for _, f := range fields {
			where := fmt.Sprintf("%s:%d", source, lineno)
			switch f {
			case padName, spaceName:
				if (f == padName && t.pad >= 0) || (f == spaceName && t.space >= 0) {
					return nil, fmt.Errorf("tts: %s: %s already defined", where, f)
				}
				if f == padName {
					t.pad = id
				} else {
					t.space = id
				}
				entry = append(entry, f)
				continue
			}
			c, err := parseField(f)
			if err != nil {
				return nil, fmt.Errorf("tts: %s: %w", where, err)
			}
			if s := string(c); norm.NFD.String(s) != s {
				return nil, fmt.Errorf("tts: %s: %q is not NFD-stable; list its parts instead", where, s)
			}
			if prev, dup := t.runes[c]; dup {
				return nil, fmt.Errorf("tts: %s: U+%04X already defined (id %d)", where, c, prev)
			}
			t.runes[c] = id
			entry = append(entry, string(c))
		}
		t.entries = append(t.entries, entry)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("tts: reading %s: %w", source, err)
	}
	if t.pad < 0 || t.space < 0 {
		return nil, fmt.Errorf("tts: %s: must define %s and %s", source, padName, spaceName)
	}
	if t.pad != 0 {
		return nil, fmt.Errorf("tts: %s: %s must be the first entry (id 0)", source, padName)
	}
	return t, nil
}

func parseField(f string) (rune, error) {
	if strings.HasPrefix(f, "U+") && len(f) >= 6 && len(f) <= 8 {
		n, err := strconv.ParseUint(f[2:], 16, 32)
		if err == nil && strings.ToUpper(f[2:]) == f[2:] && utf8.ValidRune(rune(n)) {
			return rune(n), nil
		}
	}
	if utf8.RuneCountInString(f) != 1 {
		return 0, fmt.Errorf("field %q must be one code point, U+XXXX, %s or %s", f, padName, spaceName)
	}
	c, _ := utf8.DecodeRuneInString(f)
	return c, nil
}

// Len is the number of token ids.
func (t *SymbolTable) Len() int { return len(t.entries) }

// PadID is the id of <pad>, also used as the add_blank token.
func (t *SymbolTable) PadID() int64 { return t.pad }

// SHA256 hashes the table's content (ids, symbols, aliases; not comments), in
// the canonical form defined in nabu_tts.symbols.SymbolTable.sha256. Exported
// models record it so that a model never runs with a table it wasn't trained on.
func (t *SymbolTable) SHA256() string {
	var b strings.Builder
	for _, entry := range t.entries {
		for i, s := range entry {
			if i > 0 {
				b.WriteByte('\t')
			}
			if s == padName || s == spaceName {
				b.WriteString(s)
			} else {
				r, _ := utf8.DecodeRuneInString(s)
				fmt.Fprintf(&b, "U+%04X", r)
			}
		}
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// isSpace matches Python's str.isspace(): Go's unicode.IsSpace plus the
// information separators U+001C..U+001F.
func isSpace(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1C && r <= 0x1F) }

// Normalize applies NFD, trims whitespace and collapses each whitespace run to
// a single ASCII space.
func (t *SymbolTable) Normalize(ipa string) string {
	s := norm.NFD.String(ipa)
	var b strings.Builder
	b.Grow(len(s))
	pendingSpace := false
	for _, r := range s {
		if isSpace(r) {
			pendingSpace = b.Len() > 0
			continue
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Tokenize maps an IPA string to token ids. Every code point must be in the
// table; otherwise it returns an *UnknownSymbolError. Nothing is dropped.
func (t *SymbolTable) Tokenize(ipa string) ([]int64, error) {
	if !utf8.ValidString(ipa) {
		return nil, fmt.Errorf("tts: IPA is not valid UTF-8: %q", ipa)
	}
	text := t.Normalize(ipa)
	ids := make([]int64, 0, len(text))
	offset := 0
	for _, r := range text {
		if r == ' ' {
			ids = append(ids, t.space)
		} else if id, ok := t.runes[r]; ok {
			ids = append(ids, id)
		} else {
			return nil, &UnknownSymbolError{Symbol: r, Offset: offset, Input: text}
		}
		offset++
	}
	return ids, nil
}

// Intersperse implements VITS add_blank: <pad> between all tokens and at both ends.
func (t *SymbolTable) Intersperse(ids []int64) []int64 {
	out := make([]int64, 2*len(ids)+1)
	for i := range out {
		out[i] = t.pad
	}
	for i, id := range ids {
		out[2*i+1] = id
	}
	return out
}

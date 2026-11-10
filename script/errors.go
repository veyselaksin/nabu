package script

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoSign is matched (via errors.Is) by every *NoSignError.
var ErrNoSign = errors.New("script: no sign for grapheme")

// NoSignError reports a word that is not in the lexicon and contains a
// grapheme that no sign in the sign list spells.
type NoSignError struct {
	// Input is the NFC-normalized input that was being rendered.
	Input string
	// Word is the word (whitespace-separated token) containing Grapheme.
	Word string
	// Offset is the index, in runes, of Grapheme within Input.
	Offset int
	// Grapheme is the uncovered character together with any combining marks
	// attached to it.
	Grapheme string
}

func (e *NoSignError) Error() string {
	return fmt.Sprintf("script: no sign spells %q (%s) at position %d in %q, and the word %q is not in the lexicon",
		e.Grapheme, codepoints(e.Grapheme), e.Offset, e.Input, e.Word)
}

// Is reports whether target is ErrNoSign.
func (e *NoSignError) Is(target error) bool { return target == ErrNoSign }

// InvalidUTF8Error reports input that is not valid UTF-8.
type InvalidUTF8Error struct{ Input string }

func (e *InvalidUTF8Error) Error() string {
	return fmt.Sprintf("script: input is not valid UTF-8: %q", e.Input)
}

// FormatError reports every problem found in a sign list, lexicon or
// conventions file.
type FormatError struct {
	// Source is the file name or other label the data was loaded from.
	Source   string
	Problems []string
}

func (e *FormatError) Error() string {
	return fmt.Sprintf("script: invalid file %s:\n  - %s", e.Source, strings.Join(e.Problems, "\n  - "))
}

func codepoints(s string) string {
	parts := make([]string, 0, len(s))
	for _, r := range s {
		parts = append(parts, fmt.Sprintf("U+%04X", r))
	}
	return strings.Join(parts, " ")
}

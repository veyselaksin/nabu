package g2p

import (
	"errors"
	"fmt"
	"strings"
)

// ErrUnknownGrapheme is matched (via errors.Is) by every *UnknownGraphemeError.
var ErrUnknownGrapheme = errors.New("g2p: unknown grapheme")

// UnknownGraphemeError reports input that no rule covers.
type UnknownGraphemeError struct {
	// Input is the NFC-normalized input that was being converted.
	Input string
	// Offset is the index, in runes, of Grapheme within Input.
	Offset int
	// Grapheme is the uncovered character together with any combining marks
	// attached to it.
	Grapheme string
}

func (e *UnknownGraphemeError) Error() string {
	return fmt.Sprintf("g2p: unknown grapheme %q (%s) at position %d in %q",
		e.Grapheme, codepoints(e.Grapheme), e.Offset, e.Input)
}

// Is reports whether target is ErrUnknownGrapheme.
func (e *UnknownGraphemeError) Is(target error) bool { return target == ErrUnknownGrapheme }

// InvalidUTF8Error reports input that is not valid UTF-8.
type InvalidUTF8Error struct{ Input string }

func (e *InvalidUTF8Error) Error() string {
	return fmt.Sprintf("g2p: input is not valid UTF-8: %q", e.Input)
}

// ValidationError reports every schema problem found in a rule file.
type ValidationError struct {
	// Source is the file name or other label the rule file was loaded from.
	Source   string
	Problems []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("g2p: invalid rule file %s:\n  - %s", e.Source, strings.Join(e.Problems, "\n  - "))
}

func codepoints(s string) string {
	parts := make([]string, 0, len(s))
	for _, r := range s {
		parts = append(parts, fmt.Sprintf("U+%04X", r))
	}
	return strings.Join(parts, " ")
}

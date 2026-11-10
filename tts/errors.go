package tts

import (
	"errors"
	"fmt"
)

var (
	// ErrUnknownSymbol is matched (via errors.Is) by every *UnknownSymbolError.
	ErrUnknownSymbol = errors.New("tts: unknown IPA symbol")
	// ErrEmptyInput is returned when the IPA contains nothing to speak.
	ErrEmptyInput = errors.New("tts: nothing to speak (empty IPA)")
	// ErrInvalidParams is wrapped by errors about out-of-range speaker or speed.
	ErrInvalidParams = errors.New("tts: invalid synthesis parameters")
	// ErrTooLong is returned when the input exceeds Options.MaxTokens.
	ErrTooLong = errors.New("tts: input too long")
)

// UnknownSymbolError reports an IPA code point that is not in the symbol table.
type UnknownSymbolError struct {
	Symbol rune
	// Offset is the index, in code points, of Symbol in Input.
	Offset int
	// Input is the normalized (NFD, whitespace-collapsed) IPA.
	Input string
}

func (e *UnknownSymbolError) Error() string {
	return fmt.Sprintf("tts: IPA symbol %q (U+%04X) at position %d in %q is not in the model's symbol table (tts/symbols.txt)",
		string(e.Symbol), e.Symbol, e.Offset, e.Input)
}

// Is reports whether target is ErrUnknownSymbol.
func (e *UnknownSymbolError) Is(target error) bool { return target == ErrUnknownSymbol }

package script

import (
	"fmt"
	"io/fs"

	"github.com/veyselaksin/nabu/script/signs"
)

// LoadFS reads a sign list and a conventions file from fsys.
func LoadFS(fsys fs.FS, signList, conventions string) (*SignList, *Conventions, error) {
	f, err := fsys.Open(signList)
	if err != nil {
		return nil, nil, fmt.Errorf("script: %w", err)
	}
	defer f.Close()
	sl, err := ParseSignList(f, signList)
	if err != nil {
		return nil, nil, err
	}
	data, err := fs.ReadFile(fsys, conventions)
	if err != nil {
		return nil, nil, fmt.Errorf("script: %w", err)
	}
	conv, err := ParseConventions(data, conventions)
	if err != nil {
		return nil, nil, err
	}
	if sl.Language != conv.Language {
		return nil, nil, fmt.Errorf("script: %s is for language %q but %s is for %q",
			signList, sl.Language, conventions, conv.Language)
	}
	return sl, conv, nil
}

// Egyptian returns a renderer for Middle Egyptian built from the embedded
// sign list and conventions, plus lex (which may be nil). A lexicon for a
// different language is an error.
func Egyptian(lex *Lexicon) (*Renderer, error) {
	sl, conv, err := LoadFS(signs.FS, signs.EgyptianSignList, signs.EgyptianConventions)
	if err != nil {
		return nil, err
	}
	if lex != nil && lex.Language != sl.Language {
		return nil, fmt.Errorf("script: lexicon %s is for language %q, want %q", lex.Source.ID, lex.Language, sl.Language)
	}
	return NewRenderer(sl, conv, lex), nil
}

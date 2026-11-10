// Package script renders Egyptological transliteration in Unicode
// hieroglyphs.
//
// Each whitespace-separated word is rendered by the first method that works:
//
//  1. Lexicon: if a [Lexicon] built from a corpus is loaded and attests the
//     word form, its most frequent attested spelling is used.
//  2. Phonetic: otherwise the word is spelled sign by sign from a [SignList]
//     of uniliteral (one-consonant) signs, longest value first. This is NOT
//     how a scribe would have written the word; real spellings combine
//     multi-consonant signs, phonetic complements and classifiers. Results
//     say which method produced each word so that callers can label them.
//
// Editorial separators (".", "=", …) are not written, aliases reconcile
// transliteration conventions (both from a [Conventions] file), and any
// grapheme that no sign spells is an [*NoSignError]; characters are never
// silently dropped.
//
// Like g2p, the package has no cgo and builds for GOOS=js GOARCH=wasm.
package script

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Method says how a word was rendered.
type Method string

const (
	// MethodLexicon means the spelling is attested in the lexicon's corpus.
	MethodLexicon Method = "lexicon"
	// MethodPhonetic means the word was spelled sign by sign from uniliteral
	// signs; the spelling is not attested.
	MethodPhonetic Method = "phonetic"
)

// Renderer renders transliteration in hieroglyphs. It is safe for concurrent
// use.
type Renderer struct {
	signs *SignList
	conv  *Conventions
	lex   *Lexicon
	// forms maps canonical lexicon forms to spellings.
	forms map[string]Spelling
}

// NewRenderer returns a renderer. conv and lex may be nil.
func NewRenderer(signs *SignList, conv *Conventions, lex *Lexicon) *Renderer {
	r := &Renderer{signs: signs, conv: conv, lex: lex}
	if lex != nil {
		r.forms = make(map[string]Spelling, len(lex.entries))
		for form, sp := range lex.entries {
			key := conv.canonical(form)
			// Two lexicon forms can collapse onto one canonical form (e.g. one
			// written with an alias); keep the better-attested spelling.
			if prev, ok := r.forms[key]; !ok || sp.Count > prev.Count ||
				(sp.Count == prev.Count && sp.Hieroglyphs < prev.Hieroglyphs) {
				r.forms[key] = sp
			}
		}
	}
	return r
}

// Rendering is the result of [Renderer.Render].
type Rendering struct {
	// Input is the NFC-normalized input.
	Input string `json:"input"`
	// Hieroglyphs is every word's rendering joined by single spaces.
	Hieroglyphs string `json:"hieroglyphs"`
	Words       []Word `json:"words"`
	// Sources lists the data the rendering used, each at most once.
	Sources []Source `json:"sources"`
}

// Word is the rendering of one whitespace-separated word.
type Word struct {
	// Translit is the word as given (NFC).
	Translit    string `json:"translit"`
	Hieroglyphs string `json:"hieroglyphs"`
	Method      Method `json:"method"`
	// Attestation is set for MethodLexicon.
	Attestation *Spelling `json:"attestation,omitempty"`
	// Signs lists the signs used, in order, for MethodPhonetic.
	Signs []Sign `json:"signs,omitempty"`
}

// HasLexicon reports whether a lexicon is loaded.
func (r *Renderer) HasLexicon() bool { return r.lex != nil }

// SignList returns the renderer's sign list.
func (r *Renderer) SignList() *SignList { return r.signs }

// Lexicon returns the renderer's lexicon, or nil.
func (r *Renderer) Lexicon() *Lexicon { return r.lex }

// Render renders transliterated text. It returns an [*InvalidUTF8Error] for
// invalid input and a [*NoSignError] if a word is neither in the lexicon nor
// spellable from the sign list.
func (r *Renderer) Render(text string) (*Rendering, error) {
	if !utf8.ValidString(text) {
		return nil, &InvalidUTF8Error{Input: text}
	}
	s := norm.NFC.String(text)
	out := &Rendering{Input: s, Words: []Word{}, Sources: []Source{}}
	usedSigns, usedLex := false, false

	for start := 0; start < len(s); {
		c, size := utf8.DecodeRuneInString(s[start:])
		if isSpace(c) {
			start += size
			continue
		}
		end := start
		for end < len(s) {
			c, size := utf8.DecodeRuneInString(s[end:])
			if isSpace(c) {
				break
			}
			end += size
		}
		word, err := r.renderWord(s, start, end)
		if err != nil {
			return nil, err
		}
		if word.Method == MethodLexicon {
			usedLex = true
		} else if len(word.Signs) > 0 {
			usedSigns = true
		}
		out.Words = append(out.Words, word)
		start = end
	}

	glyphs := make([]string, 0, len(out.Words))
	for _, w := range out.Words {
		if w.Hieroglyphs != "" {
			glyphs = append(glyphs, w.Hieroglyphs)
		}
	}
	out.Hieroglyphs = strings.Join(glyphs, " ")
	if usedLex {
		out.Sources = append(out.Sources, r.lex.Source)
	}
	if usedSigns {
		out.Sources = append(out.Sources, r.signs.Source)
	}
	return out, nil
}

// renderWord renders s[start:end], a single word of the NFC input s.
func (r *Renderer) renderWord(s string, start, end int) (Word, error) {
	word := s[start:end]
	if r.forms != nil {
		if sp, ok := r.forms[r.conv.canonical(word)]; ok {
			return Word{Translit: word, Hieroglyphs: sp.Hieroglyphs, Method: MethodLexicon, Attestation: &sp}, nil
		}
	}

	w := Word{Translit: word, Method: MethodPhonetic, Signs: []Sign{}}
	var glyphs strings.Builder
	for i := start; i < end; {
		if n := r.conv.separatorAt(s[:end], i); n > 0 {
			i += n
			continue
		}
		// Longest match over sign values, either spelled directly or via an
		// alias. Aliases are tried first so that an alias longer than any
		// sign value wins.
		value, n := "", 0
		if a, ok := r.conv.aliasAt(s[:end], i); ok {
			value, n = a.to, len(a.from)
		}
		if v, m := r.longestValue(s[:end], i); m > n {
			value, n = v, m
		}
		sign, ok := r.signs.Lookup(value)
		if n == 0 || !ok {
			return Word{}, &NoSignError{
				Input:    s,
				Word:     word,
				Offset:   utf8.RuneCountInString(s[:i]),
				Grapheme: cluster(s[i:end]),
			}
		}
		glyphs.WriteString(sign.Sign)
		w.Signs = append(w.Signs, sign)
		i += n
	}
	w.Hieroglyphs = glyphs.String()
	return w, nil
}

// longestValue returns the longest sign value that is a prefix of s[i:] and
// does not end inside a combining-character sequence.
func (r *Renderer) longestValue(s string, i int) (string, int) {
	rest := s[i:]
	end := 0
	for n := 0; n < r.signs.maxRunes && end < len(rest); n++ {
		_, size := utf8.DecodeRuneInString(rest[end:])
		end += size
	}
	for ; end > 0; end = prevBoundary(rest, end) {
		if splitsCluster(s, i+end) {
			continue
		}
		if _, ok := r.signs.byValue[rest[:end]]; ok {
			return rest[:end], end
		}
	}
	return "", 0
}

func prevBoundary(s string, i int) int {
	_, size := utf8.DecodeLastRuneInString(s[:i])
	return i - size
}

// splitsCluster reports whether byte offset i of s falls between a base
// character and a combining mark that belongs to it.
func splitsCluster(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	c, _ := utf8.DecodeRuneInString(s[i:])
	return isMark(c)
}

// cluster returns the first rune of s plus any combining marks that follow it.
func cluster(s string) string {
	_, n := utf8.DecodeRuneInString(s)
	for n < len(s) {
		c, size := utf8.DecodeRuneInString(s[n:])
		if !isMark(c) {
			break
		}
		n += size
	}
	return s[:n]
}

func isMark(c rune) bool  { return unicode.In(c, unicode.Mn, unicode.Mc, unicode.Me) }
func isSpace(c rune) bool { return unicode.IsSpace(c) }

// Package g2p converts transliterated text into IPA using declarative YAML
// rule files, one file per scholarly pronunciation reconstruction.
//
// The engine is deliberately small and portable: it has no cgo and no
// OS-specific code paths beyond the optional [Load] helper (which only uses
// os.ReadFile), so it builds for GOOS=js GOARCH=wasm.
//
// Conversion algorithm:
//
//  1. The input is normalized to Unicode NFC, so precomposed and decomposed
//     spellings of the same character (e.g. "ḫ" U+1E2B vs "h"+U+032E) are
//     treated identically.
//  2. At each position, rules are tried longest match first (measured in
//     runes); ties keep file order. A rule applies only if its optional
//     left/right context matches and the match does not end in the middle of
//     a combining-character sequence.
//  3. Whitespace that no rule matches is copied through unchanged.
//  4. Anything else is an [*UnknownGraphemeError]; characters are never
//     silently dropped. (A rule may explicitly map a grapheme to "".)
package g2p

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// RuleSet is a compiled, validated rule file. It is safe for concurrent use.
type RuleSet struct {
	ID        string
	Language  string
	Name      string
	Reference string
	Version   string
	Notes     string

	// byFirst indexes rules by the first rune of their match string; each
	// slice is sorted longest-match-first, preserving file order for ties.
	byFirst map[rune][]*rule
}

type rule struct {
	match  string
	ipa    string
	before *regexp.Regexp // matched against the text to the left; anchored at its end
	after  *regexp.Regexp // matched against the text to the right; anchored at its start
	index  int            // position in the file, for stable ordering and diagnostics
}

// ToIPA converts transliterated text to IPA.
//
// It returns an [*UnknownGraphemeError] (which matches [ErrUnknownGrapheme]
// via errors.Is) if any part of the input is not covered by a rule, and an
// [*InvalidUTF8Error] if the input is not valid UTF-8.
func (r *RuleSet) ToIPA(translit string) (string, error) {
	if !utf8.ValidString(translit) {
		return "", &InvalidUTF8Error{Input: translit}
	}
	s := norm.NFC.String(translit)

	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		c, size := utf8.DecodeRuneInString(s[i:])
		if rl := r.find(s, i, c); rl != nil {
			out.WriteString(rl.ipa)
			i += len(rl.match)
			continue
		}
		if unicode.IsSpace(c) {
			out.WriteRune(c)
			i += size
			continue
		}
		return "", &UnknownGraphemeError{
			Input:    s,
			Offset:   utf8.RuneCountInString(s[:i]),
			Grapheme: cluster(s[i:]),
		}
	}
	return out.String(), nil
}

// find returns the first applicable rule at byte offset i, or nil.
func (r *RuleSet) find(s string, i int, first rune) *rule {
	rest := s[i:]
	for _, rl := range r.byFirst[first] {
		if !strings.HasPrefix(rest, rl.match) {
			continue
		}
		end := i + len(rl.match)
		// Do not split a base character from its combining marks: "h" must
		// not match the first half of an uncomposable "h" + U+032D.
		if next, _ := utf8.DecodeRuneInString(s[end:]); end < len(s) && isMark(next) {
			continue
		}
		if rl.before != nil && !rl.before.MatchString(s[:i]) {
			continue
		}
		if rl.after != nil && !rl.after.MatchString(s[end:]) {
			continue
		}
		return rl
	}
	return nil
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

func isMark(c rune) bool { return unicode.In(c, unicode.Mn, unicode.Mc, unicode.Me) }

func newRuleSet(meta fileRuleSet, rules []*rule) *RuleSet {
	rs := &RuleSet{
		ID:        meta.ID,
		Language:  meta.Language,
		Name:      meta.Name,
		Reference: meta.Reference,
		Version:   meta.Version,
		Notes:     meta.Notes,
		byFirst:   make(map[rune][]*rule),
	}
	for _, rl := range rules {
		c, _ := utf8.DecodeRuneInString(rl.match)
		rs.byFirst[c] = append(rs.byFirst[c], rl)
	}
	for _, list := range rs.byFirst {
		sort.SliceStable(list, func(a, b int) bool {
			return utf8.RuneCountInString(list[a].match) > utf8.RuneCountInString(list[b].match)
		})
	}
	return rs
}

// String implements fmt.Stringer.
func (r *RuleSet) String() string { return fmt.Sprintf("%s@%s", r.ID, r.Version) }

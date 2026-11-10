package script

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
	"golang.org/x/text/unicode/norm"
)

// Conventions reconcile transliteration conventions between user input, the
// sign list and the lexicon. They are a small, reviewable YAML file (see
// signs/README.md) rather than code, because each entry is an editorial
// decision that should cite its justification.
type Conventions struct {
	Language string

	// separators are editorial marks (morpheme boundaries and the like) that
	// are not written with any sign.
	separators []string
	// aliases map an input spelling to the canonical spelling used by the
	// sign list and lexicon, e.g. "q" → "ḳ". Sorted longest first.
	aliases []alias
}

type alias struct{ from, to string }

type fileConventions struct {
	Language   string          `yaml:"language"`
	Separators []fileSeparator `yaml:"separators"`
	Aliases    []fileAlias     `yaml:"aliases"`
}

type fileSeparator struct {
	Match string `yaml:"match"`
	Note  string `yaml:"note"`
}

type fileAlias struct {
	Match     string `yaml:"match"`
	Canonical string `yaml:"canonical"`
	Note      string `yaml:"note"`
}

// LoadConventions reads a conventions file from disk.
func LoadConventions(filename string) (*Conventions, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("script: %w", err)
	}
	return ParseConventions(data, filename)
}

// ParseConventions decodes and validates a conventions file. Unknown keys are
// rejected, and every entry must carry a note explaining it.
func ParseConventions(data []byte, source string) (*Conventions, error) {
	var f fileConventions
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, &FormatError{Source: source, Problems: []string{"file is empty"}}
		}
		return nil, &FormatError{Source: source, Problems: []string{err.Error()}}
	}
	var problems []string
	addf := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	checkText := func(where, s string) {
		switch {
		case s == "":
			addf("%s: required", where)
		case !utf8.ValidString(s):
			addf("%s: not valid UTF-8", where)
		case !norm.NFC.IsNormalString(s):
			addf("%s: must be NFC-normalized (expected %q)", where, norm.NFC.String(s))
		case strings.ContainsFunc(s, isSpace):
			addf("%s: %q must not contain whitespace", where, s)
		}
	}
	if f.Language == "" {
		addf("language: required")
	}
	c := &Conventions{Language: f.Language}
	seen := map[string]string{}
	for i, s := range f.Separators {
		where := fmt.Sprintf("separators[%d]", i)
		checkText(where+".match", s.Match)
		if strings.TrimSpace(s.Note) == "" {
			addf("%s.note: required (explain why this mark is not written)", where)
		}
		if prev, dup := seen[s.Match]; dup {
			addf("%s: %q already defined in %s", where, s.Match, prev)
		}
		seen[s.Match] = where
		c.separators = append(c.separators, s.Match)
	}
	for i, a := range f.Aliases {
		where := fmt.Sprintf("aliases[%d]", i)
		checkText(where+".match", a.Match)
		checkText(where+".canonical", a.Canonical)
		if strings.TrimSpace(a.Note) == "" {
			addf("%s.note: required (cite why these spellings are equivalent)", where)
		}
		if a.Match == a.Canonical && a.Match != "" {
			addf("%s: match and canonical are the same", where)
		}
		if prev, dup := seen[a.Match]; dup {
			addf("%s: %q already defined in %s", where, a.Match, prev)
		}
		seen[a.Match] = where
		c.aliases = append(c.aliases, alias{from: a.Match, to: a.Canonical})
	}
	if len(problems) > 0 {
		return nil, &FormatError{Source: source, Problems: problems}
	}
	sort.SliceStable(c.aliases, func(i, j int) bool {
		return utf8.RuneCountInString(c.aliases[i].from) > utf8.RuneCountInString(c.aliases[j].from)
	})
	return c, nil
}

// canonical rewrites aliases in an NFC word to their canonical spelling,
// leaving everything else untouched. Like the renderer, it never splits a
// base character from its combining marks.
func (c *Conventions) canonical(word string) string {
	if c == nil || len(c.aliases) == 0 {
		return word
	}
	var b strings.Builder
	for i := 0; i < len(word); {
		if a, ok := c.aliasAt(word, i); ok {
			b.WriteString(a.to)
			i += len(a.from)
			continue
		}
		_, size := utf8.DecodeRuneInString(word[i:])
		b.WriteString(word[i : i+size])
		i += size
	}
	return b.String()
}

func (c *Conventions) aliasAt(s string, i int) (alias, bool) {
	if c == nil {
		return alias{}, false
	}
	for _, a := range c.aliases {
		if strings.HasPrefix(s[i:], a.from) && !splitsCluster(s, i+len(a.from)) {
			return a, true
		}
	}
	return alias{}, false
}

// separatorAt returns the length in bytes of the longest separator at s[i:],
// or 0.
func (c *Conventions) separatorAt(s string, i int) int {
	if c == nil {
		return 0
	}
	best := 0
	for _, sep := range c.separators {
		if len(sep) > best && strings.HasPrefix(s[i:], sep) && !splitsCluster(s, i+len(sep)) {
			best = len(sep)
		}
	}
	return best
}

package g2p

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
	"golang.org/x/text/unicode/norm"
)

// fileRuleSet mirrors the on-disk YAML schema. See rules/README.md.
type fileRuleSet struct {
	ID        string              `yaml:"id"`
	Language  string              `yaml:"language"`
	Name      string              `yaml:"name"`
	Reference string              `yaml:"reference"`
	Version   string              `yaml:"version"`
	Classes   map[string][]string `yaml:"classes"`
	Graphemes []fileGrapheme      `yaml:"graphemes"`
	Notes     string              `yaml:"notes"`
}

type fileGrapheme struct {
	Match   string       `yaml:"match"`
	IPA     *string      `yaml:"ipa"` // pointer so a missing key differs from ipa: ""
	Context *fileContext `yaml:"context"`
}

type fileContext struct {
	Before string `yaml:"before"`
	After  string `yaml:"after"`
}

var (
	idPattern       = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	languagePattern = regexp.MustCompile(`^[a-z]{2,3}(?:-[A-Za-z0-9]{1,8})*$`)
	classPattern    = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*$`)
	classRef        = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)
)

// Load reads and validates a rule file from disk.
func Load(filename string) (*RuleSet, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("g2p: %w", err)
	}
	return Parse(data, filename)
}

// LoadFS reads and validates every *.yaml file in the root of fsys, sorted by
// file name. Rule set IDs must be unique.
func LoadFS(fsys fs.FS) ([]*RuleSet, error) {
	names, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, fmt.Errorf("g2p: %w", err)
	}
	sort.Strings(names)
	seen := make(map[string]string, len(names))
	sets := make([]*RuleSet, 0, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("g2p: %w", err)
		}
		rs, err := Parse(data, name)
		if err != nil {
			return nil, err
		}
		if prev, dup := seen[rs.ID]; dup {
			return nil, fmt.Errorf("g2p: duplicate rule set id %q in %s and %s", rs.ID, prev, name)
		}
		seen[rs.ID] = name
		sets = append(sets, rs)
	}
	return sets, nil
}

// Parse decodes and validates a rule file. source labels the data in error
// messages (typically its file name). Unknown YAML keys are rejected.
func Parse(data []byte, source string) (*RuleSet, error) {
	var f fileRuleSet
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, &ValidationError{Source: source, Problems: []string{"file is empty"}}
		}
		return nil, &ValidationError{Source: source, Problems: []string{err.Error()}}
	}

	v := validator{}
	v.required("id", f.ID)
	v.required("language", f.Language)
	v.required("name", f.Name)
	v.required("reference", f.Reference)
	v.required("version", f.Version)
	if f.ID != "" && !idPattern.MatchString(f.ID) {
		v.addf("id %q must be lowercase kebab-case (e.g. egy-allen)", f.ID)
	}
	if f.Language != "" && !languagePattern.MatchString(f.Language) {
		v.addf("language %q must be an ISO 639 code such as egy, sux or hit", f.Language)
	}

	classes := make(map[string]string, len(f.Classes))
	for name, members := range f.Classes {
		if !classPattern.MatchString(name) {
			v.addf("classes.%s: class names must start with an uppercase letter", name)
		}
		if len(members) == 0 {
			v.addf("classes.%s: must have at least one member", name)
		}
		alts := make([]string, 0, len(members))
		for _, m := range members {
			if m == "" {
				v.addf("classes.%s: members must not be empty", name)
				continue
			}
			v.nfc(fmt.Sprintf("classes.%s member %q", name, m), m)
			alts = append(alts, m)
		}
		// Longest first so alternation prefers the longest member.
		sort.SliceStable(alts, func(a, b int) bool {
			return utf8.RuneCountInString(alts[a]) > utf8.RuneCountInString(alts[b])
		})
		for i, a := range alts {
			alts[i] = regexp.QuoteMeta(a)
		}
		classes[name] = "(?:" + strings.Join(alts, "|") + ")"
	}

	if len(f.Graphemes) == 0 {
		v.addf("graphemes: must contain at least one rule")
	}
	type key struct{ match, before, after string }
	seen := make(map[key]int, len(f.Graphemes))
	rules := make([]*rule, 0, len(f.Graphemes))
	for i, g := range f.Graphemes {
		where := fmt.Sprintf("graphemes[%d]", i)
		if g.Match == "" {
			v.addf("%s.match: required", where)
			continue
		}
		where = fmt.Sprintf("graphemes[%d] (%q)", i, g.Match)
		v.nfc(where+".match", g.Match)
		if first, _ := utf8.DecodeRuneInString(g.Match); isMark(first) {
			v.addf("%s.match: must not start with a combining mark", where)
		}
		if g.IPA == nil {
			v.addf("%s.ipa: required (use ipa: \"\" to map explicitly to nothing)", where)
			continue
		}
		v.nfc(where+".ipa", *g.IPA)

		rl := &rule{match: g.Match, ipa: *g.IPA, index: i}
		var ctx fileContext
		if g.Context != nil {
			ctx = *g.Context
			if ctx.Before == "" && ctx.After == "" {
				v.addf("%s.context: must set before and/or after", where)
			}
			rl.before = v.contextRegexp(where+".context.before", ctx.Before, classes, "(?:", ")$")
			rl.after = v.contextRegexp(where+".context.after", ctx.After, classes, "^(?:", ")")
		}
		k := key{g.Match, ctx.Before, ctx.After}
		if prev, dup := seen[k]; dup {
			v.addf("%s: duplicates graphemes[%d] (same match and context)", where, prev)
		}
		seen[k] = i
		rules = append(rules, rl)
	}

	if len(v.problems) > 0 {
		return nil, &ValidationError{Source: source, Problems: v.problems}
	}
	return newRuleSet(f, rules), nil
}

type validator struct{ problems []string }

func (v *validator) addf(format string, args ...any) {
	v.problems = append(v.problems, fmt.Sprintf(format, args...))
}

func (v *validator) required(field, value string) {
	if strings.TrimSpace(value) == "" {
		v.addf("%s: required", field)
	}
}

func (v *validator) nfc(where, s string) {
	if !utf8.ValidString(s) {
		v.addf("%s: not valid UTF-8", where)
	} else if !norm.NFC.IsNormalString(s) {
		v.addf("%s: must be NFC-normalized (expected %q)", where, norm.NFC.String(s))
	}
}

// contextRegexp expands {Class} references and compiles the pattern wrapped
// in prefix/suffix anchors. It returns nil for an empty pattern.
func (v *validator) contextRegexp(where, pattern string, classes map[string]string, prefix, suffix string) *regexp.Regexp {
	if pattern == "" {
		return nil
	}
	expanded := classRef.ReplaceAllStringFunc(pattern, func(ref string) string {
		name := ref[1 : len(ref)-1]
		alt, ok := classes[name]
		if !ok {
			v.addf("%s: undefined class {%s}", where, name)
			return ref
		}
		return alt
	})
	re, err := regexp.Compile(prefix + expanded + suffix)
	if err != nil {
		v.addf("%s: invalid regular expression %q: %v", where, pattern, err)
		return nil
	}
	return re
}

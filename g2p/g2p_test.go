package g2p_test

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veyselaksin/nabu/g2p"
	"github.com/veyselaksin/nabu/g2p/rules"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/golden from current output")

func mustLoad(t *testing.T, path string) *g2p.RuleSet {
	t.Helper()
	rs, err := g2p.Load(path)
	if err != nil {
		t.Fatalf("Load(%s): %v", path, err)
	}
	return rs
}

// TestRuleFiles validates every shipped rule file against the schema.
func TestRuleFiles(t *testing.T) {
	if yml, _ := filepath.Glob("rules/*.yml"); len(yml) > 0 {
		t.Fatalf("rule files must use the .yaml extension (only *.yaml is embedded): %v", yml)
	}
	paths, err := filepath.Glob("rules/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no rule files found in rules/")
	}
	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			rs := mustLoad(t, p)
			if want := strings.TrimSuffix(filepath.Base(p), ".yaml"); rs.ID != want {
				t.Errorf("id = %q, want %q (must match file name)", rs.ID, want)
			}
			if _, err := os.Stat(goldenPath(rs.ID)); err != nil {
				t.Errorf("missing golden file %s (run: go test ./g2p -run TestGolden -update)", goldenPath(rs.ID))
			}
		})
	}

	embedded, err := g2p.LoadFS(rules.FS)
	if err != nil {
		t.Fatalf("LoadFS(rules.FS): %v", err)
	}
	if len(embedded) != len(paths) {
		t.Errorf("embedded %d rule sets, found %d on disk", len(embedded), len(paths))
	}
}

func goldenPath(id string) string { return filepath.Join("testdata", "golden", id+".tsv") }

// TestGolden checks each rule file against testdata/golden/<id>.tsv, whose
// lines are "input<TAB>expected IPA". Lines starting with # are comments.
func TestGolden(t *testing.T) {
	paths, _ := filepath.Glob("rules/*.yaml")
	for _, p := range paths {
		rs := mustLoad(t, p)
		t.Run(rs.ID, func(t *testing.T) {
			gp := goldenPath(rs.ID)
			lines, err := readLines(gp)
			if err != nil {
				t.Skipf("no golden file: %v", err)
			}
			var out []string
			for n, line := range lines {
				if line == "" || strings.HasPrefix(line, "#") {
					out = append(out, line)
					continue
				}
				input, want, _ := strings.Cut(line, "\t")
				got, err := rs.ToIPA(input)
				if err != nil {
					t.Errorf("%s:%d: ToIPA(%q): %v", gp, n+1, input, err)
					out = append(out, line)
					continue
				}
				out = append(out, input+"\t"+got)
				if !*update && got != want {
					t.Errorf("%s:%d: ToIPA(%q) = %q, want %q", gp, n+1, input, got, want)
				}
			}
			if *update {
				if err := os.WriteFile(gp, []byte(strings.Join(out, "\n")+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

func TestToIPA(t *testing.T) {
	graphemes := mustLoad(t, "testdata/rules/test-graphemes.yaml")
	context := mustLoad(t, "testdata/rules/test-context.yaml")

	tests := []struct {
		name  string
		rules *g2p.RuleSet
		in    string
		want  string
	}{
		// Each Egyptological character is one grapheme, distinct from its base letter.
		{"aleph", graphemes, "ꜣ", "<3>"},
		{"ayin", graphemes, "ꜥ", "<c>"},
		{"h dot below", graphemes, "ḥ", "<H>"},
		{"h breve below", graphemes, "ḫ", "<x>"},
		{"h line below", graphemes, "ẖ", "<X>"},
		{"s caron", graphemes, "š", "<S>"},
		{"t line below", graphemes, "ṯ", "<T>"},
		{"d line below", graphemes, "ḏ", "<D>"},
		{"q", graphemes, "q", "<q>"},
		{"k dot below", graphemes, "ḳ", "<K>"},
		{"plain letters", graphemes, "hstdkn", "hstdkn"},
		{"mixed word", graphemes, "ꜥnḫ", "<c>n<x>"},
		{"explicit empty mapping", graphemes, "ḥt-n", "<H>tn"},

		// NFC: decomposed input must behave exactly like precomposed input.
		{"decomposed h breve below", graphemes, "ḫ", "<x>"},
		{"decomposed h dot below", graphemes, "ḥ", "<H>"},
		{"decomposed h line below", graphemes, "ẖ", "<X>"},
		{"decomposed s caron", graphemes, "š", "<S>"},
		{"decomposed t line below", graphemes, "ṯ", "<T>"},
		{"decomposed d line below", graphemes, "ḏ", "<D>"},
		{"decomposed k dot below", graphemes, "ḳ", "<K>"},
		{"decomposed in word", graphemes, "ꜥnḫ", "<c>n<x>"},

		// Whitespace is preserved verbatim.
		{"whitespace", graphemes, " ꜥn\tḫ\n", " <c>n\t<x>\n"},
		{"empty", graphemes, "", ""},

		// Longest match first.
		{"longest abc", context, "abc", "[ABC]"},
		{"longest ab", context, "ab", "[AB]"},
		{"longest falls back", context, "aab", "a[AB]"},

		// Contexts and classes.
		{"after class", context, "ca", "Ka"},
		{"after class multi-char member", context, "cee", "Kee"},
		{"before literal", context, "xc", "xQ"},
		{"at word end", context, "bc b", "bZ b"},
		{"at input end", context, "bc", "bZ"},
		{"default", context, "bcb", "bCb"},
		{"rule order beats context order", context, "xca", "xKa"},
		{"tie keeps file order", context, "y", "first"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.rules.ToIPA(tt.in)
			if err != nil {
				t.Fatalf("ToIPA(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ToIPA(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestToIPAUnknownGrapheme(t *testing.T) {
	rs := mustLoad(t, "testdata/rules/test-graphemes.yaml")
	tests := []struct {
		name     string
		in       string
		grapheme string
		offset   int
	}{
		{"unknown letter", "ꜥnz", "z", 2},
		{"unknown first", "zꜥ", "z", 0},
		{"uppercase is not folded", "Ḫ", "Ḫ", 0},
		// h + combining circumflex below has no precomposed form: the base
		// must not be consumed alone and the mark dropped.
		{"uncomposable mark", "nh̭", "h̭", 1},
		{"stray leading mark", "̱n", "̱", 0},
		{"lookalike yogh is not aleph", "ȝ", "ȝ", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := rs.ToIPA(tt.in)
			if err == nil {
				t.Fatalf("ToIPA(%q) = %q, want error", tt.in, got)
			}
			if !errors.Is(err, g2p.ErrUnknownGrapheme) {
				t.Fatalf("errors.Is(err, ErrUnknownGrapheme) = false; err = %v", err)
			}
			var ue *g2p.UnknownGraphemeError
			if !errors.As(err, &ue) {
				t.Fatalf("error is %T, want *UnknownGraphemeError", err)
			}
			if ue.Grapheme != tt.grapheme || ue.Offset != tt.offset {
				t.Errorf("got grapheme %q at %d, want %q at %d", ue.Grapheme, ue.Offset, tt.grapheme, tt.offset)
			}
			if got != "" {
				t.Errorf("partial output %q returned alongside error", got)
			}
		})
	}
}

func TestToIPAInvalidUTF8(t *testing.T) {
	rs := mustLoad(t, "testdata/rules/test-graphemes.yaml")
	_, err := rs.ToIPA("n\xffn")
	var ie *g2p.InvalidUTF8Error
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v (%T), want *InvalidUTF8Error", err, err)
	}
}

func TestParseValidation(t *testing.T) {
	const header = "id: x\nlanguage: egy\nname: X\nreference: R\nversion: \"1\"\n"
	tests := []struct {
		name string
		yaml string
		want string // substring of the error
	}{
		{"empty file", "", "file is empty"},
		{"missing fields", "graphemes: [{match: a, ipa: a}]\n", "id: required"},
		{"missing reference", "id: x\nlanguage: egy\nname: X\nversion: \"1\"\ngraphemes: [{match: a, ipa: a}]\n", "reference: required"},
		{"bad id", strings.Replace(header, "id: x", "id: Bad_ID", 1) + "graphemes: [{match: a, ipa: a}]\n", "kebab-case"},
		{"bad language", strings.Replace(header, "language: egy", "language: Egyptian", 1) + "graphemes: [{match: a, ipa: a}]\n", "ISO 639"},
		{"unknown key", header + "graphemes: [{match: a, ipa: a, ipaa: b}]\n", "ipaa"},
		{"unknown top-level key", header + "author: me\ngraphemes: [{match: a, ipa: a}]\n", "author"},
		{"no graphemes", header, "at least one rule"},
		{"empty match", header + "graphemes: [{match: \"\", ipa: a}]\n", "match: required"},
		{"missing ipa", header + "graphemes: [{match: a}]\n", "ipa: required"},
		{"not NFC", header + "graphemes: [{match: \"ḫ\", ipa: x}]\n", "NFC"},
		{"leading mark", header + "graphemes: [{match: \"̭\", ipa: x}]\n", "combining mark"},
		{"duplicate", header + "graphemes: [{match: a, ipa: a}, {match: a, ipa: b}]\n", "duplicates graphemes[0]"},
		{"empty context", header + "graphemes: [{match: a, ipa: a, context: {}}]\n", "before and/or after"},
		{"bad regex", header + "graphemes: [{match: a, ipa: a, context: {after: \"(\"}}]\n", "invalid regular expression"},
		{"undefined class", header + "graphemes: [{match: a, ipa: a, context: {after: \"{V}\"}}]\n", "undefined class {V}"},
		{"bad class name", header + "classes: {v: [a]}\ngraphemes: [{match: a, ipa: a}]\n", "uppercase"},
		{"empty class", header + "classes: {V: []}\ngraphemes: [{match: a, ipa: a}]\n", "at least one member"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := g2p.Parse([]byte(tt.yaml), "test.yaml")
			if err == nil {
				t.Fatalf("Parse succeeded, want error containing %q", tt.want)
			}
			var ve *g2p.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("error is %T, want *ValidationError: %v", err, err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestLoadFSDuplicateIDs(t *testing.T) {
	dir := t.TempDir()
	doc := "id: same\nlanguage: egy\nname: X\nreference: R\nversion: \"1\"\ngraphemes: [{match: a, ipa: a}]\n"
	for _, name := range []string{"a.yaml", "b.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := g2p.LoadFS(os.DirFS(dir)); err == nil || !strings.Contains(err.Error(), "duplicate rule set id") {
		t.Fatalf("err = %v, want duplicate id error", err)
	}
}

func ExampleRuleSet_ToIPA() {
	rs, err := g2p.Load("testdata/rules/test-graphemes.yaml")
	if err != nil {
		panic(err)
	}
	ipa, err := rs.ToIPA("ꜥnḫ")
	fmt.Println(ipa, err)
	_, err = rs.ToIPA("ꜥnz")
	fmt.Println(err)
	// Output:
	// <c>n<x> <nil>
	// g2p: unknown grapheme "z" (U+007A) at position 2 in "ꜥnz"
}

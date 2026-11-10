package script_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/veyselaksin/nabu/script"
	"github.com/veyselaksin/nabu/script/signs"
)

func egyptian(t *testing.T, lex *script.Lexicon) *script.Renderer {
	t.Helper()
	r, err := script.Egyptian(lex)
	if err != nil {
		t.Fatalf("Egyptian: %v", err)
	}
	return r
}

// testLexicon is a synthetic fixture: its spellings are arbitrary signs, not
// attested ones (real lexicons are CC BY-SA and are never committed).
func testLexicon(t *testing.T) *script.Lexicon {
	t.Helper()
	lx, err := script.LoadLexicon("testdata/lexicon.tsv")
	if err != nil {
		t.Fatalf("LoadLexicon: %v", err)
	}
	return lx
}

// TestEmbedded checks the shipped sign list and conventions.
func TestEmbedded(t *testing.T) {
	r := egyptian(t, nil)
	sl := r.SignList()
	if sl.Language != "egy" || sl.Source.License != "Unicode-3.0" || sl.Source.URL == "" {
		t.Errorf("unexpected sign list metadata: %+v (%s)", sl.Source, sl.Language)
	}
	if sl.Len() == 0 {
		t.Fatal("empty sign list")
	}
	// Every shipped sign must be in the basic Egyptian Hieroglyphs block, which
	// the Noto Sans Egyptian Hieroglyphs font used by the web demo covers.
	data, err := fs.ReadFile(signs.FS, signs.EgyptianSignList)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 3 || strings.HasPrefix(line, "#") || f[0] == "value" {
			continue
		}
		c := []rune(f[2])[0]
		if c < 0x13000 || c > 0x1342F {
			t.Errorf("value %q: sign %s (%s) is outside U+13000–U+1342F", f[0], f[2], f[1])
		}
	}
	if _, err := os.Stat("signs/LICENSE-unicode.txt"); err != nil {
		t.Errorf("the Unicode license must ship next to the sign list: %v", err)
	}
}

func TestRenderPhonetic(t *testing.T) {
	r := egyptian(t, nil)
	tests := []struct {
		in, want string
	}{
		{"ꜥnḫ", "𓂝𓈖𓐍"},
		{"ḥtp", "𓎛𓏏𓊪"},
		// Separators are not written.
		{"ꜥnḫ.t", "𓂝𓈖𓐍𓏏"},
		{"ḏd.ꞽn =f", "𓆓𓂧𓇋𓈖 𓆑"},
		{"nm.tꞽ-nḫt", "𓈖𓅓𓏏𓇋𓈖𓐍𓏏"},
		// Alias: q is written with the sign whose Unikemet value is ḳ.
		{"qd", "𓈎𓂧"},
		{"ḳd", "𓈎𓂧"},
		// Whitespace is normalized to single spaces; words keep their order.
		{"  ꜥnḫ \t ḥtp\n", "𓂝𓈖𓐍 𓎛𓏏𓊪"},
		// Decomposed input is NFC-normalized: h + U+032E is ḫ.
		{"ꜥnḫ", "𓂝𓈖𓐍"},
		{"", ""},
		// A word made only of separators renders as nothing.
		{"=", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := r.Render(tt.in)
			if err != nil {
				t.Fatalf("Render(%q): %v", tt.in, err)
			}
			if got.Hieroglyphs != tt.want {
				t.Errorf("Render(%q) = %q, want %q", tt.in, got.Hieroglyphs, tt.want)
			}
			for _, w := range got.Words {
				if w.Method != script.MethodPhonetic || w.Attestation != nil {
					t.Errorf("word %q: method %s, attestation %v; want phonetic without attestation", w.Translit, w.Method, w.Attestation)
				}
			}
			if tt.want != "" && (len(got.Sources) != 1 || got.Sources[0].ID != "egy-unikemet-uniliterals") {
				t.Errorf("sources = %+v, want the sign list only", got.Sources)
			}
		})
	}
}

func TestRenderSigns(t *testing.T) {
	got, err := egyptian(t, nil).Render("qn")
	if err != nil {
		t.Fatal(err)
	}
	w := got.Words[0]
	if len(w.Signs) != 2 || w.Signs[0].Value != "ḳ" || w.Signs[0].Gardiner != "N29" || w.Signs[0].Codepoint != "U+1320E" {
		t.Errorf("signs = %+v", w.Signs)
	}
	if w.Translit != "qn" {
		t.Errorf("translit = %q, want the input spelling", w.Translit)
	}
}

func TestRenderErrors(t *testing.T) {
	r := egyptian(t, nil)
	tests := []struct {
		in       string
		grapheme string
		offset   int
		word     string
	}{
		{"zꜣ", "z", 0, "zꜣ"},
		{"ꜥnḫ xyz", "x", 4, "xyz"},
		// A combining mark stays with its base character in the error.
		{"ꜥni̯", "i̯", 2, "ꜥni̯"},
		// Editorial brackets are not separators.
		{"ḥtp(.w)", "(", 3, "ḥtp(.w)"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			_, err := r.Render(tt.in)
			var nse *script.NoSignError
			if !errors.As(err, &nse) || !errors.Is(err, script.ErrNoSign) {
				t.Fatalf("Render(%q) error = %v, want *NoSignError", tt.in, err)
			}
			if nse.Grapheme != tt.grapheme || nse.Offset != tt.offset || nse.Word != tt.word {
				t.Errorf("got grapheme %q offset %d word %q; want %q %d %q",
					nse.Grapheme, nse.Offset, nse.Word, tt.grapheme, tt.offset, tt.word)
			}
		})
	}

	var bad *script.InvalidUTF8Error
	if _, err := r.Render("ꜥn\xff"); !errors.As(err, &bad) {
		t.Errorf("invalid UTF-8: error = %v, want *InvalidUTF8Error", err)
	}
}

func TestRenderLexicon(t *testing.T) {
	lx := testLexicon(t)
	r := egyptian(t, lx)
	if !r.HasLexicon() {
		t.Fatal("HasLexicon = false")
	}
	got, err := r.Render("ꜥnḫ ḥtp qd")
	if err != nil {
		t.Fatal(err)
	}
	if got.Hieroglyphs != "𓀀 𓎛𓏏𓊪 𓀁" {
		t.Errorf("hieroglyphs = %q", got.Hieroglyphs)
	}
	want := []script.Method{script.MethodLexicon, script.MethodPhonetic, script.MethodLexicon}
	for i, w := range got.Words {
		if w.Method != want[i] {
			t.Errorf("word %d (%q): method %s, want %s", i, w.Translit, w.Method, want[i])
		}
	}
	if a := got.Words[0].Attestation; a == nil || a.Count != 3 || a.Total != 4 || a.Variants != 2 {
		t.Errorf("attestation = %+v", a)
	}
	// "qd" is in the fixture as "ḳd"; aliases apply to lexicon lookups too.
	if got.Words[2].Translit != "qd" {
		t.Errorf("translit = %q", got.Words[2].Translit)
	}
	if len(got.Sources) != 2 || got.Sources[0].ID != "test-lexicon" || got.Sources[1].ID != "egy-unikemet-uniliterals" {
		t.Errorf("sources = %+v, want lexicon then sign list", got.Sources)
	}
	if got.Sources[0].Attribution == "" {
		t.Error("lexicon source lost its attribution")
	}

	// A word in the lexicon renders even if it contains graphemes without a sign.
	if got, err := r.Render("zꜣ"); err != nil || got.Hieroglyphs != "𓀂" {
		t.Errorf("Render(zꜣ) = %v, %v", got, err)
	}
}

func TestLexiconLanguageMismatch(t *testing.T) {
	lx, err := script.ParseLexicon(strings.NewReader(strings.Replace(readFile(t, "testdata/lexicon.tsv"),
		"#@language: egy", "#@language: sux", 1)), "sux.tsv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := script.Egyptian(lx); err == nil {
		t.Error("Egyptian accepted a Sumerian lexicon")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const signHeader = "#@id: t\n#@language: egy\n#@source: s\n#@source_url: u\n#@source_sha256: x\n#@license: MIT\n#@selection: test\n" +
	"value\tcodepoint\tsign\tgardiner\tfunction\tcorpus_count\tcandidates\n"

func TestParseSignListErrors(t *testing.T) {
	tests := []struct {
		name, data, want string
	}{
		{"wrong columns", "#@id: t\nvalue\tsign\n", "columns are"},
		{"missing header", "#@id: t\n", "missing column header"},
		{"missing metadata", "value\tcodepoint\tsign\tgardiner\tfunction\tcorpus_count\tcandidates\n", `missing metadata "#@language`},
		{"empty", signHeader, "no signs"},
		{"field count", signHeader + "n\tU+13216\t𓈖\n", "has 3 fields"},
		{"codepoint mismatch", signHeader + "n\tU+13217\t𓈖\tN35\tPhonemogram\t1\t1\n", "matching codepoint"},
		{"bad count", signHeader + "n\tU+13216\t𓈖\tN35\tPhonemogram\tx\t1\n", "corpus_count"},
		{"duplicate", signHeader + "n\tU+13216\t𓈖\tN35\tP\t1\t1\nn\tU+13216\t𓈖\tN35\tP\t1\t1\n", "duplicate value"},
		{"space in value", signHeader + "n n\tU+13216\t𓈖\tN35\tP\t1\t1\n", "no spaces"},
		{"not NFC", signHeader + "ḫ\tU+13216\t𓈖\tN35\tP\t1\t1\n", "NFC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := script.ParseSignList(strings.NewReader(tt.data), "test.tsv")
			var fe *script.FormatError
			if !errors.As(err, &fe) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want *FormatError containing %q", err, tt.want)
			}
		})
	}
}

func TestParseLexiconErrors(t *testing.T) {
	header := "#@id: t\n#@language: egy\n#@source: s\n#@source_url: u\n#@source_sha256: x\n#@license: CC-BY-SA-4.0\n#@attribution: a\n" +
		"form\thieroglyphs\tcount\ttotal\tvariants\n"
	tests := []struct {
		name, data, want string
	}{
		{"missing attribution", strings.Replace(header, "#@attribution: a\n", "", 1), `"#@attribution`},
		{"zero count", header + "ꜥnḫ\t𓋹\t0\t1\t1\n", "positive integer"},
		{"count exceeds total", header + "ꜥnḫ\t𓋹\t2\t1\t1\n", "must not exceed total"},
		{"duplicate", header + "ꜥnḫ\t𓋹\t1\t1\t1\nꜥnḫ\t𓋹\t1\t1\t1\n", "duplicate form"},
		{"space in spelling", header + "ꜥnḫ\t𓋹 𓋹\t1\t1\t1\n", "no spaces"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := script.ParseLexicon(strings.NewReader(tt.data), "test.tsv")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestParseConventionsErrors(t *testing.T) {
	tests := []struct {
		name, data, want string
	}{
		{"empty", "", "file is empty"},
		{"unknown key", "language: egy\nfoo: 1\n", "field foo not found"},
		{"missing language", "separators: [{match: '.', note: n}]\n", "language: required"},
		{"missing note", "language: egy\naliases: [{match: q, canonical: ḳ}]\n", "note: required"},
		{"same alias", "language: egy\naliases: [{match: q, canonical: q, note: n}]\n", "are the same"},
		{"duplicate", "language: egy\nseparators: [{match: '.', note: n}]\naliases: [{match: '.', canonical: x, note: n}]\n", "already defined"},
		{"whitespace", "language: egy\nseparators: [{match: ' ', note: n}]\n", "whitespace"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := script.ParseConventions([]byte(tt.data), "test.yaml")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestWriteRoundTrip(t *testing.T) {
	meta := map[string]string{
		"id": "rt", "language": "egy", "source": "s", "source_url": "u", "source_sha256": "x",
		"license": "MIT", "selection": "test", "attribution": "a",
	}
	rows := []script.SignRow{{
		Sign:     script.Sign{Value: "n", Sign: "𓈖", Codepoint: "U+13216", Gardiner: "N35"},
		Function: "Phonemogram", CorpusCount: 7, Candidates: 2,
	}}
	var buf bytes.Buffer
	if err := script.WriteSignList(&buf, meta, []string{"comment", "", "more"}, rows); err != nil {
		t.Fatal(err)
	}
	sl, err := script.ParseSignList(&buf, "rt.tsv")
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := sl.Lookup("n"); !ok || s != rows[0].Sign || sl.Source.Attribution != "a" {
		t.Errorf("round trip: %+v %v %+v", s, ok, sl.Source)
	}

	entries := []script.LexiconEntry{{Form: "ꜥnḫ", Spelling: script.Spelling{Hieroglyphs: "𓀀", Count: 1, Total: 2, Variants: 2}}}
	buf.Reset()
	if err := script.WriteLexicon(&buf, meta, nil, entries); err != nil {
		t.Fatal(err)
	}
	lx, err := script.ParseLexicon(&buf, "rt.tsv")
	if err != nil {
		t.Fatal(err)
	}
	if sp, ok := lx.Lookup("ꜥnḫ"); !ok || sp != entries[0].Spelling {
		t.Errorf("round trip: %+v %v", sp, ok)
	}

	if err := script.WriteLexicon(&buf, map[string]string{"id": "x"}, nil, nil); err == nil {
		t.Error("WriteLexicon accepted missing metadata")
	}
	bad := []script.LexiconEntry{{Form: "a\tb", Spelling: script.Spelling{Hieroglyphs: "𓀀", Count: 1, Total: 1, Variants: 1}}}
	if err := script.WriteLexicon(&buf, meta, nil, bad); err == nil {
		t.Error("WriteLexicon accepted a tab inside a field")
	}
}

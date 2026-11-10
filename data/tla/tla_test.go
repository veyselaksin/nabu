package tla

import (
	"os"
	"strings"
	"testing"

	"github.com/veyselaksin/nabu/script"
)

// The fixture is synthetic: arbitrary signs, not TLA data.
func load(t *testing.T) []Sentence {
	t.Helper()
	f, err := os.Open("../testdata/tla-fixture.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := Read(f)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRead(t *testing.T) {
	if got := load(t); len(got) != 5 {
		t.Fatalf("read %d sentences, want 5 (blank lines skipped)", len(got))
	}
	if _, err := Read(strings.NewReader("{\"hieroglyphs\": 1}\n")); err == nil {
		t.Error("Read accepted a non-string field")
	}
}

func TestLexicon(t *testing.T) {
	entries, st := Lexicon(load(t))
	got := map[string]script.Spelling{}
	for _, e := range entries {
		got[e.Form] = e.Spelling
	}
	want := map[string]script.Spelling{
		// 𓀀 twice, 𓀄 once.
		"ꜥnḫ": {Hieroglyphs: "𓀀", Count: 2, Total: 3, Variants: 2},
		// Brackets removed, so "ḥtp(.w)" and "ḥtp.w" are one form; the tie
		// goes to the lexicographically smaller spelling.
		"ḥtp.w": {Hieroglyphs: "𓀁𓀂", Count: 1, Total: 2, Variants: 2},
	}
	if len(got) != len(want) {
		t.Errorf("forms = %v", got)
	}
	for f, w := range want {
		if got[f] != w {
			t.Errorf("%s = %+v, want %+v", f, got[f], w)
		}
	}
	wantStats := Stats{Sentences: 5, MisalignedSentence: 1, Elements: 7, NonUnicodeSpelling: 1, EmptyForm: 1, Forms: 2}
	if st != wantStats {
		t.Errorf("stats = %+v, want %+v", st, wantStats)
	}
}

func TestIsHieroglyphText(t *testing.T) {
	for s, want := range map[string]bool{
		"𓋹": true, "𓆑︂": true, "𓐰": true, "": false, "<g>F51D</g>": false, "𓋹a": false, "𓋹 𓋹": false,
	} {
		if got := IsHieroglyphText(s); got != want {
			t.Errorf("IsHieroglyphText(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestNormalizeForm(t *testing.T) {
	if got := NormalizeForm("ḥtp(.w)"); got != "ḥtp.w" {
		t.Errorf("got %q", got)
	}
	if got := NormalizeForm("ḫtp"); got != "ḫtp" {
		t.Errorf("got %q, want NFC", got)
	}
}

func TestSignCounts(t *testing.T) {
	c := SignCounts(load(t))
	if c['𓀀'] != 3 || c['𓀅'] != 1 {
		t.Errorf("counts = %v", c)
	}
	for r := range c {
		if r < 0x13000 || r > 0x1342F {
			t.Errorf("counted non-sign U+%04X (placeholders and selectors must be ignored)", r)
		}
	}
}

package unikemet

import (
	"os"
	"strings"
	"testing"
)

func load(t *testing.T) map[rune]*Record {
	t.Helper()
	f, err := os.Open("../testdata/Unikemet-fixture.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	recs, version, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if version != "99.0.0" {
		t.Errorf("version = %q", version)
	}
	return recs
}

func TestParse(t *testing.T) {
	recs := load(t)
	if r := recs[0x1309D]; r.Func != "Phonemogram" || r.Core != "N" || r.JSesh != "D36" {
		t.Errorf("U+1309D = %+v (Func must be trimmed, Core must default to N)", r)
	}
	if r := recs[0x13003]; r.FVal != "i̯" {
		t.Errorf("FVal = %q", r.FVal)
	}
	for _, bad := range []string{"U+13000\tkEH_Core\n", "13000\tkEH_Core\tC\n", "U+ZZ\tkEH_Core\tC\n", "# only comments\n"} {
		if _, _, err := Parse(strings.NewReader(bad)); err == nil {
			t.Errorf("Parse(%q) succeeded", bad)
		}
	}
}

func TestIsUniliteral(t *testing.T) {
	recs := load(t)
	want := map[rune]bool{0x13216: true, 0x132F9: false, 0x1309D: true, 0x13002: false, 0x13003: true, 0x13001: true}
	for r, w := range want {
		if got := IsUniliteral(recs[r]); got != w {
			t.Errorf("IsUniliteral(U+%04X) = %v, want %v", r, got, w)
		}
	}
}

func TestUniliterals(t *testing.T) {
	recs := load(t)
	counts := map[rune]int{0x13216: 5, 0x132D4: 9, 0x1309D: 3, 0x133BA: 3, 0x13003: 1}
	rows, skipped := Uniliterals(recs, counts)
	got := map[string]string{}
	for _, r := range rows {
		got[r.Value] = r.Codepoint
		if r.Value == "n" && (r.CorpusCount != 9 || r.Candidates != 2 || r.Gardiner != "S3") {
			t.Errorf("row n = %+v", r)
		}
	}
	// n: the more frequent sign wins. ꜥ: a tie goes to the core sign.
	want := map[string]string{"n": "U+132D4", "ꜥ": "U+133BA", "i̯": "U+13003"}
	for v, cp := range want {
		if got[v] != cp {
			t.Errorf("value %q → %s, want %s", v, got[v], cp)
		}
	}
	if len(rows) != len(want) {
		t.Errorf("rows = %+v", rows)
	}
	// x has no candidate in the corpus and must be skipped, not guessed.
	if len(skipped) != 1 || skipped[0].Value != "x" {
		t.Errorf("skipped = %+v", skipped)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].Value >= rows[i].Value {
			t.Error("rows are not sorted by value")
		}
	}
}

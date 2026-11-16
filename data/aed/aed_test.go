package aed

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRead(t *testing.T) {
	f, err := os.Open("testdata/dictionary_entries.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := Read(f)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{ID: "10050", Form: "=f", German: "[Suffix Pron. sg.3.m.]", English: "he; him; his (suffix pron., 3rd. per. masc. sing.)",
			POS: "pronoun: personal_pronoun", Bibliography: "Wb 1, 572.1; EAG § 159"},
		{ID: "851172", Form: "=fj", German: "[Suffix Pron. sg.3.m. (Verbaladj.)]", POS: "pronoun: personal_pronoun"},
		{ID: "100", Form: "Ꜣbḏ", German: "Der Fischer", English: "Fisher", POS: "entity_name: gods_name", Bibliography: "Meeks, AL 78.0031"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Read =\n%+v\nwant\n%+v", got, want)
	}
	if got[1].Lemma() != "=fꞽ" || got[2].Lemma() != "ꜣbḏ" {
		t.Errorf("Lemma: %q, %q", got[1].Lemma(), got[2].Lemma())
	}
	if _, err := Read(strings.NewReader("<html></html>")); err == nil {
		t.Error("empty page: want error")
	}
}

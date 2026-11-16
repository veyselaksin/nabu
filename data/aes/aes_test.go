package aes

import (
	"reflect"
	"testing"
)

func TestConvertForm(t *testing.T) {
	// Pairs observed between AES and the TLA v18/v19 datasets.
	for in, want := range map[string]string{
		"jwꜣ":                  "ꞽwꜣ",
		"Jnp,w":                "ꞽnp.w",
		"(j)r(,j-j)ḫ(,t)-nswt": "(ꞽ)r(.ꞽ-ꞽ)ḫ(.t)-nswt",
		"nṯr.pl":               "nṯr.PL",
		"ẖr.du":                "ẖr.DU",
		"tp.j-ḏw≡f":            "tp.ꞽ-ḏw=f",
		"=⸢f⸣":                 "=f",
		"〈〈ḥtp-ḏi̯〉〉":          "ḥtp-ḏi̯",
		"[ḏ(d)-mdw]":           "ḏ(d)-mdw",
		"Nfr-kꜣ-Rꜥw":           "nfr-kꜣ-rꜥw",
		"⸗k":                   "=k",
		"plš":                  "plš", // ".pl" only as a marker
	} {
		if got := ConvertForm(in); got != want {
			t.Errorf("ConvertForm(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadDir(t *testing.T) {
	s, err := ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 2 || s[0].ID != "S1" || s[1].ID != "S2" {
		t.Fatalf("sentences = %+v", s)
	}
	if got := s[0].Transliteration(); got != "ḥtp-ḏi̯ nswt" {
		t.Errorf("Transliteration = %q", got)
	}
	if got := s[0].Tokens[0].Hieroglyphs(); got != "𓏙𓊵" {
		t.Errorf("Hieroglyphs = %q", got)
	}
	if got := s[0].LemmaIDs(); !reflect.DeepEqual(got, []string{"400156", ""}) {
		t.Errorf("LemmaIDs = %q", got)
	}
	if Stage(s[0].Date) != "earlier" || Stage(s[1].Date) != "" {
		t.Errorf("Stage: %q→%q, %q→%q", s[0].Date, Stage(s[0].Date), s[1].Date, Stage(s[1].Date))
	}
	for form, want := range map[string]bool{"nfr": true, "ꞽ_": false, "...": false, "⸮zšp?": false, "r…": false} {
		if got := (Sentence{Tokens: []Token{{Form: "ḏd"}, {Form: form}}}).Intact(); got != want {
			t.Errorf("Intact with %q = %v, want %v", form, got, want)
		}
	}
	if (Sentence{}).Intact() {
		t.Error("empty sentence is not intact")
	}
	if _, err := ReadDir(t.TempDir()); err == nil {
		t.Error("ReadDir of an empty dir: want error")
	}
}

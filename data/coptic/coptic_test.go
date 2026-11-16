package coptic

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRead(t *testing.T) {
	f, err := os.Open("testdata/lexicon.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := Read(f)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{ID: "C1", Lemma: "ⲁ", Dialects: []string{"S"}, POS: "Zahlzeichen",
			Glosses: map[string][]string{"de": {"Eins"}, "en": {"one"}, "fr": {"un"}}},
		{ID: "C9", Lemma: "ⲁⲗⲟⲩ", Dialects: []string{"S", "B"}, POS: "Subst. m./f.",
			Glosses: map[string][]string{"en": {"child, boy, girl", "pupil"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Read =\n%+v\nwant\n%+v", got, want)
	}
	if _, err := Read(strings.NewReader("<TEI/>")); err == nil {
		t.Error("no entries: want error")
	}
}

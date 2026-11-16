package wiktionary

import (
	"reflect"
	"strings"
	"testing"
)

const fixture = `{"word":"ꜥnḫ","pos":"verb","senses":[{"glosses":["to live"]},{"glosses":["heading","to be alive"]}],"sounds":[{"note":"reconstructed","ipa":"/ˈʕaːnax/"},{"note":"Middle Egyptian, c. 1700 BCE","ipa":"/ˈʕaːnax/"},{"note":"modern Egyptological","ipa":"/ɑːnx/"},{"note":"modern Egyptological","ipa":"/ɑːnx/"}]}
{"word":"anx","pos":"romanization","senses":[{"glosses":["romanization of ꜥnḫ"]}]}
{"word":"ꜥnḫw","pos":"noun","senses":[{"glosses":["plural of ꜥnḫ"],"form_of":[{"word":"ꜥnḫ"}]}]}

{"word":"nswt","pos":"noun","senses":[{"glosses":["king"]},{"glosses":["alt"],"alt_of":[{"word":"nsw"}]}]}
`

func TestRead(t *testing.T) {
	got, err := Read(strings.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Word: "ꜥnḫ", POS: "verb", Glosses: []string{"to live", "to be alive"}, Pronunciations: []Pronunciation{
			{Label: "Middle Egyptian, c. 1700 BCE", IPA: "/ˈʕaːnax/"}, {Label: "modern Egyptological", IPA: "/ɑːnx/"}}},
		{Word: "nswt", POS: "noun", Glosses: []string{"king"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Read =\n%+v\nwant\n%+v", got, want)
	}
	if _, err := Read(strings.NewReader("{not json")); err == nil {
		t.Error("want an error for bad JSON")
	}
}

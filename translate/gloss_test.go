package translate

import (
	"reflect"
	"testing"
)

func TestGlossPhrases(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"to live; to be alive", []string{"live", "alive"}},
		{"king; king of Upper Egypt", []string{"king"}},
		{"bread (gen.)", []string{"bread"}},
		{"[Suffix Pron. sg.1.c.]", nil},
		{"der König, Herrscher", []string{"könig", "herrscher"}},
		{"house (gen.); temple; tomb; container", []string{"house", "temple", "tomb", "container"}},
		{"a very long gloss of many words", nil},
		{"Ein Opfer, das (König/Gott NN) gibt", []string{"opfer"}},
	} {
		if got := glossPhrases(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("glossPhrases(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGlossLookup(t *testing.T) {
	docs := []Doc{
		{ID: "aed:1", Kind: KindLexicon, Source: "aed", Translit: "ꜥnḫ", English: "to live; to be alive", German: "leben"},
		{ID: "aed:2", Kind: KindLexicon, Source: "aed", Translit: "ḥqꜣ-ꜥnḫ.w", English: "ruler of the living"},
		{ID: "aed:3", Kind: KindLexicon, Source: "aed", Translit: "nswt", English: "king; king of Upper Egypt", German: "König"},
		{ID: "aed:4", Kind: KindLexicon, Source: "aed", Translit: "ꞽty", English: "sovereign; king"},
		{ID: "aed:5", Kind: KindLexicon, Source: "aed", Translit: "ꜥꜣ-pḥ.tꞽ", English: "great of strength; king"},
		{ID: "aed:6", Kind: KindLexicon, Source: "aed", Translit: "mri̯", English: "to love; to wish"},
		{ID: "c:1", Kind: KindLexicon, Source: "coptic-dict", Stage: "coptic", Translit: "ⲣⲣⲟ", English: "king"},
		{ID: "s1", Kind: KindSentence, Translit: "ꜥnḫ nswt", German: "König"},
	}
	freq := map[string]int{"aed:1": 220, "aed:3": 276, "aed:4": 30, "aed:6": 131}
	g := NewGlossIndex(docs, func(id string) int { return freq[id] })

	ids := func(gs []Gloss) []string {
		var out []string
		for _, x := range gs {
			out = append(out, x.ID+"/"+x.Term)
		}
		return out
	}
	// The plain lemma is found, not the compound; of the entries glossed
	// "king", the one where it is the first gloss comes first, and the one
	// that never occurs in the corpus is left out. Coptic is not indexed.
	got := g.Lookup("May the kings live", nil, 2, 10, nil)
	if want := []string{"aed:3/king", "aed:4/king", "aed:1/live"}; !reflect.DeepEqual(ids(got), want) {
		t.Errorf("Lookup = %v, want %v", ids(got), want)
	}
	if got[0].Match != MatchExact || got[0].Count != 276 {
		t.Errorf("first = %+v", got[0])
	}
	// Inflected English, German keywords, exclusion and the cap.
	if got := ids(g.Lookup("she loved him", []string{"König"}, 1, 10, nil)); !reflect.DeepEqual(got, []string{"aed:6/love", "aed:3/könig"}) {
		t.Errorf("Lookup = %v", got)
	}
	if got := ids(g.Lookup("king", nil, 2, 10, func(d *Doc) bool { return d.ID == "aed:3" })); !reflect.DeepEqual(got, []string{"aed:4/king"}) {
		t.Errorf("excluded Lookup = %v", got)
	}
	if got := g.Lookup("king live love", nil, 2, 2, nil); len(got) != 2 {
		t.Errorf("capped Lookup = %v", ids(got))
	}
	// An entry that never occurs is still better than nothing.
	if got := ids(NewGlossIndex(docs, nil).Lookup("strength", nil, 2, 10, nil)); len(got) != 0 {
		t.Errorf("multi-word gloss matched a single word: %v", got)
	}
	if got := ids(NewGlossIndex(docs[4:5], nil).Lookup("the king", nil, 2, 10, nil)); !reflect.DeepEqual(got, []string{"aed:5/king"}) {
		t.Errorf("zero-frequency Lookup = %v", got)
	}
	if !g.Means("aed:1", "lives") || !g.Means("aed:3", "könig") || g.Means("aed:2", "live") || g.Means("x", "live") {
		t.Error("Means is wrong")
	}
	if got := ContentWords("May the king live, the King"); !reflect.DeepEqual(got, []string{"king", "live"}) {
		t.Errorf("ContentWords = %q", got)
	}
	var none *GlossIndex
	if none.Lookup("king", nil, 2, 10, nil) != nil {
		t.Error("nil GlossIndex returned entries")
	}
}

package corpus

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuild(t *testing.T) {
	c, err := Build("testdata/data")
	if err != nil {
		t.Fatal(err)
	}
	want := []Sentence{
		{ID: "tla:1", Source: "tla", TextID: "TXT1", Stage: Earlier, Date: "-2000..-1900", Translit: "ꜥnḫ =f",
			Hieroglyphs: "𓋹 𓆑", Lemmas: []string{"38530", "10050"}, German: "Er lebt.", Group: "x:TXT1"},
		{ID: "tla-late:1", Source: "tla-late", Stage: Late, Date: "-1200", Translit: "ꞽw =f",
			Lemmas: []string{"851513", "10050"}, German: "Er ist.", Group: "t:ꞽw =f"},
		{ID: "tla-demotic:1", Source: "tla-demotic", Stage: Demotic, Date: "201..250", Translit: "mtw =w",
			Lemmas: []string{"d2779", "d1172"}, German: "und sie", Group: "t:mtw =w"},
		{ID: "aes:A2", Source: "aes", TextID: "TXT1", Date: "NK", Translit: "nswt ꞽnp.w", Hieroglyphs: "𓇓 𓃢",
			Lemmas: []string{"88040", "33920"}, German: "Der König Jnp,w.", Group: "x:TXT1"},
	}
	if !reflect.DeepEqual(c.Sentences, want) {
		t.Fatalf("Sentences =\n%+v\nwant\n%+v", c.Sentences, want)
	}
	m := c.Manifest
	if m.Duplicates["tla"] != 1 || m.Duplicates["aes"] != 1 || m.Skipped["tla"] != 1 || m.Skipped["aes"] != 1 {
		t.Errorf("duplicates %v, skipped %v", m.Duplicates, m.Skipped)
	}
	if m.Stages["earlier"] != 1 || m.Stages["unknown"] != 1 || len(m.Sources) != 8 || m.Sources[0].License != "CC-BY-SA-4.0" {
		t.Errorf("manifest = %+v", m)
	}
	if len(c.Lexicon) != 9 || !reflect.DeepEqual(c.Lexicon[0], LexEntry{ID: "aed:10050", Source: "aed", Lemma: "=f",
		German: "[Suffix Pron. sg.3.m.]", English: "he; him; his (suffix pron., 3rd. per. masc. sing.)", POS: "pronoun: personal_pronoun"}) {
		t.Errorf("Lexicon = %+v", c.Lexicon)
	}
	give := LexEntry{ID: "aed:851711", Source: "aed", Lemma: "rḏi̯", German: "geben; veranlassen", English: "to give; to cause",
		POS: "verb: verb_irr", EnglishSource: "wiktionary-egy",
		Pronunciations: []Pronunciation{
			{Label: "Middle Egyptian, c. 1700 BCE", IPA: "/(ɾV)ˈtʼiːjiʔ/", Source: "wiktionary-egy"},
			{Label: "modern Egyptological", IPA: "/rɛd͡ʒi/", Source: "wiktionary-egy"}},
		Coptic: []CopticForm{{ID: "coptic-dict:C9", Lemma: "ⲁⲗⲟⲩ", Dialects: []string{"S", "B"}, Source: "oraec-coptic-etymologies"}}}
	if !reflect.DeepEqual(c.Lexicon[3], give) {
		t.Errorf("joined entry = %+v", c.Lexicon[3])
	}
	// Not joined: the root entry, two homonyms, a clitic that already has English.
	for _, i := range []int{4, 5, 6} {
		if e := c.Lexicon[i]; e.English != "" || e.Pronunciations != nil {
			t.Errorf("entry %s must not be joined: %+v", e.ID, e)
		}
	}
	if c.Lexicon[0].Coptic != nil || m.Skipped["wiktionary-egy:homonyms"] != 1 || m.Skipped["wiktionary-egy:no AED entry"] != 2 ||
		m.Skipped["oraec-coptic-etymologies:unknown lemma ID"] != 1 || m.Skipped["oraec-coptic-etymologies:no Egyptian lemma"] != 1 {
		t.Errorf("skipped = %v", m.Skipped)
	}
	if st := m.LexiconStats["aed"]; st != (LexStats{Entries: 7, WithEnglish: 3, EnglishBorrowed: 1, WithGerman: 7, WithPronunciation: 1, WithCoptic: 1}) {
		t.Errorf("lexicon stats = %+v", st)
	}
	if e := c.Lexicon[7]; e.Source != "coptic-dict" || e.Stage != Coptic || e.English != "one" || e.French != "un" {
		t.Errorf("coptic entry = %+v", e)
	}

	dir := t.TempDir()
	if err := c.Write(dir); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "sentences.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	back, err := ReadSentences(f)
	if err != nil || !reflect.DeepEqual(back, c.Sentences) {
		t.Errorf("round trip: %v\n%+v", err, back)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Error(err)
	}
}

func TestBuildMissingSource(t *testing.T) {
	if _, err := Build(t.TempDir()); err == nil {
		t.Error("want an error for missing sources")
	}
}

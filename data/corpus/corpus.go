// Package corpus merges Nabu's Egyptian sources into one sentence corpus and
// one lexicon, in the transliteration conventions of the TLA v18 datasets,
// with every record naming its source. The result is the input for retrieval
// (RAG) and fine-tuning; it is derived from share-alike data and stays local.
package corpus

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/veyselaksin/nabu/data/aed"
	"github.com/veyselaksin/nabu/data/aes"
	"github.com/veyselaksin/nabu/data/coptic"
	"github.com/veyselaksin/nabu/data/oraec"
	"github.com/veyselaksin/nabu/data/source"
	"github.com/veyselaksin/nabu/data/tla"
	"github.com/veyselaksin/nabu/data/wiktionary"
)

// Language stages.
const (
	Earlier = "earlier" // Old and Middle Egyptian
	Late    = "late"
	Demotic = "demotic"
	Coptic  = "coptic"
)

// Sentence is one corpus sentence.
type Sentence struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	// TextID identifies the text the sentence belongs to (AES/AED-TEI IDs;
	// TLA sentences get one when AES has the same sentence).
	TextID string `json:"text_id,omitempty"`
	// Stage is the language stage, or "" when the source does not settle it.
	Stage string `json:"stage,omitempty"`
	Date  string `json:"date,omitempty"`
	// Translit is the transliteration in TLA v18 conventions.
	Translit    string   `json:"translit"`
	Hieroglyphs string   `json:"hieroglyphs,omitempty"`
	Lemmas      []string `json:"lemmas,omitempty"`
	// German is the editors' translation.
	German string `json:"de"`
	// Group keeps related sentences in the same train/test split: the text
	// ID, else the transliteration (formulae repeat across texts).
	Group string `json:"group"`
}

// LexEntry is one lexicon entry.
type LexEntry struct {
	ID       string   `json:"id"`
	Source   string   `json:"source"`
	Stage    string   `json:"stage,omitempty"`
	Lemma    string   `json:"lemma"`
	German   string   `json:"de,omitempty"`
	English  string   `json:"en,omitempty"`
	French   string   `json:"fr,omitempty"`
	POS      string   `json:"pos,omitempty"`
	Dialects []string `json:"dialects,omitempty"`
	// EnglishSource names the source of English when it is not the entry's
	// own source (an AED entry whose English gloss comes from Wiktionary).
	EnglishSource string `json:"en_source,omitempty"`
	// Pronunciations are labeled IPA transcriptions from other sources.
	Pronunciations []Pronunciation `json:"pronunciations,omitempty"`
	// Coptic lists the Coptic words this Egyptian lemma survives as.
	Coptic []CopticForm `json:"coptic,omitempty"`
}

// Pronunciation is one IPA transcription with the label its source gives it
// ("modern Egyptological", "Middle Egyptian, c. 1700 BCE", …). Readings
// labeled with a stage are reconstructions, not attested sounds.
type Pronunciation struct {
	Label  string `json:"label"`
	IPA    string `json:"ipa"`
	Source string `json:"source"`
}

// CopticForm is a Coptic descendant of an Egyptian lemma: evidence for its
// vowels, since Coptic writes them.
type CopticForm struct {
	ID       string   `json:"id"` // lexicon ID, "coptic-dict:C1494"
	Lemma    string   `json:"lemma"`
	Dialects []string `json:"dialects,omitempty"`
	Source   string   `json:"source"` // who made the link
}

// LexStats summarizes the lexicon entries of one source (a data-quality
// report: what share of them can be looked up in which way).
type LexStats struct {
	Entries           int `json:"entries"`
	WithEnglish       int `json:"with_english"`
	EnglishBorrowed   int `json:"english_from_other_source"`
	WithGerman        int `json:"with_german"`
	WithPronunciation int `json:"with_pronunciation"`
	WithCoptic        int `json:"with_coptic"`
	NoGloss           int `json:"no_gloss"`
}

// Manifest records what a build read and wrote.
type Manifest struct {
	Built      time.Time        `json:"built"`
	Sources    []ManifestSource `json:"sources"`
	Sentences  int              `json:"sentences"`
	Lexicon    int              `json:"lexicon"`
	Duplicates map[string]int   `json:"duplicates_dropped"`
	Skipped    map[string]int   `json:"skipped"`
	Stages     map[string]int   `json:"sentences_by_stage"`
	// LexiconStats is keyed by source.
	LexiconStats map[string]LexStats `json:"lexicon_by_source"`
	Files        map[string]string   `json:"files"`
}

// ManifestSource is one input source with its license and pinned files.
type ManifestSource struct {
	Key         string   `json:"key"`
	License     string   `json:"license"`
	Attribution string   `json:"attribution"`
	Files       []string `json:"files"`
	Records     int      `json:"records"`
}

// Corpus is the merged result.
type Corpus struct {
	Sentences []Sentence
	Lexicon   []LexEntry
	Manifest  Manifest
}

// Build reads every source from dataDir (<dataDir>/<source key>/…).
// Sources are read in priority order: TLA datasets first (the newer
// editions), then AES; an AES sentence with the same lemma IDs and German
// translation as a TLA sentence, or the same transliteration and
// translation, is dropped as a duplicate.
func Build(dataDir string) (*Corpus, error) {
	c := &Corpus{Manifest: Manifest{
		Duplicates: map[string]int{}, Skipped: map[string]int{}, Stages: map[string]int{},
	}}
	seen := map[string]string{} // dedup key → sentence ID
	byLemma := map[string]int{} // lemma key → index in c.Sentences (TLA only)

	add := func(s Sentence, keys ...string) bool {
		for _, k := range keys {
			if k == "" {
				continue
			}
			if _, dup := seen[k]; dup {
				c.Manifest.Duplicates[s.Source]++
				return false
			}
		}
		for _, k := range keys {
			if k != "" {
				seen[k] = s.ID
			}
		}
		c.Sentences = append(c.Sentences, s)
		return true
	}

	for _, t := range []struct{ key, stage string }{
		{"tla", Earlier}, {"tla-late", Late}, {"tla-demotic", Demotic},
	} {
		sents, err := readTLA(filepath.Join(dataDir, t.key, "train.jsonl"))
		if err != nil {
			return nil, err
		}
		n := 0
		for i, s := range sents {
			translit := canonicalTLA(s.Transliteration)
			de := cleanText(s.Translation)
			if translit == "" || de == "" {
				c.Manifest.Skipped[t.key]++
				continue
			}
			lemmas := lemmaIDs(s.Lemmatization)
			hiero := ""
			if tla.IsHieroglyphText(strings.ReplaceAll(s.Hieroglyphs, " ", "")) {
				hiero = s.Hieroglyphs
			}
			rec := Sentence{
				ID: fmt.Sprintf("%s:%d", t.key, i+1), Source: t.key, Stage: t.stage,
				Date: dateRange(s.DateNotBefore, s.DateNotAfter), Translit: translit,
				Hieroglyphs: hiero, Lemmas: lemmas, German: de, Group: "t:" + formKey(translit),
			}
			if add(rec, lemmaKey(lemmas, de), formKey(translit)+"|"+de) {
				n++
				if k := lemmaKey(lemmas, de); k != "" {
					byLemma[k] = len(c.Sentences) - 1
				}
			}
		}
		c.Manifest.Sources = append(c.Manifest.Sources, manifestSource(t.key, n))
	}

	aesSents, err := aes.ReadDir(filepath.Join(dataDir, "aes"))
	if err != nil {
		return nil, err
	}
	n := 0
	for _, s := range aesSents {
		translit := s.Transliteration()
		de := cleanText(s.Translation)
		// Like the TLA premium datasets: intact, translated sentences only.
		if translit == "" || de == "" || !s.Intact() {
			c.Manifest.Skipped["aes"]++
			continue
		}
		lemmas := s.LemmaIDs()
		lk := lemmaKey(lemmas, de)
		// A TLA sentence that AES also has learns its text ID.
		if i, ok := byLemma[lk]; ok && c.Sentences[i].TextID == "" {
			c.Sentences[i].TextID = s.TextID
			c.Sentences[i].Group = "x:" + s.TextID
		}
		rec := Sentence{
			ID: "aes:" + s.ID, Source: "aes", TextID: s.TextID, Stage: aes.Stage(s.Date),
			Date: s.Date, Translit: translit, Hieroglyphs: aesHieroglyphs(s), Lemmas: lemmas,
			German: de, Group: "x:" + s.TextID,
		}
		if add(rec, lk, formKey(translit)+"|"+de) {
			n++
		}
	}
	c.Manifest.Sources = append(c.Manifest.Sources, manifestSource("aes", n))

	if err := c.addAED(filepath.Join(dataDir, "aed", "dictionary_entries.html")); err != nil {
		return nil, err
	}
	if err := c.addCoptic(filepath.Join(dataDir, "coptic-dict", "Comprehensive_Coptic_Lexicon-v1.2-2020.xml")); err != nil {
		return nil, err
	}
	if err := c.addWiktionary(filepath.Join(dataDir, "wiktionary-egy", "kaikki.org-dictionary-Egyptian.jsonl")); err != nil {
		return nil, err
	}
	if err := c.addEtymologies(filepath.Join(dataDir, "oraec-coptic-etymologies",
		"digitizing_coptic_etymologies_coptic_list_entries.csv")); err != nil {
		return nil, err
	}
	c.Manifest.LexiconStats = map[string]LexStats{}
	for _, e := range c.Lexicon {
		st := c.Manifest.LexiconStats[e.Source]
		st.Entries++
		if e.English != "" {
			st.WithEnglish++
		}
		if e.EnglishSource != "" {
			st.EnglishBorrowed++
		}
		if e.German != "" {
			st.WithGerman++
		}
		if e.English == "" && e.German == "" && e.French == "" {
			st.NoGloss++
		}
		if len(e.Pronunciations) > 0 {
			st.WithPronunciation++
		}
		if len(e.Coptic) > 0 {
			st.WithCoptic++
		}
		c.Manifest.LexiconStats[e.Source] = st
	}

	for _, s := range c.Sentences {
		c.Manifest.Stages[stageName(s.Stage)]++
	}
	c.Manifest.Sentences, c.Manifest.Lexicon = len(c.Sentences), len(c.Lexicon)
	return c, nil
}

func (c *Corpus) addAED(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("corpus: %w (run: nabu-data fetch aed)", err)
	}
	defer f.Close()
	entries, err := aed.Read(f)
	if err != nil {
		return err
	}
	for _, e := range entries {
		c.Lexicon = append(c.Lexicon, LexEntry{
			ID: "aed:" + e.ID, Source: "aed", Lemma: e.Lemma(),
			German: cleanText(e.German), English: cleanText(e.English), POS: e.POS,
		})
	}
	c.Manifest.Sources = append(c.Manifest.Sources, manifestSource("aed", len(entries)))
	return nil
}

func (c *Corpus) addCoptic(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("corpus: %w (run: nabu-data fetch coptic-dict)", err)
	}
	defer f.Close()
	entries, err := coptic.Read(f)
	if err != nil {
		return err
	}
	for _, e := range entries {
		c.Lexicon = append(c.Lexicon, LexEntry{
			ID: "coptic-dict:" + e.ID, Source: "coptic-dict", Stage: Coptic, Lemma: e.Lemma,
			German: strings.Join(e.Glosses["de"], "; "), English: strings.Join(e.Glosses["en"], "; "),
			French: strings.Join(e.Glosses["fr"], "; "), POS: e.POS, Dialects: e.Dialects,
		})
	}
	c.Manifest.Sources = append(c.Manifest.Sources, manifestSource("coptic-dict", len(entries)))
	return nil
}

// wiktPOS maps Wiktionary parts of speech to the AED's word classes (the
// part of an AED part of speech before the colon).
var wiktPOS = map[string]string{
	"noun": "substantive", "verb": "verb", "name": "entity_name", "adj": "adjective", "prep": "preposition",
	"adv": "adverb", "pron": "pronoun", "particle": "particle", "num": "numeral", "intj": "interjection",
}

// MatchKey reduces a transliterated lemma to a form in which the TLA/AED
// and Wiktionary conventions agree: yod as j, weak consonants spelled out,
// no separators. It loses distinctions, so a match on it is only a
// candidate.
func MatchKey(lemma string) string {
	s := norm.NFC.String(strings.ToLower(lemma))
	return strings.NewReplacer("i̯", "j", "ꞽ", "j", "ı͗", "j", "u̯", "w", ".", "", "-", "", "ʾ", "", "=", "", " ", "").Replace(s)
}

// maxBorrowedSenses caps how many Wiktionary senses become an entry's
// English gloss, so that one entry does not crowd a prompt.
const maxBorrowedSenses = 5

// isAffix reports whether a lemma is a clitic or an ending ("=f", ".kw"):
// MatchKey drops the mark that tells these apart, so they are never joined.
func isAffix(lemma string) bool {
	return strings.HasPrefix(lemma, "=") || strings.HasPrefix(lemma, ".") || strings.HasPrefix(lemma, "-")
}

// addWiktionary joins Wiktionary's Egyptian entries to AED entries and
// copies over what the AED lacks: an English gloss where it has none, and
// the labeled pronunciations. Egyptian is full of homonyms and the two
// sources number them differently, so an entry is joined only when it is
// the only AED word and the only Wiktionary word with that spelling and
// word class. AED "root" entries are not words and are left alone.
func (c *Corpus) addWiktionary(path string) error {
	const key = "wiktionary-egy"
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("corpus: %w (run: nabu-data fetch %s)", err, key)
	}
	defer f.Close()
	entries, err := wiktionary.Read(f)
	if err != nil {
		return err
	}
	type class struct{ key, pos string }
	aedBy := map[class][]int{}
	for i, e := range c.Lexicon {
		pos, _, _ := strings.Cut(e.POS, ":")
		if e.Source != "aed" || pos == "root" || pos == "" || isAffix(e.Lemma) {
			continue
		}
		k := class{MatchKey(e.Lemma), pos}
		aedBy[k] = append(aedBy[k], i)
	}
	wikBy := map[class][]int{}
	for i, e := range entries {
		if pos := wiktPOS[e.POS]; pos != "" && !isAffix(e.Word) {
			k := class{MatchKey(e.Word), pos}
			wikBy[k] = append(wikBy[k], i)
		}
	}
	joined := 0
	for k, ws := range wikBy {
		as := aedBy[k]
		if len(as) == 0 {
			c.Manifest.Skipped[key+":no AED entry"] += len(ws)
			continue
		}
		if len(as) != 1 || len(ws) != 1 {
			c.Manifest.Skipped[key+":homonyms"] += len(ws)
			continue
		}
		e, w := &c.Lexicon[as[0]], entries[ws[0]]
		joined++
		if e.English == "" {
			// Wiktionary lists every sense; the first few are the common ones.
			e.English, e.EnglishSource = cleanText(strings.Join(w.Glosses[:min(len(w.Glosses), maxBorrowedSenses)], "; ")), key
		}
		for _, p := range w.Pronunciations {
			e.Pronunciations = append(e.Pronunciations, Pronunciation{Label: p.Label, IPA: p.IPA, Source: key})
		}
	}
	c.Manifest.Skipped[key+":not a joinable word class"] = len(entries) - func() (n int) {
		for _, ws := range wikBy {
			n += len(ws)
		}
		return n
	}()
	c.Manifest.Sources = append(c.Manifest.Sources, manifestSource(key, joined))
	return nil
}

// addEtymologies records, on each Egyptian lemma, the Coptic words that
// ORAEC's etymology list derives from it.
func (c *Corpus) addEtymologies(path string) error {
	const key = "oraec-coptic-etymologies"
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("corpus: %w (run: nabu-data fetch %s)", err, key)
	}
	defer f.Close()
	links, err := oraec.ReadEtymologies(f)
	if err != nil {
		return err
	}
	byID := make(map[string]int, len(c.Lexicon))
	for i, e := range c.Lexicon {
		byID[e.ID] = i
	}
	n := 0
	for _, l := range links {
		if l.Egyptian == "" {
			c.Manifest.Skipped[key+":no Egyptian lemma"]++
			continue
		}
		ei, ok1 := byID["aed:"+l.Egyptian]
		ci, ok2 := byID["coptic-dict:"+l.Coptic]
		if !ok1 || !ok2 {
			c.Manifest.Skipped[key+":unknown lemma ID"]++
			continue
		}
		cop := c.Lexicon[ci]
		c.Lexicon[ei].Coptic = append(c.Lexicon[ei].Coptic,
			CopticForm{ID: cop.ID, Lemma: cop.Lemma, Dialects: cop.Dialects, Source: key})
		n++
	}
	c.Manifest.Sources = append(c.Manifest.Sources, manifestSource(key, n))
	return nil
}

// Write stores sentences.jsonl, lexicon.jsonl and manifest.json in dir.
func (c *Corpus) Write(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	c.Manifest.Built = time.Now().UTC().Truncate(time.Second)
	c.Manifest.Files = map[string]string{"sentences": "sentences.jsonl", "lexicon": "lexicon.jsonl"}
	if err := writeJSONL(filepath.Join(dir, "sentences.jsonl"), c.Sentences); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(dir, "lexicon.jsonl"), c.Lexicon); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.Manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(data, '\n'), 0o644)
}

func writeJSONL[T any](path string, records []T) (err error) {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(tmp, path)
		} else {
			os.Remove(tmp)
		}
	}()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

func readTLA(path string) ([]tla.Sentence, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("corpus: %w (run: nabu-data fetch %s)", err, filepath.Base(filepath.Dir(path)))
	}
	defer f.Close()
	return tla.Read(f)
}

// ReadSentences reads a sentences.jsonl file written by Write.
func ReadSentences(r io.Reader) ([]Sentence, error) { return readJSONL[Sentence](r) }

// ReadLexicon reads a lexicon.jsonl file written by Write.
func ReadLexicon(r io.Reader) ([]LexEntry, error) { return readJSONL[LexEntry](r) }

func readJSONL[T any](r io.Reader) ([]T, error) {
	dec := json.NewDecoder(r)
	var out []T
	for {
		var v T
		if err := dec.Decode(&v); err == io.EOF {
			return out, nil
		} else if err != nil {
			return nil, fmt.Errorf("corpus: record %d: %w", len(out)+1, err)
		}
		out = append(out, v)
	}
}

// canonicalTLA writes the Late Egyptian suffix-pronoun mark "⸗" as "=", as
// the Earlier Egyptian and Demotic datasets do, so that the corpus uses one
// convention.
func canonicalTLA(s string) string {
	return strings.Join(strings.Fields(norm.NFC.String(strings.ReplaceAll(s, "⸗", "="))), " ")
}

func cleanText(s string) string { return strings.Join(strings.Fields(norm.NFC.String(s)), " ") }

// formKey is a transliteration with the editors' round brackets removed.
func formKey(translit string) string {
	words := strings.Fields(translit)
	for i, w := range words {
		words[i] = tla.NormalizeForm(w)
	}
	return strings.Join(words, " ")
}

func lemmaIDs(lemmatization string) []string {
	var out []string
	for _, f := range strings.Fields(lemmatization) {
		id, _, _ := strings.Cut(f, "|")
		out = append(out, id)
	}
	return out
}

// lemmaKey identifies a sentence by its lemma IDs and translation; it is ""
// when no token is lemmatized.
func lemmaKey(lemmas []string, de string) string {
	if strings.Join(lemmas, "") == "" {
		return ""
	}
	return strings.Join(lemmas, " ") + "|" + de
}

func dateRange(from, to string) string {
	switch {
	case from == "" && to == "":
		return ""
	case from == to:
		return from
	}
	return from + ".." + to
}

// aesHieroglyphs joins the tokens' spellings when every token has one.
func aesHieroglyphs(s aes.Sentence) string {
	words := make([]string, len(s.Tokens))
	for i, t := range s.Tokens {
		h := t.Hieroglyphs()
		if h == "" || !tla.IsHieroglyphText(h) {
			return ""
		}
		words[i] = h
	}
	return strings.Join(words, " ")
}

func stageName(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func manifestSource(key string, records int) ManifestSource {
	src, _ := source.Get(key)
	files := make([]string, 0, len(src.Files))
	for _, f := range src.Files {
		files = append(files, f.URL)
	}
	sort.Strings(files)
	return ManifestSource{Key: key, License: src.License, Attribution: src.Attribution, Files: files, Records: records}
}

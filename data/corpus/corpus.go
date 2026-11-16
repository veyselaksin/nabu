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
	"github.com/veyselaksin/nabu/data/source"
	"github.com/veyselaksin/nabu/data/tla"
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
}

// Manifest records what a build read and wrote.
type Manifest struct {
	Built      time.Time         `json:"built"`
	Sources    []ManifestSource  `json:"sources"`
	Sentences  int               `json:"sentences"`
	Lexicon    int               `json:"lexicon"`
	Duplicates map[string]int    `json:"duplicates_dropped"`
	Skipped    map[string]int    `json:"skipped"`
	Stages     map[string]int    `json:"sentences_by_stage"`
	Files      map[string]string `json:"files"`
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

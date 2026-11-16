// Package aes reads AES, the "Ancient Egyptian Sentences" corpus
// (github.com/simondschweitzer/aes, CC BY-SA 4.0): JSON files that map a
// sentence ID to its text ID, date, German translation and word tokens.
//
// AES comes from the January 2018 extract of the BBAW database and writes
// transliteration in the conventions of that time. ConvertForm brings a form
// to the conventions of the TLA v18/v19 datasets (Leiden Unified
// Transliteration), which the rest of Nabu uses.
package aes

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Token is one word form of a sentence.
type Token struct {
	// Form is the written form as published (2018 conventions).
	Form      string `json:"written_form"`
	LemmaID   string `json:"lemmaID"`
	LemmaForm string `json:"lemma_form"`
	// Cotext is the editor's German translation of the word in context.
	Cotext string `json:"cotext_translation"`
	// HieroUnicode is the spelling as HTML character references
	// ("&#x133d9;"); see Hieroglyphs. Present only for complete spellings.
	HieroUnicode string `json:"hiero_unicode"`
}

// Hieroglyphs returns the token's spelling in Unicode hieroglyphs, or "".
func (t Token) Hieroglyphs() string { return html.UnescapeString(t.HieroUnicode) }

// Sentence is one AES sentence.
type Sentence struct {
	ID     string `json:"-"`
	TextID string `json:"text"`
	Corpus string `json:"corpus"`
	// Date is a coarse period: "OK & FIP", "MK & SIP", "NK",
	// "TIP - Roman times" or "unknown".
	Date string `json:"date"`
	// Translation is the editor's German translation of the sentence.
	Translation string  `json:"sentence_translation"`
	Tokens      []Token `json:"token"`
}

// ReadFile parses one AES JSON file. Sentences are sorted by ID.
func ReadFile(path string) ([]Sentence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	var byID map[string]Sentence
	if err := json.Unmarshal(data, &byID); err != nil {
		return nil, fmt.Errorf("aes: %s: %w", path, err)
	}
	out := make([]Sentence, 0, len(byID))
	for id, s := range byID {
		s.ID = id
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ReadDir parses every _aes_*.json file in dir, in file name order.
func ReadDir(dir string) ([]Sentence, error) {
	files, err := filepath.Glob(filepath.Join(dir, "_aes_*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("aes: no _aes_*.json files in %s (run: nabu-data fetch aes)", dir)
	}
	sort.Strings(files)
	var out []Sentence
	for _, f := range files {
		s, err := ReadFile(f)
		if err != nil {
			return nil, err
		}
		out = append(out, s...)
	}
	return out, nil
}

// Transliteration returns the sentence's forms converted with ConvertForm,
// space-separated.
func (s Sentence) Transliteration() string {
	words := make([]string, 0, len(s.Tokens))
	for _, t := range s.Tokens {
		if f := ConvertForm(t.Form); f != "" {
			words = append(words, f)
		}
	}
	return strings.Join(words, " ")
}

// LemmaIDs returns the tokens' lemma IDs (empty strings for unlemmatized
// tokens), which the TLA datasets share.
func (s Sentence) LemmaIDs() []string {
	out := make([]string, len(s.Tokens))
	for i, t := range s.Tokens {
		out[i] = t.LemmaID
	}
	return out
}

// Intact reports whether every token is fully preserved and read with
// certainty, like the sentences of the TLA "premium" datasets: no lacunae
// ("...", "…", "_") and no uncertain readings ("?", "⸮").
func (s Sentence) Intact() bool {
	for _, t := range s.Tokens {
		if strings.ContainsAny(t.Form, "_?⸮…") || strings.Contains(t.Form, "...") {
			return false
		}
	}
	return len(s.Tokens) > 0
}

var (
	// Signs written in error by the scribe, which editions enclose in
	// braces ("{k}nb", "{{ḥꜣ.t-ꞽy}}"): not part of the word.
	superfluous  = regexp.MustCompile(`\{+[^{}]*\}+`)
	pluralMarker = regexp.MustCompile(`\.pl\b`)
	dualMarker   = regexp.MustCompile(`\.du\b`)
	// Editorial marks for damaged (⸢⸣), omitted (〈〉) and lost ([]) signs. The
	// TLA "premium" datasets contain only intact sentences, without them.
	// "!" marks a reading as sic.
	editorial = strings.NewReplacer("⸢", "", "⸣", "", "〈", "", "〉", "", "[", "", "]", "", "!", "")
	// Spellings that differ between the 2018 extract and the TLA datasets.
	spelling = strings.NewReplacer(
		"ı͗", "ꞽ", // yod written with dotless i + U+0357 or i + U+0357 before U+A7BD existed
		"i͗", "ꞽ",
		"j", "ꞽ", // yod: TLA v18 writes U+A7BD (Leiden Unified Transliteration)
		",", ".", // boundary before endings such as the feminine t
		"≡", "=", // suffix pronoun inside compounds and names
		"⸗", "=", // suffix pronoun (Late Egyptian style)
	)
)

// ConvertForm converts an AES (2018) transliteration to the conventions of
// the TLA v18 datasets: NFC, lower case (TLA does not capitalize names),
// "j" (and the older encodings "ı͗"/"i͗") → "ꞽ", "," → ".", "≡"/"⸗" → "=",
// ".pl"/".du" → ".PL"/".DU", editorial brackets for damaged, omitted and lost
// signs and the sic mark "!" removed, and signs the scribe wrote in error
// ("{…}") dropped. Round brackets are kept, as in the TLA.
//
// The rules were measured on the 5,600 AES sentences that also occur in TLA
// Earlier Egyptian v18 or Late Egyptian v19 (same lemma IDs and German
// translation): 96.1% of 15,932 tokens convert to exactly the TLA form; the
// rest differ in editorial readings (e.g. ḥtp-ḏi̯ vs. ḥtp-ḏꞽ), not in
// conventions.
func ConvertForm(s string) string {
	s = strings.ToLower(norm.NFC.String(strings.TrimSpace(s)))
	s = superfluous.ReplaceAllString(s, "")
	s = editorial.Replace(spelling.Replace(s))
	s = pluralMarker.ReplaceAllString(s, ".PL")
	s = dualMarker.ReplaceAllString(s, ".DU")
	return norm.NFC.String(s)
}

// Stage maps an AES date to a language stage where the date settles it:
// "earlier" (Old and Middle Egyptian) for the Old and Middle Kingdom. Later
// texts may be in Middle Egyptian, Late Egyptian or Demotic, so they get ""
// (unknown stage).
func Stage(date string) string {
	switch date {
	case "OK & FIP", "MK & SIP":
		return "earlier"
	}
	return ""
}

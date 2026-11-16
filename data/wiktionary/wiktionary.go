// Package wiktionary reads English Wiktionary entries as extracted by
// Wiktextract (the per-language JSON-lines files of kaikki.org). Wiktionary
// text is CC BY-SA 4.0 (and GFDL); it is community-edited, so what it says
// about pronunciation is one labeled view, not a consensus.
package wiktionary

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Entry is one dictionary entry (a word in one part of speech and etymology).
type Entry struct {
	// Word is the headword as Wiktionary writes it (for Egyptian: "j" for
	// yod, no separators before endings).
	Word string
	POS  string
	// Glosses are the English glosses of the senses, in order, without
	// senses that only point to another entry ("alternative form of …").
	Glosses []string
	// Pronunciations are the IPA transcriptions with Wiktionary's own label.
	Pronunciations []Pronunciation
}

// Pronunciation is an IPA transcription and what it claims to be: for
// Egyptian either "modern Egyptological" (the conventional reading used in
// teaching) or a reconstruction for a dated stage ("Middle Egyptian, c.
// 1700 BCE").
type Pronunciation struct {
	Label string `json:"label"`
	IPA   string `json:"ipa"`
}

type record struct {
	Word   string `json:"word"`
	POS    string `json:"pos"`
	Senses []struct {
		Glosses []string        `json:"glosses"`
		AltOf   json.RawMessage `json:"alt_of"`
		FormOf  json.RawMessage `json:"form_of"`
	} `json:"senses"`
	Sounds []struct {
		Note string   `json:"note"`
		Tags []string `json:"tags"`
		IPA  string   `json:"ipa"`
	} `json:"sounds"`
}

// skipPOS are record types that are not words of the language.
var skipPOS = map[string]bool{"romanization": true, "symbol": true, "character": true}

// Read parses a Wiktextract JSON-lines file. Records that are not words
// (romanizations, signs) or have no gloss of their own are skipped.
func Read(r io.Reader) ([]Entry, error) {
	var out []Entry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 32<<20)
	line := 0
	for sc.Scan() {
		line++
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var rec record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return nil, fmt.Errorf("wiktionary: line %d: %w", line, err)
		}
		if rec.Word == "" || skipPOS[rec.POS] {
			continue
		}
		e := Entry{Word: norm.NFC.String(rec.Word), POS: rec.POS}
		for _, s := range rec.Senses {
			if len(s.AltOf) > 0 || len(s.FormOf) > 0 || len(s.Glosses) == 0 {
				continue
			}
			// The last gloss is the most specific (earlier ones are headings).
			if g := strings.Join(strings.Fields(s.Glosses[len(s.Glosses)-1]), " "); g != "" {
				e.Glosses = append(e.Glosses, g)
			}
		}
		if len(e.Glosses) == 0 {
			continue
		}
		seen := map[Pronunciation]bool{}
		for _, s := range rec.Sounds {
			label := s.Note
			if label == "" {
				label = strings.Join(s.Tags, ", ")
			}
			// "reconstructed" repeats the dated reconstructions without the date.
			if s.IPA == "" || label == "" || label == "reconstructed" {
				continue
			}
			p := Pronunciation{Label: label, IPA: norm.NFC.String(s.IPA)}
			if !seen[p] {
				seen[p] = true
				e.Pronunciations = append(e.Pronunciations, p)
			}
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("wiktionary: %w", err)
	}
	return out, nil
}

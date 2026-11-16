// Package aed reads the entry list of AED, the Ancient Egyptian Dictionary
// (simondschweitzer.github.io/aed, CC BY-SA 4.0): one HTML page,
// dictionary_entries.html, with a link per lemma whose text is
//
//	FORM, "GERMAN" | "ENGLISH" [PART OF SPEECH] (REFERENCES)
//
// where "ENGLISH" may be the words `english translation missing`. Lemma IDs
// are the ones the TLA and AES lemmatizations use.
package aed

import (
	"fmt"
	"html"
	"io"
	"regexp"
	"strings"

	"github.com/veyselaksin/nabu/data/aes"
)

// Entry is one dictionary lemma.
type Entry struct {
	ID string
	// Form is the lemma as published (2018 conventions); see aes.ConvertForm.
	Form         string
	German       string
	English      string // "" when AED has no English translation
	POS          string
	Bibliography string
}

var (
	link  = regexp.MustCompile(`(?s)<a href="([^"/]+)\.html">(.*?)</a>`)
	entry = regexp.MustCompile(`(?s)^(.*?), "(.*)" \| (?:"(.*)"|english translation missing) \[([^\]]*)\](?: \((.*)\))?$`)
)

// Read parses dictionary_entries.html. Links that are not entries (the
// imprint) are skipped; an empty result is an error.
func Read(r io.Reader) ([]Entry, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("aed: %w", err)
	}
	var out []Entry
	for _, m := range link.FindAllStringSubmatch(string(data), -1) {
		text := strings.TrimSpace(html.UnescapeString(m[2]))
		e := entry.FindStringSubmatch(text)
		if e == nil {
			continue
		}
		out = append(out, Entry{
			ID: m[1], Form: strings.TrimSpace(e[1]), German: e[2], English: e[3],
			POS: e[4], Bibliography: e[5],
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("aed: no dictionary entries found")
	}
	return out, nil
}

// Lemma returns the entry's form in the TLA v18 conventions.
func (e Entry) Lemma() string { return aes.ConvertForm(e.Form) }

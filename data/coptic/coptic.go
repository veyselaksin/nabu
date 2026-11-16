// Package coptic reads the Comprehensive Coptic Lexicon of the Coptic
// Dictionary Online (KELLIA, TEI XML, CC BY-SA 4.0). Coptic is the last
// stage of Egyptian and, unlike the earlier scripts, writes vowels.
package coptic

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Entry is one lexicon entry.
type Entry struct {
	ID string
	// Lemma is the orthography of the lemma form (the first one listed).
	Lemma string
	// Dialects are the dialect sigla of the lemma form (S = Sahidic, B = Bohairic, …).
	Dialects []string
	POS      string
	// Glosses per language ("en", "de", "fr"), one per sense, in order.
	Glosses map[string][]string
}

type teiEntry struct {
	ID    string `xml:"http://www.w3.org/XML/1998/namespace id,attr"`
	Forms []struct {
		Type string   `xml:"type,attr"`
		Usg  []string `xml:"usg"`
		Orth []string `xml:"orth"`
		POS  string   `xml:"gramGrp>pos"`
	} `xml:"form"`
	POS    string `xml:"gramGrp>pos"`
	Senses []struct {
		Cits []struct {
			Type   string `xml:"type,attr"`
			Quotes []struct {
				Lang string `xml:"http://www.w3.org/XML/1998/namespace lang,attr"`
				Text string `xml:",chardata"`
			} `xml:"quote"`
		} `xml:"cit"`
	} `xml:"sense"`
}

// Read parses the lexicon. Entries without a lemma form are skipped.
func Read(r io.Reader) ([]Entry, error) {
	dec := xml.NewDecoder(r)
	var out []Entry
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("coptic: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "entry" {
			continue
		}
		var te teiEntry
		if err := dec.DecodeElement(&te, &se); err != nil {
			return nil, fmt.Errorf("coptic: entry: %w", err)
		}
		if e, ok := convert(te); ok {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("coptic: no entries found")
	}
	return out, nil
}

func convert(te teiEntry) (Entry, bool) {
	e := Entry{ID: te.ID, POS: clean(te.POS), Glosses: map[string][]string{}}
	for _, f := range te.Forms {
		if len(f.Orth) == 0 || (f.Type != "lemma" && e.Lemma != "") {
			continue
		}
		if e.Lemma == "" || f.Type == "lemma" {
			e.Lemma = clean(f.Orth[0])
			e.Dialects = nil
			for _, u := range f.Usg {
				if u = clean(u); u != "" {
					e.Dialects = append(e.Dialects, u)
				}
			}
			if e.POS == "" {
				e.POS = clean(f.POS)
			}
			if f.Type == "lemma" {
				break
			}
		}
	}
	if e.Lemma == "" {
		return Entry{}, false
	}
	for _, s := range te.Senses {
		for _, c := range s.Cits {
			if c.Type != "translation" {
				continue
			}
			for _, q := range c.Quotes {
				if t := clean(q.Text); t != "" && q.Lang != "" {
					e.Glosses[q.Lang] = append(e.Glosses[q.Lang], t)
				}
			}
		}
	}
	return e, true
}

func clean(s string) string { return norm.NFC.String(strings.Join(strings.Fields(s), " ")) }

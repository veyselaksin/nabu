// Package oraec reads data published by the ORAEC project
// (https://oraec.github.io).
package oraec

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// Etymology links a Coptic lemma to its Egyptian and Demotic predecessors
// (ORAEC coptic_etymologies, CC0). The IDs are those of the Coptic
// Dictionary Online ("C1494"), of the AED/TLA lemma list ("159410") and of
// the TLA's Demotic word list ("6439"); either predecessor may be missing.
// The links are scholarly judgments, and the file cites no source per row.
type Etymology struct {
	Coptic, Egyptian, Demotic string
}

// ReadEtymologies parses digitizing_coptic_etymologies_coptic_list_entries.csv:
// three columns, no header.
func ReadEtymologies(r io.Reader) ([]Etymology, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	var out []Etymology
	for line := 1; ; line++ {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("oraec etymologies: %w", err)
		}
		for i := range rec {
			rec[i] = strings.TrimSpace(strings.TrimPrefix(rec[i], "\xef\xbb\xbf"))
		}
		if len(rec) == 0 || rec[0] == "" {
			continue
		}
		if !strings.HasPrefix(rec[0], "C") {
			return nil, fmt.Errorf("oraec etymologies: line %d: %q is not a Coptic lemma ID", line, rec[0])
		}
		e := Etymology{Coptic: rec[0]}
		if len(rec) > 1 {
			e.Egyptian = rec[1]
		}
		if len(rec) > 2 {
			e.Demotic = rec[2]
		}
		out = append(out, e)
	}
	return out, nil
}

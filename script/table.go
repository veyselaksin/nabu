package script

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Sign lists and lexicons share one plain-text format so that they can be
// reviewed in a diff and edited in a spreadsheet:
//
//	#@id: egy-unikemet-uniliterals        metadata, "#@key: value"
//	# free comment                         ignored
//	value	codepoint	sign	…              column header (first non-comment line)
//	ꜣ	U+1313F	𓄿	…                      one record per line, tab-separated
//
// Every field must be valid, NFC-normalized UTF-8 without tabs or newlines.

// Source describes where a data file comes from and under which terms.
type Source struct {
	// ID identifies the data file, e.g. "egy-unikemet-uniliterals".
	ID string `json:"id"`
	// Name is a human-readable name of the upstream source.
	Name string `json:"name"`
	// URL is the exact upstream file the data was built from.
	URL string `json:"url"`
	// License is the upstream license, e.g. "Unicode-3.0" or "CC-BY-SA-4.0".
	License string `json:"license"`
	// Attribution is the credit line the license requires, if any.
	Attribution string `json:"attribution,omitempty"`
}

type table struct {
	meta    map[string]string
	columns []string
	rows    [][]string
	lines   []int // source line of each row, for error messages
}

func readTable(r io.Reader, source string, columns []string, requiredMeta []string) (*table, error) {
	t := &table{meta: map[string]string{}}
	var problems []string
	addf := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSuffix(sc.Text(), "\r")
		if !utf8.ValidString(text) {
			addf("line %d: not valid UTF-8", line)
			continue
		}
		switch {
		case strings.HasPrefix(text, "#@"):
			key, value, ok := strings.Cut(text[2:], ":")
			key = strings.TrimSpace(key)
			if !ok || key == "" {
				addf("line %d: metadata must look like \"#@key: value\"", line)
				continue
			}
			if _, dup := t.meta[key]; dup {
				addf("line %d: duplicate metadata key %q", line, key)
			}
			t.meta[key] = strings.TrimSpace(value)
		case strings.HasPrefix(text, "#"), strings.TrimSpace(text) == "" && t.columns == nil:
			// comment, or blank line before the header
		case t.columns == nil:
			t.columns = strings.Split(text, "\t")
			if !slices.Equal(t.columns, columns) {
				addf("line %d: columns are %q, want %q", line, t.columns, columns)
				return nil, &FormatError{Source: source, Problems: problems}
			}
		default:
			fields := strings.Split(text, "\t")
			if len(fields) != len(columns) {
				addf("line %d: has %d fields, want %d", line, len(fields), len(columns))
				continue
			}
			for i, f := range fields {
				if !norm.NFC.IsNormalString(f) {
					addf("line %d: %s %q must be NFC-normalized", line, columns[i], f)
				}
			}
			t.rows = append(t.rows, fields)
			t.lines = append(t.lines, line)
		}
	}
	if err := sc.Err(); err != nil {
		addf("reading: %v", err)
	}
	if t.columns == nil && len(problems) == 0 {
		addf("missing column header %q", strings.Join(columns, "\t"))
	}
	for _, k := range requiredMeta {
		if t.meta[k] == "" {
			addf("missing metadata \"#@%s: …\"", k)
		}
	}
	if len(problems) > 0 {
		return nil, &FormatError{Source: source, Problems: problems}
	}
	return t, nil
}

// writeTable writes metadata (in the order given by metaKeys, then any other
// keys sorted), free comments, the column header and rows.
func writeTable(w io.Writer, meta map[string]string, metaKeys []string, comments []string, columns []string, rows [][]string) error {
	bw := bufio.NewWriter(w)
	keys := slices.Clone(metaKeys)
	var extra []string
	for k := range meta {
		if !slices.Contains(metaKeys, k) {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	keys = append(keys, extra...)
	for _, k := range keys {
		v, ok := meta[k]
		if !ok {
			continue
		}
		if err := checkField(v); err != nil {
			return fmt.Errorf("metadata %s: %w", k, err)
		}
		fmt.Fprintf(bw, "#@%s: %s\n", k, v)
	}
	for _, c := range comments {
		for _, l := range strings.Split(c, "\n") {
			fmt.Fprintln(bw, strings.TrimRight("# "+l, " "))
		}
	}
	fmt.Fprintln(bw, strings.Join(columns, "\t"))
	for _, row := range rows {
		if len(row) != len(columns) {
			return fmt.Errorf("row %q has %d fields, want %d", row, len(row), len(columns))
		}
		for _, f := range row {
			if err := checkField(f); err != nil {
				return fmt.Errorf("row %q: %w", row, err)
			}
		}
		fmt.Fprintln(bw, strings.Join(row, "\t"))
	}
	return bw.Flush()
}

func checkField(s string) error {
	switch {
	case !utf8.ValidString(s):
		return fmt.Errorf("%q is not valid UTF-8", s)
	case strings.ContainsAny(s, "\t\r\n"):
		return fmt.Errorf("%q contains a tab or newline", s)
	case !norm.NFC.IsNormalString(s):
		return fmt.Errorf("%q is not NFC-normalized", s)
	}
	return nil
}

func (t *table) source() Source {
	return Source{
		ID:          t.meta["id"],
		Name:        t.meta["source"],
		URL:         t.meta["source_url"],
		License:     t.meta["license"],
		Attribution: t.meta["attribution"],
	}
}

// Command nabu-data downloads Nabu's upstream data and builds derived files
// from it.
//
//	nabu-data sources                 list sources and their licenses
//	nabu-data fetch unikemet tla      download pinned files (checksum-verified)
//	nabu-data build-signs             Unikemet + TLA → script/signs/egy-unikemet-uniliterals.tsv
//	nabu-data build-lexicon           TLA → $NABU_DATA_DIR/tla/lexicon-egy.tsv
//
// Downloads go to $NABU_DATA_DIR/<source> (default data/corpora/<source>,
// git-ignored). Run from the repository root.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/veyselaksin/nabu/data/source"
	"github.com/veyselaksin/nabu/data/tla"
	"github.com/veyselaksin/nabu/data/unikemet"
	"github.com/veyselaksin/nabu/script"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

const usage = `usage: nabu-data COMMAND [flags]

Commands:
  sources          list upstream sources and their licenses
  fetch KEY...     download a source's pinned files (e.g. fetch unikemet tla)
  build-signs      build the uniliteral sign list from Unikemet + TLA sign counts
  build-lexicon    build the TLA spelling lexicon (stays local; CC BY-SA 4.0)

Files are stored under $NABU_DATA_DIR/<source> (default data/corpora/<source>).
Run "nabu-data COMMAND -h" for a command's flags.
`

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stderr, usage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	var err error
	switch args[0] {
	case "sources":
		err = cmdSources(stdout, getenv)
	case "fetch":
		err = cmdFetch(ctx, args[1:], getenv, stdout, stderr)
	case "build-signs":
		err = cmdBuildSigns(args[1:], getenv, stdout, stderr)
	case "build-lexicon":
		err = cmdBuildLexicon(args[1:], getenv, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "nabu-data: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	var uerr usageError
	switch {
	case errors.As(err, &uerr):
		fmt.Fprintln(stderr, "nabu-data:", err)
		return 2
	case errors.Is(err, flag.ErrHelp):
		return 0
	case err != nil:
		fmt.Fprintln(stderr, "nabu-data:", err)
		return 1
	}
	return 0
}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func cmdSources(w io.Writer, getenv func(string) string) error {
	for _, s := range source.All() {
		license, sa := s.License, "unverified"
		if license == "" {
			license = "UNVERIFIED (check before downloading)"
		}
		if s.ShareAlike != nil {
			sa = map[bool]string{true: "yes", false: "no"}[*s.ShareAlike]
		}
		fmt.Fprintf(w, "%s\n  name:        %s\n  homepage:    %s\n  license:     %s %s\n  share-alike: %s\n  notes:       %s\n  local dir:   %s\n",
			s.Key, s.Name, s.Homepage, license, s.LicenseURL, sa, s.Notes, source.Dir(getenv, s.Key))
		for _, f := range s.Files {
			fmt.Fprintf(w, "  file:        %s\n               %s\n               sha256 %s\n", f.Name, f.URL, f.SHA256)
		}
		fmt.Fprintln(w)
	}
	return nil
}

func cmdFetch(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return usageError{"fetch: name at least one source (see: nabu-data sources)"}
	}
	client := &http.Client{Timeout: 15 * time.Minute}
	for _, key := range fs.Args() {
		src, ok := source.Get(key)
		if !ok {
			return usageError{fmt.Sprintf("fetch: unknown source %q (see: nabu-data sources)", key)}
		}
		if _, err := source.Fetch(ctx, client, src, source.Dir(getenv, key), stdout); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: license %s; attribution required: %s\n", key, src.License, src.Attribution)
	}
	return nil
}

// sourceFile returns the local path of a registered file and verifies its
// checksum, so that generated files can record exactly what they came from.
func sourceFile(getenv func(string) string, key, name, override string) (string, source.File, error) {
	src, _ := source.Get(key)
	var file source.File
	for _, f := range src.Files {
		if f.Name == name {
			file = f
		}
	}
	path := override
	if path == "" {
		path = filepath.Join(source.Dir(getenv, key), name)
	}
	if err := source.VerifyFile(path, file.SHA256); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", file, fmt.Errorf("%s not found; run: nabu-data fetch %s", path, key)
		}
		return "", file, err
	}
	return path, file, nil
}

func cmdBuildSigns(args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("build-signs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ukPath := fs.String("unikemet", "", "Unikemet.txt (default: $NABU_DATA_DIR/unikemet/Unikemet.txt)")
	tlaPath := fs.String("tla", "", "TLA train.jsonl used for sign counts (default: $NABU_DATA_DIR/tla/train.jsonl)")
	out := fs.String("out", "script/signs/egy-unikemet-uniliterals.tsv", "output sign list")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ukFile, uk, err := sourceFile(getenv, "unikemet", "Unikemet.txt", *ukPath)
	if err != nil {
		return err
	}
	tlaFile, tl, err := sourceFile(getenv, "tla", "train.jsonl", *tlaPath)
	if err != nil {
		return err
	}
	f, err := os.Open(ukFile)
	if err != nil {
		return err
	}
	records, version, err := unikemet.Parse(f)
	f.Close()
	if err != nil {
		return err
	}
	sentences, err := readTLA(tlaFile)
	if err != nil {
		return err
	}
	rows, skipped := unikemet.Uniliterals(records, tla.SignCounts(sentences))
	ukSrc, _ := source.Get("unikemet")

	meta := map[string]string{
		"id":            "egy-unikemet-uniliterals",
		"language":      "egy",
		"source":        "Unicode Unikemet database " + version,
		"source_url":    uk.URL,
		"source_sha256": uk.SHA256,
		"license":       ukSrc.License,
		"attribution":   ukSrc.Attribution,
		"selection": "most frequent candidate in TLA Earlier Egyptian v18 hieroglyphs " +
			"(" + tl.URL + ", sha256 " + tl.SHA256 + ")",
	}
	comments := []string{
		"Generated by `nabu-data build-signs`. Do not edit by hand; see README.md.",
		"",
		"One sign per uniliteral value: Unikemet signs whose kEH_Func is exactly",
		"\"Phonemogram\" and whose kEH_FVal is a single transliteration character.",
		"Where several signs share a value, the one occurring most often in the TLA",
		"corpus is chosen (corpus_count); `candidates` is how many signs share it.",
		"These signs spell words letter by letter, which is NOT how Egyptian scribes",
		"wrote; the renderer labels such words as phonetic.",
	}
	for _, s := range skipped {
		comments = append(comments, fmt.Sprintf("Skipped value %q (%d candidates): %s.", s.Value, s.Candidates, s.Reason))
	}
	var buf bytes.Buffer
	if err := script.WriteSignList(&buf, meta, comments, rows); err != nil {
		return err
	}
	// Round-trip through the parser so that a bad file is never written.
	if _, err := script.ParseSignList(bytes.NewReader(buf.Bytes()), *out); err != nil {
		return err
	}
	if err := writeFile(*out, buf.Bytes()); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s: %d values, %d skipped\n", *out, len(rows), len(skipped))
	for _, s := range skipped {
		fmt.Fprintf(stdout, "  skipped %q (%d candidates): %s\n", s.Value, s.Candidates, s.Reason)
	}
	return nil
}

func cmdBuildLexicon(args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("build-lexicon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tlaPath := fs.String("tla", "", "TLA train.jsonl (default: $NABU_DATA_DIR/tla/train.jsonl)")
	out := fs.String("out", "", "output lexicon (default: $NABU_DATA_DIR/tla/lexicon-egy.tsv)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		*out = filepath.Join(source.Dir(getenv, "tla"), "lexicon-egy.tsv")
	}
	if rel, err := filepath.Rel(".", *out); err == nil && strings.HasPrefix(rel, "script"+string(filepath.Separator)) {
		return usageError{"build-lexicon: the TLA lexicon is CC BY-SA 4.0 and must not be written into script/ (it would be embedded and committed)"}
	}
	tlaFile, tl, err := sourceFile(getenv, "tla", "train.jsonl", *tlaPath)
	if err != nil {
		return err
	}
	sentences, err := readTLA(tlaFile)
	if err != nil {
		return err
	}
	entries, st := tla.Lexicon(sentences)
	src, _ := source.Get("tla")
	meta := map[string]string{
		"id":            "egy-tla-v18-lexicon",
		"language":      "egy",
		"source":        src.Name,
		"source_url":    tl.URL,
		"source_sha256": tl.SHA256,
		"license":       src.License,
		"attribution":   src.Attribution,
	}
	comments := []string{
		"Generated by `nabu-data build-lexicon`. Derived from the TLA corpus and therefore",
		"licensed CC BY-SA 4.0 with the attribution above. Do not commit this file.",
		"",
		"form: a transliterated sentence element (NFC, editor's round brackets removed).",
		"hieroglyphs: its most frequent spelling; count: occurrences of that spelling;",
		"total: occurrences of the form; variants: distinct spellings of the form.",
		fmt.Sprintf("Built from %d sentences: %d aligned elements, %d skipped (signs not in Unicode), %d misaligned sentences.",
			st.Sentences, st.Elements, st.NonUnicodeSpelling, st.MisalignedSentence),
	}
	var buf bytes.Buffer
	if err := script.WriteLexicon(&buf, meta, comments, entries); err != nil {
		return err
	}
	if _, err := script.ParseLexicon(bytes.NewReader(buf.Bytes()), *out); err != nil {
		return err
	}
	if err := writeFile(*out, buf.Bytes()); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s: %d forms from %d sentences (%d elements skipped: signs not in Unicode)\n",
		*out, st.Forms, st.Sentences, st.NonUnicodeSpelling)
	fmt.Fprintf(stdout, "license: %s (share-alike); keep the attribution with anything built from it:\n  %s\n",
		src.License, src.Attribution)
	return nil
}

func readTLA(path string) ([]tla.Sentence, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return tla.Read(f)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

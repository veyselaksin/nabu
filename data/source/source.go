// Package source is the registry of upstream data Nabu can download, with
// the license terms recorded for each. Keep it in sync with DATA_LICENSES.md.
//
// Downloads are pinned: every file has an exact URL (a versioned path or a
// commit revision) and a SHA-256, and a download whose checksum differs is
// rejected, so that a published model or sign list can name exactly what it
// was built from.
package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Source is one upstream data source.
type Source struct {
	Key      string
	Name     string
	Homepage string
	// License is an SPDX identifier, or "" if not yet verified.
	License    string
	LicenseURL string
	// ShareAlike is nil if not yet verified.
	ShareAlike *bool
	// Attribution is the credit line the license requires.
	Attribution string
	Notes       string
	Files       []File
}

// File is one pinned upstream file.
type File struct {
	Name   string // local file name
	URL    string
	SHA256 string
}

// Verified reports whether the license has been checked and recorded.
func (s Source) Verified() bool { return s.License != "" && s.ShareAlike != nil }

func ptr(b bool) *bool { return &b }

// TLAAttribution is the citation the TLA dataset card asks for.
const TLAAttribution = "Thesaurus Linguae Aegyptiae, Original Earlier Egyptian sentences, corpus v18, premium, " +
	"https://huggingface.co/datasets/thesaurus-linguae-aegyptiae/tla-Earlier_Egyptian_original-v18-premium, " +
	"v1.1, 2/16/2024, ed. by Tonio Sebastian Richter & Daniel A. Werning on behalf of the " +
	"Berlin-Brandenburgische Akademie der Wissenschaften and Hans-Werner Fischer-Elfert & Peter Dils " +
	"on behalf of the Sächsische Akademie der Wissenschaften zu Leipzig. CC BY-SA 4.0."

var registry = []Source{
	{
		Key:         "unikemet",
		Name:        "Unicode Unikemet database (Egyptian hieroglyphs), Unicode 18.0.0",
		Homepage:    "https://www.unicode.org/reports/tr57/",
		License:     "Unicode-3.0",
		LicenseURL:  "https://www.unicode.org/license.txt",
		ShareAlike:  ptr(false),
		Attribution: "Unicode Unikemet database 18.0.0, © Unicode, Inc., Unicode License v3.",
		Notes:       "Sign values (kEH_FVal) and functions (kEH_Func) for every encoded hieroglyph.",
		Files: []File{{
			Name:   "Unikemet.txt",
			URL:    "https://www.unicode.org/Public/18.0.0/ucd/Unikemet.txt",
			SHA256: "a6c03de18f40f54df5bf3ea6c3152f23075ba8647520e98ea80e8e0352d65a33",
		}},
	},
	{
		Key:         "tla",
		Name:        "TLA: Thesaurus Linguae Aegyptiae, Original Earlier Egyptian sentences, corpus v18, premium",
		Homepage:    "https://thesaurus-linguae-aegyptiae.de/",
		License:     "CC-BY-SA-4.0",
		LicenseURL:  "https://creativecommons.org/licenses/by-sa/4.0/",
		ShareAlike:  ptr(true),
		Attribution: TLAAttribution,
		Notes: "12,773 intact Old and Middle Egyptian sentences with aligned hieroglyphs, transliteration " +
			"and lemmatization. Share-alike: anything built from it (the lexicon) stays local and is not committed.",
		Files: []File{{
			Name: "train.jsonl",
			URL: "https://huggingface.co/datasets/thesaurus-linguae-aegyptiae/tla-Earlier_Egyptian_original-v18-premium/" +
				"resolve/a7ba9e9d1a74a9a0f919b024ba84dd6d643b2966/train.jsonl",
			SHA256: "13d4f6e17e5348c4a1dcf1c02795c8401367e187eaf903da325a1cf83a998f99",
		}},
	},
	{
		Key:      "oracc",
		Name:     "ORACC: Open Richly Annotated Cuneiform Corpus (Sumerian)",
		Homepage: "https://oracc.museum.upenn.edu/",
		// TODO(source): verify license and share-alike terms; they may differ per ORACC sub-project.
		Notes: "Terms may vary per sub-project; record the license of each project used.",
	},
	{
		Key:      "cdli",
		Name:     "CDLI: Cuneiform Digital Library Initiative (Sumerian)",
		Homepage: "https://cdli.mpiwg-berlin.mpg.de/",
		// TODO(source): verify license and share-alike terms on the CDLI site.
		Notes: "Licensing may differ between catalogue data, transliterations and images.",
	},
}

// All returns every registered source, sorted by key.
func All() []Source {
	out := append([]Source(nil), registry...)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Get returns the source with the given key.
func Get(key string) (Source, bool) {
	for _, s := range registry {
		if s.Key == key {
			return s, true
		}
	}
	return Source{}, false
}

// Dir returns the local directory for a source's files: $NABU_DATA_DIR/<key>,
// defaulting to data/corpora/<key> (git-ignored).
func Dir(getenv func(string) string, key string) string {
	root := getenv("NABU_DATA_DIR")
	if root == "" {
		root = filepath.Join("data", "corpora")
	}
	return filepath.Join(root, key)
}

// ErrUnverified is returned when downloading a source whose license has not
// been verified.
var ErrUnverified = errors.New("source license not verified; record it in data/source and DATA_LICENSES.md first")

// ChecksumError reports a download or local file whose SHA-256 differs from
// the pinned one.
type ChecksumError struct {
	Path, Want, Got string
}

func (e *ChecksumError) Error() string {
	return fmt.Sprintf("%s: sha256 is %s, want %s (upstream changed or the download is corrupt)", e.Path, e.Got, e.Want)
}

// Fetch downloads every file of src into dir, verifying checksums. Files
// already present with the right checksum are kept. It returns the paths.
func Fetch(ctx context.Context, client *http.Client, src Source, dir string, log io.Writer) ([]string, error) {
	if !src.Verified() {
		return nil, fmt.Errorf("%s: %w", src.Key, ErrUnverified)
	}
	if len(src.Files) == 0 {
		return nil, fmt.Errorf("%s: no downloadable files are registered", src.Key)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var paths []string
	for _, f := range src.Files {
		path := filepath.Join(dir, f.Name)
		if err := VerifyFile(path, f.SHA256); err == nil {
			fmt.Fprintf(log, "%s: already present, checksum ok\n", path)
			paths = append(paths, path)
			continue
		}
		fmt.Fprintf(log, "%s: downloading %s\n", path, f.URL)
		if err := download(ctx, client, f, path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// VerifyFile checks that the file at path has the given SHA-256.
func VerifyFile(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return &ChecksumError{Path: path, Want: want, Got: got}
	}
	return nil
}

func download(ctx context.Context, client *http.Client, f File, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "nabu-data")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", f.URL, resp.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("GET %s: %w", f.URL, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != f.SHA256 {
		return &ChecksumError{Path: f.URL, Want: f.SHA256, Got: got}
	}
	return os.Rename(tmp.Name(), path)
}

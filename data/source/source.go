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

// tlaEditors is the editors' line every TLA dataset card asks to be cited.
const tlaEditors = "ed. by Tonio Sebastian Richter & Daniel A. Werning on behalf of the " +
	"Berlin-Brandenburgische Akademie der Wissenschaften and Hans-Werner Fischer-Elfert & Peter Dils " +
	"on behalf of the Sächsische Akademie der Wissenschaften zu Leipzig. CC BY-SA 4.0."

// aedContributors credits the editors of the BBAW database extract (January
// 2018) that AES and AED are built from, as both READMEs list them.
const aedContributors = "Teilauszug der Datenbank des Vorhabens \"Strukturen und Transformationen des Wortschatzes " +
	"der ägyptischen Sprache\" vom Januar 2018 (https://nbn-resolving.org/urn:nbn:de:kobv:b4-opus4-29190), " +
	"with contributions of Burkhard Backes, Susanne Beck, Anke Blöbaum, Angela Böhme, Marc Brose, " +
	"Adelheid Burkhardt, Roberto A. Díaz Hernández, Peter Dils, Roland Enmarch, Frank Feder, Heinz Felber, " +
	"Silke Grallert, Stefan Grunert, Ingelore Hafemann, Anne Herzberg, John M. Iskander, Ines Köhler, " +
	"Maxim Kupreyev, Renata Landgrafova, Verena Lepper, Lutz Popko, Alexander Schütze, Simon Schweitzer, " +
	"Stephan Seidlmayer, Gunnar Sperveslage, Susanne Töpfer, Doris Topmann, Anja Weber"

const aesBase = "https://raw.githubusercontent.com/simondschweitzer/aes/35276d2527cca1a055e31ed5f6683e777717170f/files/aes/"

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
		Key:        "tla-late",
		Name:       "TLA: Thesaurus Linguae Aegyptiae, Late Egyptian sentences, corpus v19, premium",
		Homepage:   "https://thesaurus-linguae-aegyptiae.de/",
		License:    "CC-BY-SA-4.0",
		LicenseURL: "https://creativecommons.org/licenses/by-sa/4.0/",
		ShareAlike: ptr(true),
		Attribution: "Thesaurus Linguae Aegyptiae, Late Egyptian sentences, corpus v19, premium, " +
			"https://huggingface.co/datasets/thesaurus-linguae-aegyptiae/tla-late_egyptian-v19-premium, v1.0, 1/19/2025, " +
			tlaEditors,
		Notes: "3,606 intact Late Egyptian sentences, same schema as tla (suffix pronouns written with ⸗).",
		Files: []File{{
			Name: "train.jsonl",
			URL: "https://huggingface.co/datasets/thesaurus-linguae-aegyptiae/tla-late_egyptian-v19-premium/" +
				"resolve/8af85941783ba575d5a8985c80c688f948c50040/train.jsonl",
			SHA256: "dfded75c7d34bc1f2f12e18af6936394dc0c5cadacd1229dc28d46751d6389df",
		}},
	},
	{
		Key:        "tla-demotic",
		Name:       "TLA: Thesaurus Linguae Aegyptiae, Demotic sentences, corpus v18, premium",
		Homepage:   "https://thesaurus-linguae-aegyptiae.de/",
		License:    "CC-BY-SA-4.0",
		LicenseURL: "https://creativecommons.org/licenses/by-sa/4.0/",
		ShareAlike: ptr(true),
		Attribution: "Thesaurus Linguae Aegyptiae, Demotic sentences, corpus v18, premium, " +
			"https://huggingface.co/datasets/thesaurus-linguae-aegyptiae/tla-demotic-v18-premium, v1.1, 2/16/2024, " +
			tlaEditors,
		Notes: "13,383 intact Demotic sentences with transliteration and German translation (no hieroglyphs).",
		Files: []File{{
			Name: "train.jsonl",
			URL: "https://huggingface.co/datasets/thesaurus-linguae-aegyptiae/tla-demotic-v18-premium/" +
				"resolve/c02baaaddaf0e2b6b9d128874df23c655eedfe7c/train.jsonl",
			SHA256: "787e9ce8b5005a056f4579723ccffeaa5db1ed0bd7f42b7e5d5ec34d6867a63f",
		}},
	},
	{
		Key:         "aes",
		Name:        "AES: Ancient Egyptian Sentences (from AED-TEI), 2021",
		Homepage:    "https://github.com/simondschweitzer/aes",
		License:     "CC-BY-SA-4.0",
		LicenseURL:  "https://creativecommons.org/licenses/by-sa/4.0/",
		ShareAlike:  ptr(true),
		Attribution: "AES - Ancient Egyptian Sentences, data transformation by Simon D. Schweitzer (2020-2021), from " + aedContributors + ". CC BY-SA 4.0.",
		Notes: "More than 100,000 sentences of all periods with German translations, lemma IDs (shared with the TLA) " +
			"and partial hieroglyphs. Uses the 2018 transliteration conventions (j for yod, \",\" before feminine t, " +
			"capitalized names); build-corpus converts them to the TLA v18 conventions.",
		Files: []File{
			{Name: "_aes_bbawamarna.json", URL: aesBase + "_aes_bbawamarna.json", SHA256: "c32d2ee56eddbb9816ed35a3e72484997d9a367bad8484e8048156f1e0ade7b7"},
			{Name: "_aes_bbawarchive.json", URL: aesBase + "_aes_bbawarchive.json", SHA256: "10dd9721b1d32bd0bef559839f2ad43ff0d64bfc7c501505d59e8648aaa45b45"},
			{Name: "_aes_bbawbriefe.json", URL: aesBase + "_aes_bbawbriefe.json", SHA256: "97929b4fb89f8336bbd5c87f98b1ae781e406d1b27b38475d0375760d91a75b0"},
			{Name: "_aes_bbawfelsinschriften.json", URL: aesBase + "_aes_bbawfelsinschriften.json", SHA256: "d10af1f7b4a1055beeba4dc3d7f754f985a05b74e3a65bc0487f5a6857878bce"},
			{Name: "_aes_bbawgrabinschriften.json", URL: aesBase + "_aes_bbawgrabinschriften.json", SHA256: "eb5b4a2e63d3bca7502bd0b3c56ffae8c4e937f3f52e2220a3c30b3f6d36c7db"},
			{Name: "_aes_bbawgraeberspzt.json", URL: aesBase + "_aes_bbawgraeberspzt.json", SHA256: "9b80027f9b30a53f75976021cd909f0bf0f843274ebbbac133791525a073eb1e"},
			{Name: "_aes_bbawhistbiospzt.json", URL: aesBase + "_aes_bbawhistbiospzt.json", SHA256: "0c276429b48316294a093994c8eb0b732ad4f2334187672a3d4c393794735397"},
			{Name: "_aes_bbawpyramidentexte.json", URL: aesBase + "_aes_bbawpyramidentexte.json", SHA256: "c82b275a2f5b70cb50b919d68f2113ef1d2c5cc1d50602718aa7931fa5f6ccf0"},
			{Name: "_aes_bbawramessiden.json", URL: aesBase + "_aes_bbawramessiden.json", SHA256: "acef9ecaa13f9aa6589b4c59241c0c9aec3f16918cbf177b71914553e17a583f"},
			{Name: "_aes_bbawtempelbib.json", URL: aesBase + "_aes_bbawtempelbib.json", SHA256: "4308cb1af531ab13098b4cda19e64747b17f0bf95e4a24d0227af6a45dc6375c"},
			{Name: "_aes_bbawtotenlit.json", URL: aesBase + "_aes_bbawtotenlit.json", SHA256: "86bc7980c0f4e869d2f1bbb138bc0ed2cbe6b55a4a30e0d0507822277ca61f4a"},
			{Name: "_aes_sawlit.json", URL: aesBase + "_aes_sawlit.json", SHA256: "b3d5eb695584be976b9981392eb4e149d13f447f677cf445b5a954898bc82ef4"},
			{Name: "_aes_sawmedizin.json", URL: aesBase + "_aes_sawmedizin.json", SHA256: "ce94fd4e33d070c70f0a128258e04b3dab10261bbac2e39a52b53b5c92353306"},
			{Name: "_aes_smaek.json", URL: aesBase + "_aes_smaek.json", SHA256: "2549cb017f9c78601233b8a2b6ecfcd6ff16e7240c2131dd1167ad98d3f98d30"},
			{Name: "_aes_tb.json", URL: aesBase + "_aes_tb.json", SHA256: "4ec0b282a911d22d6bf9f0c51e0f9da9df30273ddd89bbec70d038de2227b2d5"},
			{Name: "_aes_tuebingerstelen.json", URL: aesBase + "_aes_tuebingerstelen.json", SHA256: "01fc74baa5837c17f1d7f313926f92c063a6b2da4743d0bd5b6f254a5c22e2c5"},
		},
	},
	{
		Key:         "aed",
		Name:        "AED: Ancient Egyptian Dictionary, version 1.0 (dictionary entry list)",
		Homepage:    "https://simondschweitzer.github.io/aed/",
		License:     "CC-BY-SA-4.0",
		LicenseURL:  "https://creativecommons.org/licenses/by-sa/4.0/",
		ShareAlike:  ptr(true),
		Attribution: "AED - Ancient Egyptian Dictionary, version 1.0, data transformation by Simon D. Schweitzer, from " + aedContributors + ". CC BY-SA 4.0.",
		Notes:       "More than 30,000 lemmas with German and (mostly) English translations, part of speech and references.",
		Files: []File{{
			Name:   "dictionary_entries.html",
			URL:    "https://raw.githubusercontent.com/simondschweitzer/aed/0d7fb01defcdd49e83c7fc01aece845f03269fa9/dictionary_entries.html",
			SHA256: "44778be535cebd6814465e3914d6a9c365acdc5d85d21d9db3a800ebdc2e0271",
		}},
	},
	{
		Key:        "coptic-dict",
		Name:       "Coptic Dictionary Online: Comprehensive Coptic Lexicon v1.2 (KELLIA)",
		Homepage:   "https://coptic-dictionary.org/",
		License:    "CC-BY-SA-4.0",
		LicenseURL: "https://creativecommons.org/licenses/by-sa/4.0/",
		ShareAlike: ptr(true),
		Attribution: "Coptic Dictionary Online, ed. by the Koptische/Coptic Electronic Language and Literature " +
			"International Alliance (KELLIA), Comprehensive Coptic Lexicon v1.2 (2020), DOI 10.17169/refubium-2333. CC BY-SA 4.0.",
		Notes: "TEI lexicon of Coptic, the last stage of Egyptian: lemmas with English, German and French glosses.",
		Files: []File{{
			Name: "Comprehensive_Coptic_Lexicon-v1.2-2020.xml",
			URL: "https://raw.githubusercontent.com/KELLIA/dictionary/f365e2d239bd12b6897f3a761487e40d384b2b24/" +
				"xml/Comprehensive_Coptic_Lexicon-v1.2-2020.xml",
			SHA256: "ace84fce11b76386a6adb9888b2cb947056954fc9fe9c04aa0676b1f595fd43d",
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

// Dir returns the local directory for a source's files: <Root>/<key>.
func Dir(getenv func(string) string, key string) string {
	return filepath.Join(Root(getenv), key)
}

// Root returns the corpus root: $NABU_DATA_DIR, else $NABU_HOME/datasets,
// else $MODELS_ROOT/projects/nabu/datasets (a shared model drive), else
// data/corpora (git-ignored). Use CheckRoot before writing to it.
func Root(getenv func(string) string) string {
	if d := getenv("NABU_DATA_DIR"); d != "" {
		return d
	}
	if h := getenv("NABU_HOME"); h != "" {
		return filepath.Join(h, "datasets")
	}
	if m := getenv("MODELS_ROOT"); m != "" {
		return filepath.Join(m, "projects", "nabu", "datasets")
	}
	return filepath.Join("data", "corpora")
}

// CheckRoot reports an error when Root would come from a MODELS_ROOT that is
// not a directory (an unmounted drive): creating directories under its mount
// point would write to the internal disk instead.
func CheckRoot(getenv func(string) string) error {
	if getenv("NABU_DATA_DIR") != "" || getenv("NABU_HOME") != "" {
		return nil
	}
	m := getenv("MODELS_ROOT")
	if m == "" {
		return nil
	}
	if fi, err := os.Stat(m); err != nil || !fi.IsDir() {
		return fmt.Errorf("MODELS_ROOT=%q does not exist (drive not mounted?); mount it or unset MODELS_ROOT", m)
	}
	return nil
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

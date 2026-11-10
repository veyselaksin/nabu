// Command nabu-tts synthesizes speech with an exported Nabu TTS model, using
// the same Go runtime as the API (no Python).
//
// One utterance, from a transliteration (G2P with a rule set) or from IPA:
//
//	nabu-tts --model runs/smoke/model.onnx --ruleset egy-allen -o out.wav "ꜥnḫ"
//	nabu-tts --model runs/smoke/model.onnx --ipa -o out.wav "ʕnχ"
//
// Evaluation samples: render a fixed list for listening and comparing
// checkpoints (see eval/samples/README.md):
//
//	nabu-tts --model runs/full/model.onnx --samples eval/testsets/tts-samples.tsv \
//	  --out-dir eval/samples --label step-200000
//
// Writes <out-dir>/<label>/<id>.wav, <id>.txt and index.tsv, then regenerates
// <out-dir>/index.html with one column per label.
//
// The ONNX Runtime library comes from --ort-lib, $NABU_ORT_LIB, or the
// loader's default search path.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/veyselaksin/nabu/g2p"
	"github.com/veyselaksin/nabu/g2p/rules"
	"github.com/veyselaksin/nabu/tts"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type config struct {
	model, ortLib, ruleset, rulesDir, out, samples, outDir, label string
	ipa                                                           bool
	speaker                                                       int
	speed                                                         float64
	allSpeakers                                                   bool
}

func run(args []string, stdout, stderr io.Writer) int {
	var c config
	fs := flag.NewFlagSet("nabu-tts", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&c.model, "model", os.Getenv("NABU_TTS_MODEL"), "exported .onnx model (default $NABU_TTS_MODEL)")
	fs.StringVar(&c.ortLib, "ort-lib", os.Getenv("NABU_ORT_LIB"), "ONNX Runtime shared library (default $NABU_ORT_LIB)")
	fs.StringVar(&c.ruleset, "ruleset", "egy-allen", "rule set id for G2P")
	fs.StringVar(&c.rulesDir, "rules-dir", "", "load *.yaml rule files from this directory instead of the embedded ones")
	fs.BoolVar(&c.ipa, "ipa", false, "TEXT is IPA; skip G2P")
	fs.IntVar(&c.speaker, "speaker", 0, "speaker index")
	fs.Float64Var(&c.speed, "speed", 1, "speaking rate (0.5–2)")
	fs.StringVar(&c.out, "o", "out.wav", "output WAV file (single utterance)")
	fs.StringVar(&c.samples, "samples", "", "TSV of evaluation inputs (id, ruleset, transliteration)")
	fs.StringVar(&c.outDir, "out-dir", "eval/samples", "with --samples: output directory")
	fs.StringVar(&c.label, "label", "", "with --samples: subdirectory name (default: model name)")
	fs.BoolVar(&c.allSpeakers, "all-speakers", false, "with --samples: render every speaker")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: nabu-tts --model M.onnx [--ruleset ID | --ipa] [-o out.wav] TEXT")
		fmt.Fprintln(stderr, "       nabu-tts --model M.onnx --samples FILE.tsv [--out-dir DIR] [--label NAME]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if c.model == "" {
		fmt.Fprintln(stderr, "nabu-tts: --model (or $NABU_TTS_MODEL) is required")
		return 2
	}
	if c.samples == "" && fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	model, err := tts.Open(c.model, tts.Options{LibraryPath: c.ortLib})
	if err != nil {
		fmt.Fprintln(stderr, "nabu-tts:", err)
		return 1
	}
	defer model.Close()
	sets, err := loadRulesets(c.rulesDir)
	if err != nil {
		fmt.Fprintln(stderr, "nabu-tts:", err)
		return 1
	}

	if c.samples != "" {
		if err := renderSamples(c, model, sets, stdout); err != nil {
			fmt.Fprintln(stderr, "nabu-tts:", err)
			return 1
		}
		return 0
	}

	text := fs.Arg(0)
	ipa := text
	if !c.ipa {
		rs, ok := sets[c.ruleset]
		if !ok {
			fmt.Fprintf(stderr, "nabu-tts: unknown ruleset %q\n", c.ruleset)
			return 2
		}
		if ipa, err = rs.ToIPA(text); err != nil {
			fmt.Fprintln(stderr, "nabu-tts:", err)
			return 1
		}
	}
	if err := synthToFile(model, ipa, tts.Params{Speaker: c.speaker, Speed: c.speed}, c.out); err != nil {
		fmt.Fprintln(stderr, "nabu-tts:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\t/%s/\n", c.out, ipa)
	return 0
}

func loadRulesets(dir string) (map[string]*g2p.RuleSet, error) {
	var list []*g2p.RuleSet
	var err error
	if dir != "" {
		list, err = g2p.LoadFS(os.DirFS(dir))
	} else {
		list, err = g2p.LoadFS(rules.FS)
	}
	if err != nil {
		return nil, err
	}
	m := make(map[string]*g2p.RuleSet, len(list))
	for _, rs := range list {
		m[rs.ID] = rs
	}
	return m, nil
}

func synthToFile(model *tts.Model, ipa string, p tts.Params, path string) error {
	audio, err := model.Synthesize(context.Background(), ipa, p)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := tts.EncodeWAV(f, audio.Samples, audio.SampleRate); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

type sample struct {
	ID, Ruleset, Text string
}

func readSamples(path string) ([]sample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []sample
	sc := bufio.NewScanner(f)
	lineno := 0
	seen := map[string]bool{}
	for sc.Scan() {
		lineno++
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			return nil, fmt.Errorf("%s:%d: want 3 tab-separated fields (id, ruleset, transliteration)", path, lineno)
		}
		s := sample{ID: parts[0], Ruleset: parts[1], Text: parts[2]}
		if !safeName.MatchString(s.ID) || seen[s.ID] {
			return nil, fmt.Errorf("%s:%d: id %q must be unique and match %s", path, lineno, s.ID, safeName)
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	return out, sc.Err()
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]*$`)

func renderSamples(c config, model *tts.Model, sets map[string]*g2p.RuleSet, stdout io.Writer) error {
	samples, err := readSamples(c.samples)
	if err != nil {
		return err
	}
	info := model.Info()
	label := c.label
	if label == "" {
		label = strings.NewReplacer("@", "-step-", "/", "-").Replace(info.Name)
	}
	if !safeName.MatchString(label) {
		return fmt.Errorf("label %q must match %s", label, safeName)
	}
	dir := filepath.Join(c.outDir, label)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	speakers := []int{c.speaker}
	if c.allSpeakers {
		speakers = speakers[:0]
		for i := range info.Speakers {
			speakers = append(speakers, i)
		}
	}

	var index strings.Builder
	index.WriteString("file\tid\truleset\truleset_version\ttransliteration\tipa\tmodel\tspeaker\n")
	failed := 0
	for _, s := range samples {
		rs, ok := sets[s.Ruleset]
		if !ok {
			return fmt.Errorf("sample %s: unknown ruleset %q", s.ID, s.Ruleset)
		}
		ipa, err := rs.ToIPA(s.Text)
		for _, spk := range speakers {
			name := s.ID
			if c.allSpeakers {
				name = fmt.Sprintf("%s.spk%d", s.ID, spk)
			}
			if err == nil {
				err = synthToFile(model, ipa, tts.Params{Speaker: spk, Speed: c.speed}, filepath.Join(dir, name+".wav"))
			}
			if err != nil {
				// Record the failure next to the audio instead of skipping silently.
				failed++
				_ = os.WriteFile(filepath.Join(dir, name+".txt"), []byte(fmt.Sprintf("ERROR: %v\n", err)), 0o644)
				fmt.Fprintf(stdout, "%s\tERROR\t%v\n", name, err)
				continue
			}
			txt := fmt.Sprintf("id: %s\ntransliteration: %s\nipa: /%s/\nruleset: %s@%s\nreference: %s\nmodel: %s\nspeaker: %d (%s)\n",
				s.ID, s.Text, ipa, rs.ID, rs.Version, rs.Reference, info.Name, spk, info.Speakers[spk])
			if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(txt), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(&index, "%s.wav\t%s\t%s\t%s\t%s\t%s\t%s\t%d\n", name, s.ID, rs.ID, rs.Version, s.Text, ipa, info.Name, spk)
			fmt.Fprintf(stdout, "%s\t/%s/\n", filepath.Join(dir, name+".wav"), ipa)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "index.tsv"), []byte(index.String()), 0o644); err != nil {
		return err
	}
	if err := writeHTMLIndex(c.outDir); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s and %s\n", dir, filepath.Join(c.outDir, "index.html"))
	if failed > 0 {
		return fmt.Errorf("%d sample(s) failed; see the .txt files", failed)
	}
	return nil
}

type htmlRow struct {
	File, ID, Text, IPA, Ruleset string
	Cells                        []string // per label: relative wav path or ""
}

// writeHTMLIndex builds a side-by-side listening page over every label
// directory that has an index.tsv.
func writeHTMLIndex(outDir string) error {
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return err
	}
	var labels []string
	rows := map[string]*htmlRow{}
	var order []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(outDir, e.Name(), "index.tsv"))
		if err != nil {
			continue
		}
		labels = append(labels, e.Name())
		for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			f := strings.Split(line, "\t")
			if i == 0 || len(f) < 8 {
				continue
			}
			// The first label (in directory order) supplies the row's text and IPA.
			if _, ok := rows[f[0]]; !ok {
				rows[f[0]] = &htmlRow{File: f[0], ID: f[1], Ruleset: f[2] + "@" + f[3], Text: f[4], IPA: f[5]}
				order = append(order, f[0])
			}
		}
	}
	sort.Strings(labels)
	sort.Strings(order)
	list := make([]htmlRow, 0, len(order))
	for _, file := range order {
		r := rows[file]
		r.Cells = make([]string, len(labels))
		for i, l := range labels {
			if _, err := os.Stat(filepath.Join(outDir, l, file)); err == nil {
				r.Cells[i] = l + "/" + file
			}
		}
		list = append(list, *r)
	}
	f, err := os.Create(filepath.Join(outDir, "index.html"))
	if err != nil {
		return err
	}
	if err := indexTemplate.Execute(f, map[string]any{"Labels": labels, "Rows": list}); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

var indexTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Nabu TTS samples</title>
<style>
body{font-family:system-ui,sans-serif;margin:1.5rem}
table{border-collapse:collapse}td,th{border:1px solid #ccc;padding:.4rem .6rem;vertical-align:top}
.ipa{font-family:"Charis SIL","Doulos SIL","Noto Serif",serif;font-size:1.1rem}
.note{background:#fff4dc;border:1px solid #e5c37a;padding:.6rem 1rem;max-width:60rem}
</style></head><body>
<h1>Nabu TTS samples</h1>
<p class="note">Reconstructed pronunciations rendered by a voice trained on modern-language
recordings. Not recordings of any ancient language. Each row names the rule set
(reconstruction) that produced the IPA. Columns are checkpoints/labels.</p>
<table><thead><tr><th>sample</th><th>transliteration</th><th>IPA</th><th>rule set</th>
{{range .Labels}}<th>{{.}}</th>{{end}}</tr></thead><tbody>
{{range .Rows}}<tr><td>{{.File}}</td><td>{{.Text}}</td><td class="ipa">/{{.IPA}}/</td><td>{{.Ruleset}}</td>
{{range .Cells}}<td>{{if .}}<audio controls preload="none" src="{{.}}"></audio>{{else}}–{{end}}</td>{{end}}</tr>
{{end}}</tbody></table></body></html>
`))

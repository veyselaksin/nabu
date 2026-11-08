// Command nabu-g2p converts transliterated text to IPA using a Nabu rule file.
//
//	nabu-g2p --rules rules/egy-allen.yaml "ꜥnḫ"
//	nabu-g2p --rules rules/egy-allen.yaml --json "ꜥnḫ" "ḥtp"
//	printf 'ꜥnḫ\nḥtp\n' | nabu-g2p --rules rules/egy-allen.yaml
//
// Each positional argument (or, with none, each line of stdin) is converted
// separately and printed on its own line. With --json, each result is one JSON
// object per line. Exit status: 0 success, 1 conversion error, 2 usage error.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/veyselaksin/nabu/g2p"
)

type result struct {
	Input     string `json:"input"`
	IPA       string `json:"ipa"`
	Ruleset   string `json:"ruleset"`
	Reference string `json:"reference"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nabu-g2p", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rulesPath := fs.String("rules", "", "path to a rule file, e.g. rules/egy-allen.yaml (required)")
	asJSON := fs.Bool("json", false, "print {input, ipa, ruleset, reference} as JSON, one object per line")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: nabu-g2p --rules FILE [--json] [TEXT ...]")
		fmt.Fprintln(stderr, "Converts each TEXT (or each line of stdin) from transliteration to IPA.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *rulesPath == "" {
		fmt.Fprintln(stderr, "nabu-g2p: --rules is required")
		fs.Usage()
		return 2
	}

	rs, err := g2p.Load(*rulesPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	inputs := fs.Args()
	if len(inputs) == 0 {
		sc := bufio.NewScanner(stdin)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			inputs = append(inputs, sc.Text())
		}
		if err := sc.Err(); err != nil {
			fmt.Fprintln(stderr, "nabu-g2p: reading stdin:", err)
			return 2
		}
	}

	out := bufio.NewWriter(stdout)
	defer out.Flush()
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	status := 0
	for _, in := range inputs {
		ipa, err := rs.ToIPA(in)
		if err != nil {
			fmt.Fprintln(stderr, err)
			status = 1
			continue
		}
		if *asJSON {
			_ = enc.Encode(result{Input: in, IPA: ipa, Ruleset: rs.ID, Reference: rs.Reference})
		} else {
			fmt.Fprintln(out, ipa)
		}
	}
	return status
}

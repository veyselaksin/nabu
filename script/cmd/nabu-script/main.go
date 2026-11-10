// Command nabu-script renders Egyptological transliteration in Unicode
// hieroglyphs.
//
//	nabu-script "ꜥnḫ"
//	nabu-script --lexicon data/corpora/tla/lexicon-egy.tsv --json "ꜥnḫ" "ḏd.ꞽn =f"
//	printf 'ꜥnḫ\nḥtp\n' | nabu-script
//
// Without --lexicon every word is spelled phonetically from uniliteral signs,
// which is not how Egyptian was written. Each positional argument (or, with
// none, each line of stdin) is rendered separately and printed on its own
// line; plain output marks phonetic words with a trailing "*" (explained on
// stderr). Exit status: 0 success, 1 rendering error, 2 usage error.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/veyselaksin/nabu/script"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nabu-script", flag.ContinueOnError)
	fs.SetOutput(stderr)
	lexPath := fs.String("lexicon", "", "spelling lexicon built by `nabu-data build-lexicon` (optional)")
	asJSON := fs.Bool("json", false, "print the full rendering (words, methods, sources) as JSON, one object per line")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: nabu-script [--lexicon FILE] [--json] [TEXT ...]")
		fmt.Fprintln(stderr, "Renders each TEXT (or each line of stdin) from transliteration to hieroglyphs.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	var lex *script.Lexicon
	if *lexPath != "" {
		var err error
		if lex, err = script.LoadLexicon(*lexPath); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	r, err := script.Egyptian(lex)
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
			fmt.Fprintln(stderr, "nabu-script: reading stdin:", err)
			return 2
		}
	}

	out := bufio.NewWriter(stdout)
	defer out.Flush()
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	status, phonetic := 0, false
	for _, in := range inputs {
		res, err := r.Render(in)
		if err != nil {
			fmt.Fprintln(stderr, err)
			status = 1
			continue
		}
		if *asJSON {
			_ = enc.Encode(res)
			continue
		}
		parts := make([]string, 0, len(res.Words))
		for _, w := range res.Words {
			if w.Hieroglyphs == "" {
				continue
			}
			if w.Method == script.MethodPhonetic {
				phonetic = true
				parts = append(parts, w.Hieroglyphs+"*")
			} else {
				parts = append(parts, w.Hieroglyphs)
			}
		}
		fmt.Fprintln(out, strings.Join(parts, " "))
	}
	if err := out.Flush(); err != nil {
		fmt.Fprintln(stderr, "nabu-script:", err)
		return 1
	}
	if phonetic {
		fmt.Fprintln(stderr, "* spelled phonetically from uniliteral signs; not an attested spelling")
	}
	return status
}

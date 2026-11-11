// Command nabu-translate translates English into Middle Egyptian
// transliteration with a local language model, grounded in the TLA corpus.
//
//	nabu-translate "the king lives"
//	nabu-translate --json "bread and beer"
//	nabu-translate --search "Brot Bier"     # retrieval only, no model
//
// It needs a server implementing the OpenAI chat-completions API (for example
// llama.cpp's llama-server; see translate/README.md) and the TLA corpus
// (make data-fetch). The transliteration goes to stdout; confidence, unattested
// words and the model's notes go to stderr. Exit status: 0 success, 1
// translation error, 2 usage error.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/veyselaksin/nabu/data/source"
	"github.com/veyselaksin/nabu/translate"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nabu-translate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	llmURL := fs.String("llm-url", getenv("NABU_LLM_URL"), "OpenAI-compatible API root, e.g. http://127.0.0.1:8090/v1 (env NABU_LLM_URL)")
	model := fs.String("model", getenv("NABU_LLM_MODEL"), "model name sent to the server (env NABU_LLM_MODEL)")
	corpus := fs.String("tla", envOr(getenv, "NABU_TLA_CORPUS", "data/corpora/tla/train.jsonl"), "TLA train.jsonl (env NABU_TLA_CORPUS)")
	examples := fs.Int("examples", 8, "corpus sentences given to the model")
	timeout := fs.Duration("timeout", 5*time.Minute, "give up after this long")
	search := fs.String("search", "", "only search the corpus for these German terms and print the matches")
	asJSON := fs.Bool("json", false, "print the full result as JSON")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: nabu-translate [flags] ENGLISH TEXT")
		fmt.Fprintln(stderr, "       nabu-translate --search \"German terms\"")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	text := strings.Join(fs.Args(), " ")
	if *search == "" && (strings.TrimSpace(text) == "" || *llmURL == "") {
		if *llmURL == "" {
			fmt.Fprintln(stderr, "nabu-translate: set --llm-url or NABU_LLM_URL (see translate/README.md)")
		}
		fs.Usage()
		return 2
	}

	index, err := translate.LoadIndex(*corpus)
	if err != nil {
		fmt.Fprintln(stderr, "nabu-translate:", err)
		return 2
	}
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	if *search != "" {
		hits := index.Search(strings.Fields(*search), *examples)
		if *asJSON {
			_ = enc.Encode(hits)
			return 0
		}
		for _, h := range hits {
			fmt.Fprintf(stdout, "%.2f\t%s\n\t%s\n", h.Score, h.Transliteration, h.Translation)
		}
		return 0
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	client := &translate.Client{BaseURL: *llmURL, Model: *model, APIKey: getenv("NABU_LLM_API_KEY"), HTTP: &http.Client{}}
	res, err := translate.New(client, index, translate.Options{Examples: *examples}).Translate(ctx, text)
	if err != nil {
		fmt.Fprintln(stderr, "nabu-translate:", err)
		return 1
	}
	if *asJSON {
		_ = enc.Encode(res)
		return 0
	}
	fmt.Fprintln(stdout, res.Transliteration)
	var unattested []string
	for _, w := range res.Words {
		if !w.Attested {
			unattested = append(unattested, w.Form)
		}
	}
	fmt.Fprintf(stderr, "confidence %.2f: %d of %d words attested in the TLA corpus (vocabulary check only)\n",
		res.Confidence, len(res.Words)-len(unattested), len(res.Words))
	if len(unattested) > 0 {
		fmt.Fprintf(stderr, "not attested: %s\n", strings.Join(unattested, " "))
	}
	if res.Notes != "" {
		fmt.Fprintf(stderr, "model notes: %s\n", res.Notes)
	}
	fmt.Fprintf(stderr, "model: %s; examples: %s\n", res.Model, source.TLAAttribution)
	return 0
}

func envOr(getenv func(string) string, key, def string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return def
}

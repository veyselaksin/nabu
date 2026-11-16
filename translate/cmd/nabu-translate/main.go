// Command nabu-translate translates English into Middle Egyptian
// transliteration with a local language model, grounded in the TLA corpus.
//
//	nabu-translate "the king lives"
//	nabu-translate --json "bread and beer"
//	nabu-translate --search "Brot Bier"     # retrieval only, no model
//	nabu-translate index                    # build the vector index (RAG)
//	nabu-translate backfill-en              # machine-translate the corpus's German into English
//	nabu-translate export-sft --out DIR     # fine-tuning data with retrieval in the prompts
//	nabu-translate eval --data DIR          # chrF etc. on held-out sentences
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
	"path/filepath"
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
	if len(args) > 0 && args[0] == "index" {
		return runIndex(ctx, args[1:], getenv, stdout, stderr)
	}
	if len(args) > 0 && args[0] == "eval" {
		return runEval(ctx, args[1:], getenv, stdout, stderr)
	}
	if len(args) > 0 && args[0] == "export-sft" {
		return runExportSFT(ctx, args[1:], getenv, stdout, stderr)
	}
	if len(args) > 0 && args[0] == "backfill-en" {
		return runBackfill(ctx, args[1:], getenv, stdout, stderr)
	}
	fs := flag.NewFlagSet("nabu-translate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	llmURL := fs.String("llm-url", getenv("NABU_LLM_URL"), "OpenAI-compatible API root, e.g. http://127.0.0.1:8090/v1 (env NABU_LLM_URL)")
	model := fs.String("model", getenv("NABU_LLM_MODEL"), "model name sent to the server (env NABU_LLM_MODEL)")
	corpus := fs.String("tla", envOr(getenv, "NABU_TLA_CORPUS", filepath.Join(source.Dir(getenv, "tla"), "train.jsonl")), "TLA train.jsonl (env NABU_TLA_CORPUS; default under the nabu-data root)")
	examples := fs.Int("examples", 8, "corpus sentences given to the model")
	timeout := fs.Duration("timeout", 5*time.Minute, "give up after this long")
	search := fs.String("search", "", "only search the corpus for these German terms and print the matches")
	asJSON := fs.Bool("json", false, "print the full result as JSON")
	ragDir := fs.String("rag", envOr(getenv, "NABU_RAG_INDEX", defaultRAG(getenv)), "vector index directory; used when it exists (env NABU_RAG_INDEX)")
	embedURL := fs.String("embed-url", embedURLDefault(getenv), "embeddings API root (env NABU_EMBED_URL; default NABU_LLM_URL)")
	embedModel := fs.String("embed-model", envOr(getenv, "NABU_EMBED_MODEL", translate.DefaultEmbedModel), "embedding model (env NABU_EMBED_MODEL)")
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
	opts := translate.Options{Examples: *examples}
	if *ragDir != "" {
		if _, err := os.Stat(*ragDir); err == nil {
			vx, err := translate.LoadRAG(*ragDir, index)
			if err != nil {
				fmt.Fprintln(stderr, "nabu-translate:", err)
				return 2
			}
			if vx.Manifest.Model != *embedModel {
				fmt.Fprintf(stderr, "nabu-translate: the index was built with %s, not %s\n", vx.Manifest.Model, *embedModel)
				return 2
			}
			opts.Vectors = vx
			opts.Embedder = &translate.Client{BaseURL: *embedURL, Model: *embedModel, HTTP: &http.Client{}}
		}
	}
	res, err := translate.New(client, index, opts).Translate(ctx, text)
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
	if res.RetrievalNote != "" {
		fmt.Fprintf(stderr, "retrieval: %s (%s)\n", res.Retrieval, res.RetrievalNote)
	}
	var srcs []string
	for _, s := range res.Sources {
		srcs = append(srcs, s.ID)
	}
	fmt.Fprintf(stderr, "model: %s; retrieval: %s; sources: %s (CC BY-SA 4.0, see DATA_LICENSES.md)\n",
		res.Model, res.Retrieval, strings.Join(srcs, ", "))
	return 0
}

func defaultRAG(getenv func(string) string) string {
	return filepath.Join(source.Root(getenv), "rag", "egy")
}

func embedURLDefault(getenv func(string) string) string {
	if v := getenv("NABU_EMBED_URL"); v != "" {
		return v
	}
	if v := getenv("NABU_LLM_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:11434/v1"
}

// runEval scores translations of held-out sentences (see translate.Evaluate).
func runEval(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nabu-translate eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	data := fs.String("data", "", "directory written by export-sft (required)")
	split := fs.String("split", "test", "test or valid")
	lang := fs.String("lang", "", "only inputs in this language: English or German (default: all)")
	limit := fs.Int("limit", 200, "sentences to evaluate (0: all)")
	label := fs.String("label", "", "name of this run in the report")
	out := fs.String("out", "", "report path (default: <runs>/llm/eval-<date>-<label>.json)")
	llmURL := fs.String("llm-url", envOr(getenv, "NABU_LLM_URL", "http://127.0.0.1:11434/v1"), "OpenAI-compatible API root (env NABU_LLM_URL)")
	model := fs.String("model", envOr(getenv, "NABU_LLM_MODEL", "qwen3:4b-instruct"), "model name (env NABU_LLM_MODEL)")
	tlaPath := fs.String("tla", envOr(getenv, "NABU_TLA_CORPUS", filepath.Join(source.Dir(getenv, "tla"), "train.jsonl")), "TLA train.jsonl")
	ragDir := fs.String("rag", envOr(getenv, "NABU_RAG_INDEX", defaultRAG(getenv)), "vector index directory; \"none\" for BM25 only")
	embedURL := fs.String("embed-url", embedURLDefault(getenv), "embeddings API root (env NABU_EMBED_URL)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *data == "" {
		fmt.Fprintln(stderr, "nabu-translate eval: --data is required")
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "nabu-translate eval:", err); return 1 }
	f, err := os.Open(filepath.Join(*data, *split+"-meta.jsonl"))
	if err != nil {
		return fail(err)
	}
	metas, err := translate.ReadSFTMeta(f)
	f.Close()
	if err != nil {
		return fail(err)
	}
	index, err := translate.LoadIndex(*tlaPath)
	if err != nil {
		return fail(err)
	}
	opts := translate.Options{}
	if *ragDir != "none" {
		vx, err := translate.LoadRAG(*ragDir, index)
		if err != nil {
			return fail(err)
		}
		opts.Vectors = vx
		opts.Embedder = &translate.Client{BaseURL: *embedURL, Model: vx.Manifest.Model, HTTP: &http.Client{}}
	}
	client := &translate.Client{BaseURL: *llmURL, Model: *model, APIKey: getenv("NABU_LLM_API_KEY"), HTTP: &http.Client{Timeout: 5 * time.Minute}}
	tr := translate.New(client, index, opts)
	if *label == "" {
		*label = strings.NewReplacer("/", "-", ":", "-").Replace(*model)
		if opts.Vectors == nil {
			*label += "-bm25"
		} else {
			*label += "-hybrid"
		}
	}
	rep, err := translate.Evaluate(ctx, tr, metas, translate.EvalOptions{Label: *label, Lang: *lang, Limit: *limit, Progress: stderr})
	if err != nil {
		return fail(err)
	}
	if *out == "" {
		runs := getenv("NABU_RUNS_DIR")
		if runs == "" {
			runs = filepath.Join(filepath.Dir(source.Root(getenv)), "training-runs")
		}
		*out = filepath.Join(runs, "llm", fmt.Sprintf("eval-%s-%s.json", rep.Started.Format("20060102-1504"), *label))
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return fail(err)
	}
	raw, _ := json.MarshalIndent(rep, "", "  ")
	if err := os.WriteFile(*out, append(raw, '\n'), 0o644); err != nil {
		return fail(err)
	}
	fmt.Fprintf(stdout, "%s: n=%d chrF=%.1f exact=%.1f%% attested=%.1f%% foreign=%.1f%% errors=%d (%s retrieval)\nreport: %s\n",
		rep.Label, rep.N, rep.ChrF, 100*rep.Exact, 100*rep.Attested, 100*rep.Foreign, rep.Errors, rep.Retrieval, *out)
	return 0
}

// runExportSFT writes fine-tuning data (see translate.ExportSFT).
func runExportSFT(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nabu-translate export-sft", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := source.Root(getenv)
	corpusDir := fs.String("corpus", filepath.Join(root, "corpus", "egy"), "corpus directory (with en.jsonl from backfill-en, if any)")
	tlaPath := fs.String("tla", envOr(getenv, "NABU_TLA_CORPUS", filepath.Join(source.Dir(getenv, "tla"), "train.jsonl")), "TLA train.jsonl (BM25 index and alphabet)")
	ragDir := fs.String("rag", envOr(getenv, "NABU_RAG_INDEX", defaultRAG(getenv)), "vector index directory (required)")
	embedURL := fs.String("embed-url", embedURLDefault(getenv), "embeddings API root (env NABU_EMBED_URL)")
	out := fs.String("out", "", "output directory (required; e.g. <checkpoints>/llm/data)")
	examples := fs.Int("examples", 8, "corpus sentences per prompt (as at inference)")
	maxTrain := fs.Int("max-train", 20000, "training examples (0: all)")
	maxValid := fs.Int("max-valid", 500, "validation examples (0: all)")
	maxTest := fs.Int("max-test", 500, "test examples (0: all)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *out == "" {
		fmt.Fprintln(stderr, "nabu-translate export-sft: --out is required")
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "nabu-translate export-sft:", err); return 1 }
	sents, _, _, err := translate.ReadCorpus(*corpusDir)
	if err != nil {
		return fail(err)
	}
	english, err := translate.ReadEnglish(filepath.Join(*corpusDir, "en.jsonl"))
	if err != nil {
		return fail(err)
	}
	index, err := translate.LoadIndex(*tlaPath)
	if err != nil {
		return fail(err)
	}
	vx, err := translate.LoadRAG(*ragDir, index)
	if err != nil {
		return fail(err)
	}
	emb := &translate.Client{BaseURL: *embedURL, Model: vx.Manifest.Model, HTTP: &http.Client{Timeout: 10 * time.Minute}}
	tr := translate.New(&translate.Client{}, index, translate.Options{Examples: *examples, Vectors: vx, Embedder: emb, Model: "export"})
	fmt.Fprintf(stdout, "exporting from %d sentences (%d with English) into %s\n", len(sents), len(english), *out)
	m, err := translate.ExportSFT(ctx, tr, sents, english, *out, translate.SFTOptions{
		MaxTrain: *maxTrain, MaxValid: *maxValid, MaxTest: *maxTest, Progress: stderr,
	})
	if err != nil {
		return fail(err)
	}
	fmt.Fprintf(stdout, "examples %v, inputs %v; derived from CC BY-SA 4.0 data, keep local\n", m.Counts, m.Langs)
	return 0
}

// runBackfill writes synthetic English for the corpus sentences.
func runBackfill(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nabu-translate backfill-en", flag.ContinueOnError)
	fs.SetOutput(stderr)
	corpusDir := fs.String("corpus", filepath.Join(source.Root(getenv), "corpus", "egy"), "corpus directory written by nabu-data build-corpus")
	llmURL := fs.String("llm-url", envOr(getenv, "NABU_LLM_URL", "http://127.0.0.1:11434/v1"), "OpenAI-compatible API root (env NABU_LLM_URL)")
	model := fs.String("model", envOr(getenv, "NABU_LLM_MODEL", "qwen3:4b-instruct"), "model name (env NABU_LLM_MODEL)")
	batch := fs.Int("batch", 20, "sentences per request")
	limit := fs.Int("limit", 0, "stop after this many sentences (0: all)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	sents, _, _, err := translate.ReadCorpus(*corpusDir)
	if err != nil {
		fmt.Fprintln(stderr, "nabu-translate backfill-en:", err)
		return 2
	}
	out := filepath.Join(*corpusDir, "en.jsonl")
	fmt.Fprintf(stdout, "translating German → English with %s at %s into %s (resumable)\n", *model, *llmURL, out)
	client := &translate.Client{BaseURL: *llmURL, Model: *model, APIKey: getenv("NABU_LLM_API_KEY"), HTTP: &http.Client{Timeout: 10 * time.Minute}}
	n, err := translate.BackfillEnglish(ctx, client, translate.StageOrder(sents), out, translate.BackfillOptions{
		Model: *model, Batch: *batch, Limit: *limit, Progress: stderr,
	})
	fmt.Fprintf(stdout, "wrote %d translations (machine-translated; derived from CC BY-SA 4.0 data, keep local)\n", n)
	if err != nil {
		fmt.Fprintln(stderr, "nabu-translate backfill-en:", err, "(rerun to resume)")
		return 1
	}
	return 0
}

// runIndex builds the vector index from the merged corpus.
func runIndex(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nabu-translate index", flag.ContinueOnError)
	fs.SetOutput(stderr)
	corpusDir := fs.String("corpus", filepath.Join(source.Root(getenv), "corpus", "egy"), "corpus directory written by nabu-data build-corpus")
	out := fs.String("out", envOr(getenv, "NABU_RAG_INDEX", defaultRAG(getenv)), "index directory (env NABU_RAG_INDEX)")
	embedURL := fs.String("embed-url", embedURLDefault(getenv), "embeddings API root (env NABU_EMBED_URL; default NABU_LLM_URL)")
	embedModel := fs.String("embed-model", envOr(getenv, "NABU_EMBED_MODEL", translate.DefaultEmbedModel), "embedding model (env NABU_EMBED_MODEL)")
	dims := fs.Int("dims", translate.DefaultEmbedDims, "keep this many embedding dimensions (0: all)")
	batch := fs.Int("batch", 64, "texts per embeddings request")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	sents, lex, built, err := translate.ReadCorpus(*corpusDir)
	if err != nil {
		fmt.Fprintln(stderr, "nabu-translate index:", err)
		return 2
	}
	docs := translate.CorpusDocs(sents, lex)
	fmt.Fprintf(stdout, "embedding %d documents with %s at %s into %s\n", len(docs), *embedModel, *embedURL, *out)
	client := &translate.Client{BaseURL: *embedURL, Model: *embedModel, HTTP: &http.Client{Timeout: 10 * time.Minute}}
	err = translate.BuildVectors(ctx, client, docs, *out, translate.BuildOptions{
		Model: *embedModel, Dims: *dims, Batch: *batch, CorpusBuilt: built, Progress: stderr,
	})
	if err != nil {
		fmt.Fprintln(stderr, "\nnabu-translate index:", err, "(rerun to resume)")
		return 1
	}
	fmt.Fprintln(stdout, "done; license: CC BY-SA 4.0 (derived data, keep local)")
	return 0
}

func envOr(getenv func(string) string, key, def string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return def
}

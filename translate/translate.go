// Package translate translates English into Egyptological transliteration
// with a language model, grounded by retrieval from the TLA corpus (RAG).
//
// The corpora translate their sentences into German, so a request runs in
// these steps:
//
//  1. the model turns the English text into German search terms, and the
//     BM25 Index retrieves TLA sentences whose German translation matches;
//  2. with a VectorIndex, the English text is also embedded and matched
//     against every corpus sentence and dictionary entry (cross-lingual),
//     and the two sentence rankings are fused (reciprocal-rank fusion);
//  3. the model translates the English text, with those sentences
//     (German translation → transliteration) as examples and the
//     dictionary entries as a glossary.
//
// Every output word is then looked up in the corpus. Confidence is the share
// of output words attested there: a vocabulary check, not a measure of
// grammatical or historical correctness.
package translate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/veyselaksin/nabu/data/source"
	"github.com/veyselaksin/nabu/data/tla"
	"github.com/veyselaksin/nabu/script"
)

// Errors returned by Translate.
var (
	ErrEmptyInput   = errors.New("translate: nothing to translate")
	ErrInvalidUTF8  = errors.New("translate: text is not valid UTF-8")
	ErrModelOutput  = errors.New("translate: the language model returned no usable transliteration")
	errNoTranslator = errors.New("translate: nil Translator")
)

// Options configures a Translator.
type Options struct {
	// Examples is how many corpus sentences go into the prompt (default 8).
	Examples int
	// Model names the language model in results (default: the client's
	// Model, else "unknown").
	Model string
	// Vectors and Embedder enable vector retrieval; without them retrieval
	// is BM25 only.
	Vectors  *VectorIndex
	Embedder Embedder
	// Glossary is how many dictionary entries go into the prompt (default 10).
	Glossary int
	// Exclude, if set, drops matching documents from retrieval (evaluation
	// uses it to hide a test sentence's own text).
	Exclude func(*Doc) bool
}

// Translator translates English into Middle Egyptian transliteration.
type Translator struct {
	llm   Chatter
	index *Index
	opts  Options
}

// New returns a Translator using llm for generation and index for retrieval
// and the vocabulary check.
func New(llm Chatter, index *Index, opts Options) *Translator {
	if opts.Examples <= 0 {
		opts.Examples = 8
	}
	if opts.Glossary <= 0 {
		opts.Glossary = 10
	}
	if opts.Model == "" {
		if c, ok := llm.(*Client); ok && c.Model != "" {
			opts.Model = c.Model
		} else {
			opts.Model = "unknown"
		}
	}
	return &Translator{llm: llm, index: index, opts: opts}
}

// Index returns the retrieval index.
func (t *Translator) Index() *Index { return t.index }

// Model returns the model name reported in results.
func (t *Translator) Model() string { return t.opts.Model }

// Word is one output word and its attestation in the corpus.
type Word struct {
	Form     string `json:"form"`
	Attested bool   `json:"attested"`
	// Count is how often the form occurs in the corpus.
	Count int `json:"count"`
	// Foreign lists graphemes that the corpus never writes (e.g. a bare
	// "i" where the TLA writes "ꞽ" or "i̯"). The G2P and script stages
	// reject such words.
	Foreign []string `json:"foreign,omitempty"`
}

// Result is a translation and the evidence behind it.
type Result struct {
	Input           string `json:"input"`
	Transliteration string `json:"transliteration"`
	Words           []Word `json:"words"`
	// Confidence is the share of Words attested in the corpus, in [0, 1].
	Confidence float64 `json:"confidence"`
	// Notes is the model's own comment on uncertain choices (may be empty).
	Notes string `json:"notes,omitempty"`
	// Corrected reports that the model's first answer used graphemes
	// outside the corpus alphabet and was asked to correct them.
	Corrected bool `json:"corrected,omitempty"`
	// Keywords are the German search terms used for retrieval.
	Keywords []string `json:"keywords"`
	// Examples are the corpus sentences given to the model.
	Examples []Hit `json:"examples"`
	// Glossary holds the dictionary entries given to the model.
	Glossary []Gloss `json:"glossary"`
	// Retrieval is "hybrid" (BM25 + vectors) or "bm25"; RetrievalNote says
	// why vector search was not used when it is configured.
	Retrieval     string          `json:"retrieval"`
	RetrievalNote string          `json:"retrieval_note,omitempty"`
	Model         string          `json:"model"`
	Sources       []script.Source `json:"sources"`
}

// Gloss is a dictionary entry given to the model.
type Gloss struct {
	ID      string  `json:"id"`
	Source  string  `json:"source"`
	Lemma   string  `json:"lemma"`
	English string  `json:"en,omitempty"`
	German  string  `json:"de,omitempty"`
	POS     string  `json:"pos,omitempty"`
	Score   float64 `json:"score"`
}

// TLASource describes the retrieval corpus for attribution.
func TLASource() script.Source { return sourceInfo("tla") }

func sourceInfo(key string) script.Source {
	s, ok := source.Get(key)
	if !ok {
		return script.Source{ID: key}
	}
	return script.Source{ID: s.Key, Name: s.Name, URL: s.Homepage, License: s.License, Attribution: s.Attribution}
}

const keywordPrompt = `You help search a corpus of Ancient Egyptian texts whose translations are in German.
Given English text, reply with a JSON object {"keywords": [...]} listing up to 8 German words that a
German translation of the text would contain: nouns, verbs in the infinitive and adjectives, in their
base form, without articles. Reply with the JSON object only.`

const translatePrompt = `You translate English into Middle Egyptian (Earlier Egyptian). Write the result in
the transliteration conventions of the Thesaurus Linguae Aegyptiae, exactly as in the example sentences:
words separated by single spaces, suffix pronouns as separate words beginning with "=", no hieroglyphs,
no punctuation, no brackets. Prefer words and constructions that occur in the examples.
Reply with a JSON object {"transliteration": "...", "notes": "..."}, where notes is one short English
sentence about uncertain choices or words that have no Egyptian equivalent.`

// alphabetPrompt lists the corpus alphabet (Index.Alphabet).
const alphabetPrompt = `
Write only with these letters and marks, as the corpus does (yod is "ꞽ", never "i", "j" or "y" for it): `

// correctionPrompt asks the model to rewrite words with foreign graphemes.
const correctionPrompt = `These words use letters that the Thesaurus Linguae Aegyptiae never writes: %s.
Rewrite them using only the allowed letters and marks, and reply with the corrected JSON object
{"transliteration": "...", "notes": "..."}.`

// Translate translates English text.
func (t *Translator) Translate(ctx context.Context, text string) (*Result, error) {
	return t.TranslateFrom(ctx, "English", text, t.opts.Exclude)
}

// TranslateFrom translates text written in lang ("English" or "German"),
// with exclude (may be nil) hiding documents from retrieval. Evaluation uses
// it to translate held-out sentences without retrieving their own text.
func (t *Translator) TranslateFrom(ctx context.Context, lang, text string, exclude func(*Doc) bool) (*Result, error) {
	if t == nil {
		return nil, errNoTranslator
	}
	if !utf8.ValidString(text) {
		return nil, ErrInvalidUTF8
	}
	text = strings.TrimSpace(norm.NFC.String(text))
	if text == "" {
		return nil, ErrEmptyInput
	}

	keywords, err := t.keywords(ctx, text)
	if err != nil {
		return nil, err
	}
	hits, glossary, retrieval, note := t.retrieve(ctx, text, keywords, exclude)

	messages := t.Prompt(lang, text, hits, glossary)
	ans, err := t.ask(ctx, messages)
	if err != nil {
		return nil, err
	}

	// One correction round when the answer uses graphemes the corpus never
	// writes: those words would fail in the G2P and script stages. The
	// corrected answer is kept only if it is better.
	corrected := false
	if bad := t.foreignWords(ans.translit); len(bad) > 0 {
		retry := append(messages,
			Message{Role: "assistant", Content: ans.reply},
			Message{Role: "user", Content: fmt.Sprintf(correctionPrompt, strings.Join(bad, ", "))},
		)
		again, err := t.ask(ctx, retry)
		switch {
		case err == nil && len(t.foreignWords(again.translit)) < len(bad):
			ans, corrected = again, true
		case err != nil && !errors.Is(err, ErrModelOutput):
			return nil, err
		}
	}
	return t.result(text, ans, corrected, keywords, hits, glossary, retrieval, note), nil
}

// Prompt builds the translation request for text in the source language
// lang ("English"; fine-tuning data also uses "German"): system prompt with
// the corpus alphabet, then glossary, examples and the text. Inference and
// the fine-tuning export share it, so their prompts cannot drift apart.
func (t *Translator) Prompt(lang, text string, hits []Hit, glossary []Gloss) []Message {
	var user strings.Builder
	if len(glossary) > 0 {
		user.WriteString("Dictionary entries (lemma: English / German meaning):\n")
		for _, g := range glossary {
			meaning := g.English
			if g.German != "" {
				meaning = strings.TrimPrefix(meaning+" / "+g.German, " / ")
			}
			fmt.Fprintf(&user, "- %s: %s\n", g.Lemma, oneLine(meaning))
		}
		user.WriteString("\n")
	}
	if len(hits) > 0 {
		user.WriteString("Example sentences from Egyptian text corpora (German translation → transliteration):\n")
		for i, h := range hits {
			fmt.Fprintf(&user, "%d. %s → %s\n", i+1, oneLine(h.Translation), promptForm(h.Transliteration))
		}
		user.WriteString("\n")
	}
	fmt.Fprintf(&user, "%s: %s", lang, text)

	system := translatePrompt
	if alpha := t.index.Alphabet(); len(alpha) > 0 {
		system += alphabetPrompt + strings.Join(alpha, " ")
	}
	return []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user.String()},
	}
}

func (t *Translator) result(text string, ans answer, corrected bool, keywords []string, hits []Hit, glossary []Gloss, retrieval, note string) *Result {
	translit := ans.translit

	res := &Result{
		Input:           text,
		Transliteration: translit,
		Notes:           ans.notes,
		Corrected:       corrected,
		Keywords:        keywords,
		Examples:        hits,
		Glossary:        glossary,
		Retrieval:       retrieval,
		RetrievalNote:   note,
		Model:           t.opts.Model,
		Sources:         usedSources(hits, glossary),
	}
	if res.Examples == nil {
		res.Examples = []Hit{}
	}
	if res.Glossary == nil {
		res.Glossary = []Gloss{}
	}
	attested := 0
	for _, w := range strings.Fields(translit) {
		n := t.index.FormCount(w)
		res.Words = append(res.Words, Word{Form: w, Attested: n > 0, Count: n, Foreign: t.index.Foreign(w)})
		if n > 0 {
			attested++
		}
	}
	res.Confidence = float64(attested) / float64(len(res.Words))
	return res
}

// rrfK is the rank offset of reciprocal-rank fusion (the usual 60).
const rrfK = 60

// retrieve finds the examples and glossary for text: BM25 over the German
// keywords and, when configured, vector search over the English text.
// Vector search is an aid: if embedding fails, retrieval falls back to BM25
// and says why.
func (t *Translator) retrieve(ctx context.Context, text string, keywords []string, exclude func(*Doc) bool) ([]Hit, []Gloss, string, string) {
	if t.opts.Vectors == nil || t.opts.Embedder == nil {
		return t.bm25(keywords, exclude, t.opts.Examples), nil, "bm25", ""
	}
	vecs, err := t.opts.Embedder.Embed(ctx, []string{QueryText(text)})
	if err != nil || len(vecs) == 0 {
		return t.bm25(keywords, exclude, t.opts.Examples), nil, "bm25", fmt.Sprintf("vector search unavailable: %v", err)
	}
	// Fusion gets a longer BM25 list to rank against the vector hits.
	hits, glossary := t.retrieveVector(vecs[0], t.bm25(keywords, exclude, 2*t.opts.Examples), exclude)
	return hits, glossary, "hybrid", ""
}

// bm25 returns up to k TLA sentences ranked by German keywords (Index.Search);
// exclude sees each hit as a Doc (transliteration and translation only).
func (t *Translator) bm25(keywords []string, exclude func(*Doc) bool, k int) []Hit {
	if exclude == nil {
		return t.index.Search(keywords, k)
	}
	var out []Hit
	for _, h := range t.index.Search(keywords, k+8) {
		if exclude != nil && exclude(&Doc{Kind: KindSentence, Source: h.Source, Translit: h.Transliteration, German: h.Translation}) {
			continue
		}
		if out = append(out, h); len(out) == k {
			break
		}
	}
	return out
}

// retrieveVector searches the vector index with an embedded query and fuses
// the sentences with the BM25 ranking.
func (t *Translator) retrieveVector(q []float32, bm25 []Hit, exclude func(*Doc) bool) ([]Hit, []Gloss) {
	k := t.opts.Examples
	sentences := t.opts.Vectors.Search(q, 2*k, func(d *Doc) bool {
		return d.Kind == KindSentence && t.stageOK(d.Stage) && (exclude == nil || !exclude(d))
	})
	entries := t.opts.Vectors.Search(q, t.opts.Glossary, func(d *Doc) bool {
		return d.Kind == KindLexicon && d.Stage != "coptic" && (exclude == nil || !exclude(d))
	})
	var glossary []Gloss
	for _, e := range entries {
		glossary = append(glossary, Gloss{ID: e.ID, Source: e.Source, Lemma: e.Translit, English: e.English,
			German: e.German, POS: e.POS, Score: e.Score})
	}
	return fuse(bm25, sentences, k), glossary
}

// stageOK keeps sentences that can serve as Middle Egyptian examples: the
// earlier stage, and texts whose stage the source does not settle.
func (t *Translator) stageOK(stage string) bool { return stage == "earlier" || stage == "" }

// fuse merges the BM25 and vector rankings by reciprocal-rank fusion and
// returns the k best; the same sentence from both lists counts once.
func fuse(bm25 []Hit, vec []ScoredDoc, k int) []Hit {
	type entry struct {
		hit   Hit
		score float64
		order int
	}
	byKey := map[string]*entry{}
	var list []*entry
	add := func(h Hit, rank int) {
		key := promptForm(h.Transliteration) + "|" + oneLine(h.Translation)
		e := byKey[key]
		if e == nil {
			e = &entry{hit: h, order: len(list)}
			byKey[key] = e
			list = append(list, e)
		}
		e.score += 1 / float64(rrfK+rank+1)
	}
	for i, h := range bm25 {
		add(h, i)
	}
	for i, d := range vec {
		add(Hit{Example: Example{ID: d.ID, Source: d.Source, Stage: d.Stage, Transliteration: d.Translit,
			Translation: d.German, Date: d.Date}}, i)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].score > list[j].score })
	out := make([]Hit, 0, min(k, len(list)))
	for _, e := range list[:min(k, len(list))] {
		h := e.hit
		h.Score = e.score
		out = append(out, h)
	}
	return out
}

// usedSources lists the attribution of every source the result drew on,
// TLA first.
func usedSources(hits []Hit, glossary []Gloss) []script.Source {
	keys := []string{"tla"}
	seen := map[string]bool{"tla": true}
	add := func(k string) {
		if k != "" && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for _, h := range hits {
		add(h.Source)
	}
	for _, g := range glossary {
		add(g.Source)
	}
	out := make([]script.Source, len(keys))
	for i, k := range keys {
		out[i] = sourceInfo(k)
	}
	return out
}

type answer struct {
	reply, translit, notes string
}

// ask runs one translation request and parses the reply.
func (t *Translator) ask(ctx context.Context, messages []Message) (answer, error) {
	reply, err := t.llm.Chat(ctx, messages, ChatOptions{JSON: true, MaxTokens: 400})
	if err != nil {
		return answer{}, err
	}
	var out struct {
		Transliteration string `json:"transliteration"`
		Notes           string `json:"notes"`
	}
	if err := decodeObject(reply, &out); err != nil {
		return answer{}, fmt.Errorf("%w: %v", ErrModelOutput, err)
	}
	translit := cleanTransliteration(out.Transliteration)
	if translit == "" {
		return answer{}, ErrModelOutput
	}
	return answer{reply: reply, translit: translit, notes: strings.TrimSpace(out.Notes)}, nil
}

// foreignWords describes the words of translit that use graphemes outside
// the corpus alphabet, e.g. `ḥi ("i")`.
func (t *Translator) foreignWords(translit string) []string {
	var out []string
	for _, w := range strings.Fields(translit) {
		if f := t.index.Foreign(w); len(f) > 0 {
			out = append(out, fmt.Sprintf(`%s ("%s")`, w, strings.Join(f, `", "`)))
		}
	}
	return out
}

// keywords asks the model for German search terms. Retrieval is an aid, so
// an unparsable reply yields no keywords rather than an error; a failed
// request is still an error.
func (t *Translator) keywords(ctx context.Context, text string) ([]string, error) {
	reply, err := t.llm.Chat(ctx, []Message{
		{Role: "system", Content: keywordPrompt},
		{Role: "user", Content: text},
	}, ChatOptions{JSON: true, MaxTokens: 100})
	if err != nil {
		return nil, err
	}
	var out struct {
		Keywords []string `json:"keywords"`
	}
	if decodeObject(reply, &out) != nil {
		return []string{}, nil
	}
	keywords := []string{}
	seen := map[string]bool{}
	for _, k := range out.Keywords {
		k = strings.TrimSpace(norm.NFC.String(k))
		if k != "" && !seen[strings.ToLower(k)] && len(keywords) < 8 {
			seen[strings.ToLower(k)] = true
			keywords = append(keywords, k)
		}
	}
	return keywords, nil
}

// cleanTransliteration normalizes a model's transliteration: NFC, single
// spaces, and no sentence punctuation (which the TLA does not use and the
// script and G2P stages reject). Periods are kept: they separate morphemes.
func cleanTransliteration(s string) string {
	s = norm.NFC.String(s)
	s = strings.Map(func(r rune) rune {
		switch r {
		case ',', ';', ':', '!', '?', '"', '“', '”', '(', ')', '[', ']', '⸢', '⸣', '〈', '〉':
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// promptForm shows a corpus transliteration the way the model should write:
// editor's brackets removed (tla.NormalizeForm), one line.
func promptForm(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = tla.NormalizeForm(w)
	}
	return strings.Join(words, " ")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

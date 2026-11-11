// Package translate translates English into Egyptological transliteration
// with a language model, grounded by retrieval from the TLA corpus (RAG).
//
// The TLA translates its sentences into German, so a request runs in three
// steps:
//
//  1. the model turns the English text into German search terms;
//  2. the Index retrieves TLA sentences whose German translation matches;
//  3. the model translates the English text, with those sentences
//     (German translation → transliteration) as examples.
//
// Every output word is then looked up in the corpus. Confidence is the share
// of output words attested there: a vocabulary check, not a measure of
// grammatical or historical correctness.
package translate

import (
	"context"
	"errors"
	"fmt"
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
	// Keywords are the German search terms used for retrieval.
	Keywords []string `json:"keywords"`
	// Examples are the corpus sentences given to the model.
	Examples []Hit           `json:"examples"`
	Model    string          `json:"model"`
	Sources  []script.Source `json:"sources"`
}

// TLASource describes the retrieval corpus for attribution.
func TLASource() script.Source {
	s, _ := source.Get("tla")
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

// Translate translates English text.
func (t *Translator) Translate(ctx context.Context, text string) (*Result, error) {
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
	hits := t.index.Search(keywords, t.opts.Examples)

	var user strings.Builder
	if len(hits) > 0 {
		user.WriteString("Example sentences from the TLA corpus (German translation → transliteration):\n")
		for i, h := range hits {
			fmt.Fprintf(&user, "%d. %s → %s\n", i+1, oneLine(h.Translation), promptForm(h.Transliteration))
		}
		user.WriteString("\n")
	}
	fmt.Fprintf(&user, "English: %s", text)

	reply, err := t.llm.Chat(ctx, []Message{
		{Role: "system", Content: translatePrompt},
		{Role: "user", Content: user.String()},
	}, ChatOptions{JSON: true, MaxTokens: 400})
	if err != nil {
		return nil, err
	}
	var out struct {
		Transliteration string `json:"transliteration"`
		Notes           string `json:"notes"`
	}
	if err := decodeObject(reply, &out); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelOutput, err)
	}
	translit := cleanTransliteration(out.Transliteration)
	if translit == "" {
		return nil, ErrModelOutput
	}

	res := &Result{
		Input:           text,
		Transliteration: translit,
		Notes:           strings.TrimSpace(out.Notes),
		Keywords:        keywords,
		Examples:        hits,
		Model:           t.opts.Model,
		Sources:         []script.Source{TLASource()},
	}
	if res.Examples == nil {
		res.Examples = []Hit{}
	}
	attested := 0
	for _, w := range strings.Fields(translit) {
		n := t.index.FormCount(w)
		res.Words = append(res.Words, Word{Form: w, Attested: n > 0, Count: n})
		if n > 0 {
			attested++
		}
	}
	res.Confidence = float64(attested) / float64(len(res.Words))
	return res, nil
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

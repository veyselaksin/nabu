package translate

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/veyselaksin/nabu/data/tla"
)

func fixtureIndex(t *testing.T) *Index {
	t.Helper()
	f, err := os.Open("testdata/tla-fixture.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := tla.Read(f)
	if err != nil {
		t.Fatal(err)
	}
	return NewIndex(s)
}

func translits(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Transliteration
	}
	return out
}

func TestTokenize(t *testing.T) {
	got := tokenize("Der König, des $šnꜥ$-Hauses! a 12")
	want := []string{"der", "könig", "des", "šnꜥ", "hauses", "12"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokenize = %q, want %q", got, want)
	}
}

func TestIndexBuild(t *testing.T) {
	ix := fixtureIndex(t)
	// 8 lines: one duplicate, one without translation, one too long.
	if ix.Len() != 5 {
		t.Errorf("Len = %d, want 5", ix.Len())
	}
	for form, want := range map[string]int{"ꜥnḫ": 4, "=f": 2, "ḥnq.t": 1, "ḥnq(.t)": 1, "nzw": 2, "missing": 0} {
		if got := ix.FormCount(form); got != want {
			t.Errorf("FormCount(%q) = %d, want %d", form, got, want)
		}
	}
}

func TestSearch(t *testing.T) {
	ix := fixtureIndex(t)
	tests := []struct {
		name  string
		terms []string
		k     int
		want  []string
	}{
		{"prefix match", []string{"Brot"}, 5, []string{"tʾ", "tʾ ḥnq(.t)"}},
		{"short term matches whole words only", []string{"er"}, 5, []string{"ꜥnḫ =f"}},
		{"each term gets its best example", []string{"Brot", "König"}, 2, []string{"tʾ", "nzw"}},
		{"case and phrase", []string{"BIER und"}, 5, []string{"tʾ ḥnq(.t)"}},
		{"no match", []string{"Flugzeug"}, 5, []string{}},
		{"k = 0", []string{"Brot"}, 0, []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := translits(ix.Search(tc.terms, tc.k)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Search(%q, %d) = %q, want %q", tc.terms, tc.k, got, tc.want)
			}
		})
	}
	if hits := ix.Search([]string{"Leben"}, 1); len(hits) != 1 || hits[0].DateNotBefore != "-2000" || hits[0].Score <= 0 {
		t.Errorf("Search(Leben) = %+v, want the dated example with a positive score", hits)
	}
}

// fakeLLM answers the keyword and translation prompts with canned replies,
// and a correction request with correction (default: translation again).
type fakeLLM struct {
	keywords, translation, correction string
	err                               error
	calls                             [][]Message
}

func (f *fakeLLM) Chat(_ context.Context, m []Message, opts ChatOptions) (string, error) {
	f.calls = append(f.calls, m)
	if f.err != nil {
		return "", f.err
	}
	if !opts.JSON {
		return "", errors.New("expected JSON mode")
	}
	if strings.Contains(m[0].Content, `"keywords"`) {
		return f.keywords, nil
	}
	if len(m) > 2 && f.correction != "" {
		return f.correction, nil
	}
	return f.translation, nil
}

func TestTranslate(t *testing.T) {
	llm := &fakeLLM{
		keywords: "```json\n{\"keywords\": [\"Brot\", \"brot\", \" König \", \"\"]}\n```",
		translation: "<think>{\"transliteration\": \"no\"}</think>Sure: " +
			`{"transliteration": "tʾ,  nzw  xyz!", "notes": "xyz is a guess"}`,
	}
	tr := New(llm, fixtureIndex(t), Options{Examples: 3, Model: "fake"})
	res, err := tr.Translate(context.Background(), "  bread of the king ")
	if err != nil {
		t.Fatal(err)
	}
	if res.Input != "bread of the king" || res.Transliteration != "tʾ nzw xyz" || res.Notes != "xyz is a guess" || res.Model != "fake" {
		t.Errorf("result = %+v", res)
	}
	if !reflect.DeepEqual(res.Keywords, []string{"Brot", "König"}) {
		t.Errorf("Keywords = %q", res.Keywords)
	}
	wantWords := []Word{{Form: "tʾ", Attested: true, Count: 2}, {Form: "nzw", Attested: true, Count: 2}, {Form: "xyz"}}
	if !reflect.DeepEqual(res.Words, wantWords) {
		t.Errorf("Words = %+v, want %+v", res.Words, wantWords)
	}
	if res.Confidence != 2.0/3.0 {
		t.Errorf("Confidence = %v", res.Confidence)
	}
	if got := translits(res.Examples); !reflect.DeepEqual(got, []string{"tʾ", "nzw", "tʾ ḥnq(.t)"}) {
		t.Errorf("Examples = %q", got)
	}
	if len(res.Sources) != 1 || res.Sources[0].License != "CC-BY-SA-4.0" || res.Sources[0].Attribution == "" {
		t.Errorf("Sources = %+v", res.Sources)
	}

	if len(llm.calls) != 2 {
		t.Fatalf("%d model calls, want 2", len(llm.calls))
	}
	prompt := llm.calls[1][1].Content
	for _, want := range []string{"Brot und Bier → tʾ ḥnq.t\n", "English: bread of the king"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("translation prompt lacks %q:\n%s", want, prompt)
		}
	}
}

func TestGraphemes(t *testing.T) {
	got := graphemes("di̯ =ꞽ")
	want := []string{"d", "i̯", " ", "=", "ꞽ"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("graphemes = %q, want %q", got, want)
	}
}

func TestAlphabet(t *testing.T) {
	// 40,009 graphemes: the threshold is 2 occurrences, so the single bare
	// "i" is foreign while "i̯" (twice) belongs to the alphabet.
	forms := map[string]int{"ꞽ": 40000, "di̯": 2, "=f": 2, "i": 1}
	got := graphemeAlphabet(forms)
	want := []string{"ꞽ", "=", "d", "f", "i̯"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("alphabet = %q, want %q", got, want)
	}

	ix := fixtureIndex(t)
	for form, want := range map[string][]string{
		"ꜥnḫ": nil, "ḥnq(.t)": nil, "=f": nil, "ȝḫ": {"ȝ"}, "ʿʿnḫȝ": {"ʿ", "ȝ"},
	} {
		if got := ix.Foreign(form); !reflect.DeepEqual(got, want) {
			t.Errorf("Foreign(%q) = %q, want %q", form, got, want)
		}
	}
}

func TestTranslateCorrection(t *testing.T) {
	ix := fixtureIndex(t)
	t.Run("corrected", func(t *testing.T) {
		llm := &fakeLLM{
			keywords:    "{}",
			translation: `{"transliteration": "ʿnḫ ȝḫ"}`,
			correction:  `{"transliteration": "ꜥnḫ ȝḫ", "notes": "fixed"}`,
		}
		res, err := New(llm, ix, Options{}).Translate(context.Background(), "life")
		if err != nil {
			t.Fatal(err)
		}
		if res.Transliteration != "ꜥnḫ ȝḫ" || !res.Corrected || res.Notes != "fixed" {
			t.Errorf("result = %+v", res)
		}
		want := []Word{{Form: "ꜥnḫ", Attested: true, Count: 4}, {Form: "ȝḫ", Foreign: []string{"ȝ"}}}
		if !reflect.DeepEqual(res.Words, want) {
			t.Errorf("Words = %+v, want %+v", res.Words, want)
		}
		if len(llm.calls) != 3 {
			t.Fatalf("%d model calls, want 3", len(llm.calls))
		}
		if !strings.Contains(llm.calls[1][0].Content, "ꞽ") {
			t.Error("system prompt lacks the alphabet")
		}
		retry := llm.calls[2]
		if len(retry) != 4 || retry[2].Role != "assistant" || !strings.Contains(retry[3].Content, `ʿnḫ ("ʿ"), ȝḫ ("ȝ")`) {
			t.Errorf("correction request = %+v", retry)
		}
	})
	t.Run("no better", func(t *testing.T) {
		llm := &fakeLLM{keywords: "{}", translation: `{"transliteration": "ȝḫ"}`, correction: "not json"}
		res, err := New(llm, ix, Options{}).Translate(context.Background(), "spirit")
		if err != nil {
			t.Fatal(err)
		}
		if res.Transliteration != "ȝḫ" || res.Corrected || len(res.Words[0].Foreign) != 1 {
			t.Errorf("result = %+v", res)
		}
	})
	t.Run("valid needs no correction", func(t *testing.T) {
		llm := &fakeLLM{keywords: "{}", translation: `{"transliteration": "ꜥnḫ"}`}
		if _, err := New(llm, ix, Options{}).Translate(context.Background(), "life"); err != nil {
			t.Fatal(err)
		}
		if len(llm.calls) != 2 {
			t.Errorf("%d model calls, want 2", len(llm.calls))
		}
	})
}

func TestTranslateUnparsableKeywords(t *testing.T) {
	llm := &fakeLLM{keywords: "no idea", translation: `{"transliteration": "ꜥnḫ"}`}
	res, err := New(llm, fixtureIndex(t), Options{}).Translate(context.Background(), "life")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Keywords) != 0 || len(res.Examples) != 0 || res.Confidence != 1 || res.Model != "unknown" {
		t.Errorf("result = %+v", res)
	}
	if strings.Contains(llm.calls[1][1].Content, "Example sentences") {
		t.Error("prompt has an examples header without examples")
	}
}

func TestTranslateErrors(t *testing.T) {
	ix := fixtureIndex(t)
	boom := &LLMError{Status: 500, Msg: "boom"}
	tests := []struct {
		name string
		llm  *fakeLLM
		text string
		want error
	}{
		{"empty", &fakeLLM{}, "  ", ErrEmptyInput},
		{"bad utf8", &fakeLLM{}, "\xff", ErrInvalidUTF8},
		{"backend", &fakeLLM{err: boom}, "life", boom},
		{"not json", &fakeLLM{keywords: "{}", translation: "ꜥnḫ"}, "life", ErrModelOutput},
		{"empty transliteration", &fakeLLM{keywords: "{}", translation: `{"transliteration": " ?! "}`}, "life", ErrModelOutput},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.llm, ix, Options{}).Translate(context.Background(), tc.text)
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestClient(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"data": []}`)
		case r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost:
			if r.Header.Get("Authorization") != "Bearer k" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_ = json.NewDecoder(r.Body).Decode(&got)
			_, _ = io.WriteString(w, `{"choices": [{"message": {"role": "assistant", "content": "hi"}}]}`)
		default:
			http.Error(w, "nope", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/v1/", Model: "m", APIKey: "k"}
	reply, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "x"}}, ChatOptions{JSON: true, MaxTokens: 7})
	if err != nil || reply != "hi" {
		t.Fatalf("Chat = %q, %v", reply, err)
	}
	if got["model"] != "m" || got["max_tokens"] != float64(7) || got["temperature"] != float64(0) || got["stream"] != false {
		t.Errorf("request = %v", got)
	}
	if rf, _ := got["response_format"].(map[string]any); rf["type"] != "json_object" {
		t.Errorf("response_format = %v", got["response_format"])
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}

	c.APIKey = ""
	_, err = c.Chat(context.Background(), nil, ChatOptions{})
	var lerr *LLMError
	if !errors.As(err, &lerr) || lerr.Status != http.StatusUnauthorized || !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("err = %v, want HTTP 401 LLMError", err)
	}

	c.BaseURL = srv.URL + "/other"
	if err := c.Ping(context.Background()); !errors.As(err, &lerr) || lerr.Status != http.StatusNotFound {
		t.Errorf("Ping err = %v, want HTTP 404", err)
	}
	srv.Close()
	if err := c.Ping(context.Background()); !errors.As(err, &lerr) || lerr.Status != 0 {
		t.Errorf("Ping err = %v, want a connection error", err)
	}
}

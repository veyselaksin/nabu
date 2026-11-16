package translate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testVectors(t *testing.T) *VectorIndex {
	t.Helper()
	dir := t.TempDir()
	docs := append(testDocs(), Doc{ID: "c1", Kind: KindLexicon, Source: "coptic-dict", Stage: "coptic",
		Translit: "ⲟⲉⲓⲕ", English: "bread", German: "Brot"})
	if err := BuildVectors(context.Background(), &fakeEmbedder{}, docs, dir, BuildOptions{Model: "fake", Dims: 4}); err != nil {
		t.Fatal(err)
	}
	vx, err := LoadVectors(dir)
	if err != nil {
		t.Fatal(err)
	}
	return vx
}

func TestTranslateHybrid(t *testing.T) {
	llm := &fakeLLM{
		keywords:    `{"keywords": ["Brot"]}`,
		translation: `{"transliteration": "tꞽ ḥnq.t", "notes": ""}`,
	}
	vx := testVectors(t)
	tr := New(llm, fixtureIndex(t), Options{Examples: 3, Glossary: 2, Model: "fake", Vectors: vx,
		Embedder: &queryEmbedder{v: []float32{1, 1, 0, 0}}})
	res, err := tr.Translate(context.Background(), "bread and beer")
	if err != nil {
		t.Fatal(err)
	}
	if res.Retrieval != "hybrid" || res.RetrievalNote != "" {
		t.Errorf("retrieval %q %q", res.Retrieval, res.RetrievalNote)
	}
	// The Late Egyptian sentence (s3) and the Coptic entry are filtered out.
	var ids []string
	for _, h := range res.Examples {
		if h.Stage == "late" {
			t.Errorf("late example %+v", h)
		}
		ids = append(ids, h.ID)
	}
	if !strings.Contains(strings.Join(ids, ","), "s1") {
		t.Errorf("examples %v lack s1", ids)
	}
	if len(res.Glossary) != 2 || res.Glossary[0].ID != "l2" || res.Glossary[0].English != "beer" {
		t.Errorf("glossary = %+v", res.Glossary)
	}
	prompt := llm.calls[1][1].Content
	if !strings.Contains(prompt, "- ḥnq.t: beer / Bier") || strings.Contains(prompt, "ⲟⲉⲓⲕ") {
		t.Errorf("prompt:\n%s", prompt)
	}
	var srcs []string
	for _, s := range res.Sources {
		srcs = append(srcs, s.ID)
	}
	if got := strings.Join(srcs, ","); !strings.HasPrefix(got, "tla,") || !strings.Contains(got, "aed") {
		t.Errorf("sources = %s", got)
	}

	// Exclude hides documents (evaluation).
	tr.opts.Exclude = func(d *Doc) bool { return d.ID == "s1" || d.ID == "l2" }
	res, _ = tr.Translate(context.Background(), "bread and beer")
	for _, h := range res.Examples {
		if h.ID == "s1" {
			t.Error("excluded s1 retrieved")
		}
	}
	if len(res.Glossary) > 0 && res.Glossary[0].ID == "l2" {
		t.Error("excluded l2 retrieved")
	}
}

type queryEmbedder struct {
	v   []float32
	err error
}

func (q *queryEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if q.err != nil {
		return nil, q.err
	}
	if len(texts) != 1 || !strings.HasPrefix(texts[0], "Instruct: ") {
		return nil, errors.New("want one instructed query")
	}
	return [][]float32{q.v}, nil
}

func TestTranslateVectorFallback(t *testing.T) {
	llm := &fakeLLM{keywords: `{"keywords": ["Brot"]}`, translation: `{"transliteration": "tꞽ"}`}
	tr := New(llm, fixtureIndex(t), Options{Vectors: testVectors(t), Embedder: &queryEmbedder{err: errors.New("model not found")}})
	res, err := tr.Translate(context.Background(), "bread")
	if err != nil {
		t.Fatal(err)
	}
	if res.Retrieval != "bm25" || !strings.Contains(res.RetrievalNote, "model not found") || len(res.Examples) == 0 {
		t.Errorf("fallback: %q %q %d", res.Retrieval, res.RetrievalNote, len(res.Examples))
	}
}

func TestFuse(t *testing.T) {
	bm := []Hit{
		{Example: Example{Source: "tla", Transliteration: "tꞽ (j)", Translation: "Brot"}},
		{Example: Example{Source: "tla", Transliteration: "ḥnq.t", Translation: "Bier"}},
	}
	vec := []ScoredDoc{
		{Doc: Doc{ID: "aes:1", Source: "aes", Translit: "ḥnq.t", German: "Bier"}},
		{Doc: Doc{ID: "aes:2", Source: "aes", Translit: "nswt", German: "König"}},
	}
	got := fuse(bm, vec, 2)
	// "ḥnq.t" is in both lists, so it wins; the BM25 record is kept.
	if len(got) != 2 || got[0].Transliteration != "ḥnq.t" || got[0].Source != "tla" || got[1].Transliteration != "tꞽ (j)" {
		t.Errorf("fuse = %+v", got)
	}
}

func TestClientEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/v1/embeddings" || req.Model != "emb" || len(req.Input) != 2 {
			http.Error(w, "bad", 400)
			return
		}
		// Out of order, as the API allows.
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[0,1]},{"index":0,"embedding":[1,0]}]}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/v1", Model: "emb"}
	got, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil || len(got) != 2 || got[0][0] != 1 || got[1][1] != 1 {
		t.Fatalf("Embed = %v, %v", got, err)
	}
	if _, err := c.Embed(context.Background(), []string{"a"}); err == nil {
		t.Error("HTTP 400: want error")
	}
}

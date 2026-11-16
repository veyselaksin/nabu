package translate

import (
	"context"
	"math"
	"strings"
	"testing"
)

func TestChrF(t *testing.T) {
	var c ChrF
	c.Add("ꜥnḫ =f", "ꜥnḫ =f")
	if c.Score() != 100 {
		t.Errorf("identical = %v", c.Score())
	}
	var d ChrF
	d.Add("xyz", "abc")
	if d.Score() != 0 {
		t.Errorf("disjoint = %v", d.Score())
	}
	var e ChrF
	e.Add("", "abc")
	if e.Score() != 0 {
		t.Errorf("empty hypothesis = %v", e.Score())
	}
	// "ab" vs "abc": 1-grams P=1 R=2/3; 2-grams P=1 R=1/2; 3-grams: no hyp.
	var f ChrF
	f.Add("a b", "abc")
	p, r := 1.0, (2.0/3+0.5+0)/3
	want := 100 * 5 * p * r / (4*p + r)
	if math.Abs(f.Score()-want) > 1e-9 {
		t.Errorf("partial = %v, want %v", f.Score(), want)
	}
}

func TestEvaluate(t *testing.T) {
	llm := &fakeLLM{keywords: `{"keywords": ["Brot"]}`, translation: `{"transliteration": "tꞽ ḥnq.t"}`}
	tr := New(llm, fixtureIndex(t), Options{Examples: 2, Vectors: testVectors(t), Embedder: &queryEmbedder{v: []float32{1, 1, 0, 0}}})
	metas := []SFTMeta{
		{ID: "s1", Split: "test", Lang: "German", Input: "Brot und Bier", Reference: "tꞽ ḥnq.t", Group: "x:T1"},
		{ID: "s2", Split: "test", Lang: "English", Input: "bread", Reference: "tꞽ", Group: "t:tꞽ"},
		{ID: "s3", Split: "test", Lang: "German", Input: "König", Reference: "nswt", Group: "x:T2"},
	}
	rep, err := Evaluate(context.Background(), tr, metas, EvalOptions{Lang: "German", Label: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.N != 2 || rep.Retrieval != "hybrid" || rep.Exact != 0.5 || rep.Rows[0].ChrF != 100 || rep.Rows[1].ChrF > 20 {
		t.Errorf("report = %+v", rep)
	}
	// The reference itself is never among the examples given to the model.
	for _, call := range llm.calls {
		if len(call) > 1 && strings.HasSuffix(call[1].Content, "German: Brot und Bier") &&
			strings.Contains(call[1].Content, "→ tꞽ ḥnq.t\n") {
			t.Errorf("leak:\n%s", call[1].Content)
		}
	}
	if !strings.Contains(llm.calls[1][1].Content, "German: Brot und Bier") {
		t.Errorf("prompt: %s", llm.calls[1][1].Content)
	}
	if _, err := Evaluate(context.Background(), tr, metas, EvalOptions{Lang: "Coptic"}); err == nil {
		t.Error("no sentences: want error")
	}
}

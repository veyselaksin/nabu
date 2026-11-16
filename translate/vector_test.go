package translate

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestF16(t *testing.T) {
	for _, f := range []float32{0, 1, -1, 0.5, 0.1, -0.333, 65504, 6.1035156e-05, 5.9604645e-08, 1e-3} {
		got := f16ToF32(f32ToF16(f))
		if d := math.Abs(float64(got - f)); d > math.Abs(float64(f))*1e-3+6e-8 {
			t.Errorf("round trip %g = %g", f, got)
		}
	}
	if f32ToF16(1) != 0x3c00 || f32ToF16(-2) != 0xc000 || f32ToF16(1e6) != 0x7c00 || f32ToF16(1e-9) != 0 {
		t.Errorf("bits: %#x %#x %#x %#x", f32ToF16(1), f32ToF16(-2), f32ToF16(1e6), f32ToF16(1e-9))
	}
	if !math.IsNaN(float64(f16ToF32(f32ToF16(float32(math.NaN()))))) {
		t.Error("NaN lost")
	}
}

// fakeEmbedder maps a text to a vector by keyword; it fails after failAfter
// calls when failAfter > 0.
type fakeEmbedder struct {
	calls, failAfter int
	texts            []string
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.calls++
	if f.failAfter > 0 && f.calls > f.failAfter {
		return nil, errors.New("server went away")
	}
	f.texts = append(f.texts, texts...)
	out := make([][]float32, len(texts))
	for i, s := range texts {
		v := []float32{0.01, 0.01, 0.01, 0.01, 9}
		for j, w := range []string{"Brot", "Bier", "König", "leben"} {
			if strings.Contains(s, w) {
				v[j] = 1
			}
		}
		out[i] = v
	}
	return out, nil
}

func testDocs() []Doc {
	return []Doc{
		{ID: "s1", Kind: KindSentence, Source: "tla", Stage: "earlier", Translit: "tꞽ ḥnq.t", German: "Brot und Bier"},
		{ID: "s2", Kind: KindSentence, Source: "aes", Translit: "nswt ꜥnḫ", German: "Der König möge leben"},
		{ID: "s3", Kind: KindSentence, Source: "tla-late", Stage: "late", Translit: "pꜣ tꞽ", German: "das Brot"},
		{ID: "l1", Kind: KindLexicon, Source: "aed", Translit: "ꜥnḫ", German: "leben", English: "to live"},
		{ID: "l2", Kind: KindLexicon, Source: "aed", Translit: "ḥnq.t", German: "Bier", English: "beer"},
	}
}

func TestBuildLoadSearch(t *testing.T) {
	defer func(w time.Duration) { retryWait = w }(retryWait)
	retryWait = time.Millisecond
	dir := t.TempDir()
	docs := testDocs()
	emb := &fakeEmbedder{failAfter: 2}
	opts := BuildOptions{Model: "fake", Dims: 4, Batch: 2}
	err := BuildVectors(context.Background(), emb, docs, dir, opts)
	if err == nil {
		t.Fatal("want the injected failure")
	}
	if _, err := LoadVectors(dir); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("LoadVectors of a partial build: %v", err)
	}
	// Resume: only the fifth document is left.
	emb2 := &fakeEmbedder{}
	if err := BuildVectors(context.Background(), emb2, docs, dir, opts); err != nil {
		t.Fatal(err)
	}
	if len(emb2.texts) != 1 || emb2.texts[0] != "ḥnq.t: beer / Bier" {
		t.Errorf("resumed texts = %q", emb2.texts)
	}
	if _, err := os.Stat(filepath.Join(dir, buildFile)); !os.IsNotExist(err) {
		t.Error("build.json left behind")
	}

	vx, err := LoadVectors(dir)
	if err != nil {
		t.Fatal(err)
	}
	if vx.Len() != 5 || vx.Manifest.Dims != 4 || vx.Manifest.Kinds[KindLexicon] != 2 {
		t.Fatalf("manifest = %+v", vx.Manifest)
	}
	q := []float32{1, 1, 0, 0, 0} // "Brot", "Bier"; the fifth dimension is cut
	got := vx.Search(q, 2, func(d *Doc) bool { return d.Kind == KindSentence })
	if len(got) != 2 || got[0].ID != "s1" || got[1].ID != "s3" || got[0].Score < 0.99 {
		t.Errorf("Search = %+v", got)
	}
	got = vx.Search(q, 1, func(d *Doc) bool { return d.Kind == KindLexicon })
	if len(got) != 1 || got[0].ID != "l2" {
		t.Errorf("lexicon Search = %+v", got)
	}
	if vx.Search(q, 0, nil) != nil || vx.Search([]float32{1}, 3, nil) != nil {
		t.Error("k=0 or short query: want nil")
	}

	// A different model starts over.
	emb3 := &fakeEmbedder{}
	if err := BuildVectors(context.Background(), emb3, docs, dir, BuildOptions{Model: "other", Dims: 4, Batch: 2}); err != nil {
		t.Fatal(err)
	}
	if len(emb3.texts) != 5 {
		t.Errorf("rebuild embedded %d texts, want 5", len(emb3.texts))
	}
}

func TestEmbedText(t *testing.T) {
	d := testDocs()
	if d[0].embedText() != "Brot und Bier" || d[3].embedText() != "ꜥnḫ: to live / leben" {
		t.Errorf("%q %q", d[0].embedText(), d[3].embedText())
	}
	if !strings.HasPrefix(QueryText("hi"), "Instruct: ") || !strings.HasSuffix(QueryText("hi"), "\nQuery: hi") {
		t.Error(QueryText("hi"))
	}
}

func TestBuildVectorsReusesFinishedIndex(t *testing.T) {
	dir := t.TempDir()
	docs := testDocs()
	opts := BuildOptions{Model: "fake", Dims: 4, Batch: 2}
	if err := BuildVectors(context.Background(), &fakeEmbedder{}, docs, dir, opts); err != nil {
		t.Fatal(err)
	}
	before, err := LoadVectors(dir)
	if err != nil {
		t.Fatal(err)
	}

	// One document changes its text, one is new; the rest keep their vectors.
	changed := append([]Doc(nil), docs...)
	changed[1].German += " Bier"
	changed = append(changed, Doc{ID: "new", Kind: KindLexicon, Source: "aed", Translit: "tꞽ", English: "bread", German: "Brot"})
	// The first attempt dies after one request; the rerun must still reuse.
	emb := &fakeEmbedder{failAfter: 1}
	if err := BuildVectors(context.Background(), emb, changed, dir, opts); err == nil {
		t.Fatal("want an error from the interrupted rebuild")
	}
	emb2 := &fakeEmbedder{}
	if err := BuildVectors(context.Background(), emb2, changed, dir, opts); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{changed[1].embedText(): true, changed[len(changed)-1].embedText(): true}
	for _, text := range append(emb.texts, emb2.texts...) {
		if !want[text] {
			t.Errorf("embedded again: %q", text)
		}
		delete(want, text)
	}
	if len(want) != 0 {
		t.Errorf("not embedded: %v", want)
	}
	after, err := LoadVectors(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.docs) != len(changed) {
		t.Fatalf("docs = %d, want %d", len(after.docs), len(changed))
	}
	for i := range docs {
		if i == 1 {
			continue
		}
		if !reflect.DeepEqual(before.vecs[i*4:(i+1)*4], after.vecs[i*4:(i+1)*4]) {
			t.Errorf("vector of %s changed", docs[i].ID)
		}
	}
	left, _ := filepath.Glob(filepath.Join(dir, "prev-*"))
	if len(left) != 0 {
		t.Errorf("left over: %v", left)
	}

	// Another model: nothing is reused.
	emb3 := &fakeEmbedder{}
	if err := BuildVectors(context.Background(), emb3, changed, dir, BuildOptions{Model: "other", Dims: 4}); err != nil {
		t.Fatal(err)
	}
	if len(emb3.texts) != len(changed) {
		t.Errorf("other model embedded %d texts, want %d", len(emb3.texts), len(changed))
	}
}

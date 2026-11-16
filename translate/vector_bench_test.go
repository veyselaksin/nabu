package translate

import (
	"os"
	"runtime"
	"testing"
	"time"
)

// BenchmarkVectorSearchReal measures brute-force search on a real index:
//
//	NABU_RAG_INDEX=<data>/rag/egy go test ./translate -run '^$' -bench VectorSearchReal -benchtime 200x
//
// Queries are rows of the index itself, so no embedding server is needed.
func BenchmarkVectorSearchReal(b *testing.B) {
	dir := os.Getenv("NABU_RAG_INDEX")
	if dir == "" {
		b.Skip("set NABU_RAG_INDEX to a built vector index")
	}
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	vx, err := LoadVectors(dir)
	if err != nil {
		b.Fatal(err)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	b.Logf("%d docs × %d dims: loaded in %s, heap +%d MB", vx.Len(), vx.Manifest.Dims,
		time.Since(start).Round(time.Millisecond), (after.HeapAlloc-before.HeapAlloc)>>20)
	dims := vx.Manifest.Dims
	query := func(i int) []float32 { j := (i * 7919) % vx.Len(); return vx.vecs[j*dims : (j+1)*dims] }
	b.Run("lexicon-top10", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			vx.Search(query(i), 10, func(d *Doc) bool { return d.Kind == KindLexicon && d.Stage != "coptic" })
		}
	})
	b.Run("sentences-earlier-top16", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			vx.Search(query(i), 16, func(d *Doc) bool { return d.Kind == KindSentence && (d.Stage == "earlier" || d.Stage == "") })
		}
	})
	b.Run("unfiltered-top10", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			vx.Search(query(i), 10, nil)
		}
	})
}

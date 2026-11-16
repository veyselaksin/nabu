package translate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/veyselaksin/nabu/data/corpus"

	"github.com/veyselaksin/nabu/data/source"
	"github.com/veyselaksin/nabu/data/tla"
)

// LoadIndex reads the TLA corpus file downloaded by `nabu-data fetch tla`,
// after checking that it is the pinned version, and indexes it.
func LoadIndex(path string) (*Index, error) {
	src, _ := source.Get("tla")
	if err := source.VerifyFile(path, src.Files[0].SHA256); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s not found; run: make data-fetch", path)
		}
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sentences, err := tla.Read(f)
	if err != nil {
		return nil, err
	}
	return NewIndex(sentences), nil
}

// DefaultEmbedModel is the embedding model the index is built with by
// default: Qwen3-Embedding-0.6B (Apache-2.0), multilingual, served by
// Ollama as qwen3-embedding:0.6b.
const DefaultEmbedModel = "qwen3-embedding:0.6b"

// DefaultEmbedDims truncates the embeddings (Matryoshka) to keep the
// in-memory index small: ~130k docs × 512 × 4 B ≈ 260 MB.
const DefaultEmbedDims = 512

// LoadRAG loads the vector index in dir and adds the forms of its
// Middle-Egyptian-usable sentences to index's vocabulary check.
func LoadRAG(dir string, index *Index) (*VectorIndex, error) {
	vx, err := LoadVectors(dir)
	if err != nil {
		return nil, err
	}
	if index != nil {
		var forms []string
		for _, d := range vx.docs {
			if d.Kind == KindSentence && (d.Stage == "earlier" || d.Stage == "") {
				forms = append(forms, d.Translit)
			}
		}
		index.AddForms(forms)
	}
	return vx, nil
}

// ReadCorpus reads sentences.jsonl, lexicon.jsonl and the build time from a
// directory written by `nabu-data build-corpus`.
func ReadCorpus(dir string) ([]corpus.Sentence, []corpus.LexEntry, time.Time, error) {
	open := func(name string) (*os.File, error) {
		f, err := os.Open(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s not found; run: make corpus", filepath.Join(dir, name))
		}
		return f, err
	}
	f, err := open("sentences.jsonl")
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	defer f.Close()
	sents, err := corpus.ReadSentences(f)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	g, err := open("lexicon.jsonl")
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	defer g.Close()
	lex, err := corpus.ReadLexicon(g)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	var m corpus.Manifest
	if raw, err := os.ReadFile(filepath.Join(dir, "manifest.json")); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	return sents, lex, m.Built, nil
}

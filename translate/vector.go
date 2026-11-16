package translate

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/veyselaksin/nabu/data/corpus"
)

// Kinds of retrievable documents.
const (
	KindSentence = "sentence"
	KindLexicon  = "lexicon"
)

// Doc is one record of the vector index: a corpus sentence or a lexicon
// entry, with its provenance.
type Doc struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Source   string `json:"source"`
	Stage    string `json:"stage,omitempty"`
	TextID   string `json:"text_id,omitempty"`
	Translit string `json:"translit"`
	German   string `json:"de,omitempty"`
	English  string `json:"en,omitempty"`
	// EnglishSource names where English comes from when it is not Source.
	EnglishSource string `json:"en_source,omitempty"`
	POS           string `json:"pos,omitempty"`
	Date          string `json:"date,omitempty"`
}

// embedText is what gets embedded for a document. Queries carry an
// instruction (QueryText); documents do not, as the Qwen3 embedding models
// expect.
func (d Doc) embedText() string {
	switch d.Kind {
	case KindLexicon:
		var gloss []string
		for _, g := range []string{d.English, d.German} {
			if g != "" {
				gloss = append(gloss, g)
			}
		}
		return d.Translit + ": " + strings.Join(gloss, " / ")
	}
	if d.English != "" {
		return d.German + "\n" + d.English
	}
	return d.German
}

// queryInstruction tells an instruction-aware embedding model what the
// query is for.
const queryInstruction = "Given an English text, retrieve Ancient Egyptian sentences and dictionary entries whose translation has the same meaning"

// QueryText wraps a query in the instruction format of the Qwen3 embedding
// models.
func QueryText(text string) string {
	return "Instruct: " + queryInstruction + "\nQuery: " + text
}

// VectorManifest describes a built vector index.
type VectorManifest struct {
	Model  string `json:"model"`
	Dims   int    `json:"dims"`
	Docs   int    `json:"docs"`
	Format string `json:"format"`
	// Query is the instruction queries must be wrapped in (QueryText).
	Query       string         `json:"query_instruction"`
	Kinds       map[string]int `json:"kinds"`
	CorpusBuilt time.Time      `json:"corpus_built"`
	Built       time.Time      `json:"built"`
	License     string         `json:"license"`
}

const vectorFormat = "f16le, L2-normalized, row-major docs × dims"

// VectorIndex is an in-memory, brute-force cosine index.
type VectorIndex struct {
	Manifest VectorManifest
	docs     []Doc
	vecs     []float32 // len(docs) × dims, each row L2-normalized
}

// Len returns the number of documents.
func (vx *VectorIndex) Len() int { return len(vx.docs) }

// Docs returns the documents (do not modify).
func (vx *VectorIndex) Docs() []Doc { return vx.docs }

// ScoredDoc is a search result.
type ScoredDoc struct {
	Doc
	Score float64 `json:"score"`
}

// Search returns the k documents most similar to q (cosine) among those
// that keep accepts (nil accepts all), best first.
func (vx *VectorIndex) Search(q []float32, k int, keep func(*Doc) bool) []ScoredDoc {
	dims := vx.Manifest.Dims
	if k <= 0 || len(q) < dims {
		return nil
	}
	q = normalized(q[:dims])
	type cand struct {
		i int
		s float32
	}
	var top []cand // kept sorted, best first, at most k
	for i := range vx.docs {
		if keep != nil && !keep(&vx.docs[i]) {
			continue
		}
		row := vx.vecs[i*dims : (i+1)*dims]
		var s float32
		for j, x := range row {
			s += x * q[j]
		}
		if len(top) == k && s <= top[k-1].s {
			continue
		}
		pos := sort.Search(len(top), func(j int) bool { return top[j].s < s })
		if len(top) < k {
			top = append(top, cand{})
		}
		copy(top[pos+1:], top[pos:len(top)-1])
		top[pos] = cand{i, s}
	}
	out := make([]ScoredDoc, len(top))
	for j, c := range top {
		out[j] = ScoredDoc{Doc: vx.docs[c.i], Score: float64(c.s)}
	}
	return out
}

// normalized returns v scaled to unit length (truncating an embedding to
// fewer dimensions, as Matryoshka-trained models allow, needs this).
func normalized(v []float32) []float32 {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	out := make([]float32, len(v))
	if n == 0 {
		return out
	}
	inv := float32(1 / math.Sqrt(n))
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}

// Index files.
const (
	vecFile      = "vectors.f16"
	docFile      = "docs.jsonl"
	manifestFile = "manifest.json"
)

// LoadVectors reads an index written by BuildVectors.
func LoadVectors(dir string) (*VectorIndex, error) {
	raw, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if errors.Is(err, os.ErrNotExist) {
		if _, perr := os.Stat(filepath.Join(dir, docFile)); perr == nil {
			return nil, fmt.Errorf("vector index in %s is incomplete; rerun: make rag-index", dir)
		}
		return nil, fmt.Errorf("no vector index in %s; build it with: make rag-index", dir)
	}
	if err != nil {
		return nil, err
	}
	vx := &VectorIndex{}
	if err := json.Unmarshal(raw, &vx.Manifest); err != nil {
		return nil, fmt.Errorf("%s: %w", manifestFile, err)
	}
	dims := vx.Manifest.Dims
	if dims <= 0 {
		return nil, fmt.Errorf("%s: bad dims %d", manifestFile, dims)
	}
	f, err := os.Open(filepath.Join(dir, docFile))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if vx.docs, err = readDocs(f); err != nil {
		return nil, err
	}
	vf, err := os.Open(filepath.Join(dir, vecFile))
	if err != nil {
		return nil, err
	}
	defer vf.Close()
	buf := make([]byte, dims*2)
	r := bufio.NewReaderSize(vf, 1<<20)
	vx.vecs = make([]float32, 0, len(vx.docs)*dims)
	for range vx.docs {
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, fmt.Errorf("%s: fewer vectors than documents: %w", vecFile, err)
		}
		for j := 0; j < dims; j++ {
			vx.vecs = append(vx.vecs, f16ToF32(binary.LittleEndian.Uint16(buf[2*j:])))
		}
	}
	if len(vx.docs) != vx.Manifest.Docs {
		return nil, fmt.Errorf("vector index: manifest says %d docs, found %d", vx.Manifest.Docs, len(vx.docs))
	}
	return vx, nil
}

func readDocs(r io.Reader) ([]Doc, error) {
	var out []Doc
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var d Doc
		if err := json.Unmarshal(sc.Bytes(), &d); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", docFile, len(out)+1, err)
		}
		out = append(out, d)
	}
	return out, sc.Err()
}

// CorpusDocs turns a corpus into index documents: sentences of at most
// maxExampleWords words (longer ones would crowd a small model's prompt),
// then lexicon entries that have a gloss.
func CorpusDocs(sentences []corpus.Sentence, lexicon []corpus.LexEntry) []Doc {
	var out []Doc
	for _, s := range sentences {
		if n := len(strings.Fields(s.Translit)); n == 0 || n > maxExampleWords || s.German == "" {
			continue
		}
		out = append(out, Doc{ID: s.ID, Kind: KindSentence, Source: s.Source, Stage: s.Stage, TextID: s.TextID,
			Translit: s.Translit, German: s.German, Date: s.Date})
	}
	for _, e := range lexicon {
		if e.Lemma == "" || (e.English == "" && e.German == "") {
			continue
		}
		out = append(out, Doc{ID: e.ID, Kind: KindLexicon, Source: e.Source, Stage: e.Stage,
			Translit: e.Lemma, German: e.German, English: e.English, EnglishSource: e.EnglishSource, POS: e.POS})
	}
	return out
}

// BuildOptions configures BuildVectors.
type BuildOptions struct {
	Model string
	// Dims truncates embeddings to this many dimensions (0: keep all).
	// Qwen3 embedding models are Matryoshka-trained, so a prefix of the
	// vector, renormalized, is still a good embedding.
	Dims        int
	Batch       int // texts per request (default 64)
	CorpusBuilt time.Time
	Progress    io.Writer
}

// BuildVectors embeds docs and writes the index to dir. It resumes an
// interrupted build: documents already embedded (same order) are kept.
// Rebuilding over a finished index made with the same model and dimensions
// embeds only texts that index does not have; the old index is set aside
// (prev-*) until the new one is complete.
func BuildVectors(ctx context.Context, emb Embedder, docs []Doc, dir string, opts BuildOptions) error {
	if opts.Batch <= 0 {
		opts.Batch = 64
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	prev, err := previousVectors(dir, opts.Model, opts.Dims)
	if err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(dir, manifestFile)) // incomplete until rewritten
	done, dims, err := resumePoint(dir, docs, opts.Model, opts.Dims)
	if err != nil {
		return err
	}
	if dims == 0 && prev != nil {
		dims = prev.dims
		if err := writeBuildState(dir, buildState{Model: opts.Model, Dims: dims}); err != nil {
			return err
		}
	}
	reused := 0
	vf, err := os.OpenFile(filepath.Join(dir, vecFile), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer vf.Close()
	df, err := os.OpenFile(filepath.Join(dir, docFile), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer df.Close()

	start := time.Now()
	for i := done; i < len(docs); i += opts.Batch {
		batch := docs[i:min(i+opts.Batch, len(docs))]
		texts := make([]string, len(batch))
		for j, d := range batch {
			texts[j] = d.embedText()
		}
		// Vectors as stored bytes; nil until known.
		stored := make([][]byte, len(batch))
		var missing []string
		var missingAt []int
		for j, text := range texts {
			if stored[j] = prev.lookup(text); stored[j] == nil {
				missing, missingAt = append(missing, text), append(missingAt, j)
			} else {
				reused++
			}
		}
		var vecs [][]float32
		if len(missing) > 0 {
			if vecs, err = embedRetry(ctx, emb, missing); err != nil {
				return fmt.Errorf("embedding documents %d–%d: %w", i+1, i+len(batch), err)
			}
		}
		if dims == 0 {
			dims = len(vecs[0])
			if opts.Dims > 0 && opts.Dims < dims {
				dims = opts.Dims
			}
			if err := writeBuildState(dir, buildState{Model: opts.Model, Dims: dims}); err != nil {
				return err
			}
		}
		for k, v := range vecs {
			if len(v) < dims {
				return fmt.Errorf("embedding has %d dimensions, want %d", len(v), dims)
			}
			b := make([]byte, 0, dims*2)
			for _, x := range normalized(v[:dims]) {
				b = binary.LittleEndian.AppendUint16(b, f32ToF16(x))
			}
			stored[missingAt[k]] = b
		}
		var vb []byte
		var db strings.Builder
		for j := range batch {
			vb = append(vb, stored[j]...)
			line, _ := json.Marshal(batch[j])
			db.Write(line)
			db.WriteByte('\n')
		}
		// Vectors first: on resume, documents without a vector are dropped.
		if _, err := vf.Write(vb); err != nil {
			return err
		}
		if _, err := df.WriteString(db.String()); err != nil {
			return err
		}
		if opts.Progress != nil {
			n := i + len(batch)
			rate := float64(n-done) / time.Since(start).Seconds()
			eta := time.Duration(float64(len(docs)-n)/rate) * time.Second
			fmt.Fprintf(opts.Progress, "\rembedded %d/%d (%d reused, %.0f/s, %s left)   ", n, len(docs), reused, rate, eta.Round(time.Second))
		}
	}
	if opts.Progress != nil {
		fmt.Fprintln(opts.Progress)
	}
	kinds := map[string]int{}
	for _, d := range docs {
		kinds[d.Kind]++
	}
	m := VectorManifest{
		Model: opts.Model, Dims: dims, Docs: len(docs), Format: vectorFormat, Query: queryInstruction,
		Kinds: kinds, CorpusBuilt: opts.CorpusBuilt, Built: time.Now().UTC().Truncate(time.Second),
		License: "CC-BY-SA-4.0 (derived from the corpus sources; keep local, see DATA_LICENSES.md)",
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, manifestFile), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	for _, name := range []string{vecFile, docFile, manifestFile} {
		if err := os.Remove(filepath.Join(dir, prevPrefix+name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Remove(filepath.Join(dir, buildFile))
}

const prevPrefix = "prev-"

// prevVectors are the vectors of the index a rebuild replaces, by the text
// that was embedded.
type prevVectors struct {
	dims int
	data []byte         // stored half-precision vectors
	at   map[string]int // embedded text → record number
}

// lookup returns the stored vector for text, or nil.
func (p *prevVectors) lookup(text string) []byte {
	if p == nil {
		return nil
	}
	i, ok := p.at[text]
	if !ok {
		return nil
	}
	return p.data[i*p.dims*2 : (i+1)*p.dims*2]
}

// previousVectors sets a finished index in dir aside as prev-* and loads
// it (or one set aside by an interrupted rebuild). It returns nil when
// there is none or it was built with another model or dimension count.
func previousVectors(dir, model string, wantDims int) (*prevVectors, error) {
	path := func(name string) string { return filepath.Join(dir, prevPrefix+name) }
	if _, err := os.Stat(filepath.Join(dir, manifestFile)); err == nil {
		// Manifest last: without it the set-aside copy is not used.
		for _, name := range []string{vecFile, docFile, manifestFile} {
			if err := os.Rename(filepath.Join(dir, name), path(name)); err != nil {
				return nil, err
			}
		}
		_ = os.Remove(filepath.Join(dir, buildFile))
	}
	var m VectorManifest
	raw, err := os.ReadFile(path(manifestFile))
	if err != nil || json.Unmarshal(raw, &m) != nil || m.Model != model || m.Dims <= 0 ||
		(wantDims > 0 && m.Dims != wantDims) {
		return nil, nil
	}
	f, err := os.Open(path(docFile))
	if err != nil {
		return nil, nil
	}
	docs, err := readDocs(f)
	f.Close()
	if err != nil {
		return nil, nil
	}
	data, err := os.ReadFile(path(vecFile))
	if err != nil || len(data) < len(docs)*m.Dims*2 {
		return nil, nil
	}
	p := &prevVectors{dims: m.Dims, data: data, at: make(map[string]int, len(docs))}
	for i, d := range docs {
		p.at[d.embedText()] = i
	}
	return p, nil
}

// buildState records an in-progress build, so that it can resume.
type buildState struct {
	Model string `json:"model"`
	Dims  int    `json:"dims"`
}

const buildFile = "build.json"

func writeBuildState(dir string, st buildState) error {
	raw, _ := json.Marshal(st)
	return os.WriteFile(filepath.Join(dir, buildFile), raw, 0o644)
}

// resumePoint returns how many leading docs an interrupted build already
// embedded, and their dimensions, truncating the files to that many
// records. It starts over when the model, the requested dimensions or the
// stored documents differ.
func resumePoint(dir string, docs []Doc, model string, wantDims int) (done, dims int, err error) {
	vpath, dpath := filepath.Join(dir, vecFile), filepath.Join(dir, docFile)
	reset := func() (int, int, error) {
		for _, p := range []string{vpath, dpath, filepath.Join(dir, buildFile)} {
			if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				return 0, 0, err
			}
		}
		return 0, 0, nil
	}
	var st buildState
	raw, err := os.ReadFile(filepath.Join(dir, buildFile))
	if err != nil || json.Unmarshal(raw, &st) != nil || st.Model != model || st.Dims <= 0 ||
		(wantDims > 0 && st.Dims != wantDims) {
		return reset()
	}
	f, err := os.Open(dpath)
	if err != nil {
		return reset()
	}
	stored, err := readDocs(f)
	f.Close()
	fi, serr := os.Stat(vpath)
	if err != nil || serr != nil {
		return reset()
	}
	n := min(len(stored), int(fi.Size()/int64(st.Dims*2)), len(docs))
	for i := 0; i < n; i++ {
		if stored[i] != docs[i] {
			return reset()
		}
	}
	if err := os.Truncate(vpath, int64(n*st.Dims*2)); err != nil {
		return 0, 0, err
	}
	var b strings.Builder
	for _, d := range stored[:n] {
		line, _ := json.Marshal(d)
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(dpath, []byte(b.String()), 0o644); err != nil {
		return 0, 0, err
	}
	return n, st.Dims, nil
}

// f32ToF16 converts to IEEE 754 half precision, rounding to nearest even.
func f32ToF16(f float32) uint16 {
	b := math.Float32bits(f)
	sign := uint16(b>>16) & 0x8000
	exp := int((b>>23)&0xff) - 127 + 15
	mant := b & 0x7fffff
	switch {
	case (b>>23)&0xff == 0xff: // Inf or NaN
		if mant != 0 {
			return sign | 0x7e00
		}
		return sign | 0x7c00
	case exp >= 0x1f:
		return sign | 0x7c00
	case exp <= 0:
		if exp < -10 {
			return sign
		}
		mant |= 0x800000
		shift := uint(14 - exp)
		half := mant >> shift
		rem := mant & (1<<shift - 1)
		mid := uint32(1) << (shift - 1)
		if rem > mid || rem == mid && half&1 == 1 {
			half++
		}
		return sign | uint16(half)
	}
	half := uint32(exp)<<10 | mant>>13
	rem := mant & 0x1fff
	if rem > 0x1000 || rem == 0x1000 && half&1 == 1 {
		half++ // may carry into the exponent, which is correct
	}
	return sign | uint16(half)
}

// f16ToF32 converts from IEEE 754 half precision.
func f16ToF32(h uint16) float32 {
	sign := uint32(h&0x8000) << 16
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h & 0x3ff)
	switch {
	case exp == 0 && mant == 0:
		return math.Float32frombits(sign)
	case exp == 0: // subnormal
		e := uint32(127 - 15 + 1)
		for mant&0x400 == 0 {
			mant <<= 1
			e--
		}
		return math.Float32frombits(sign | e<<23 | (mant&0x3ff)<<13)
	case exp == 0x1f:
		return math.Float32frombits(sign | 0xff<<23 | mant<<13)
	}
	return math.Float32frombits(sign | (exp+127-15)<<23 | mant<<13)
}

// retryWait is the first backoff of embedRetry (tests shorten it).
var retryWait = 2 * time.Second

// embedRetry retries a failed embeddings request a few times with backoff:
// a long build should survive a server that restarts or drops a connection.
func embedRetry(ctx context.Context, emb Embedder, texts []string) ([][]float32, error) {
	var err error
	for attempt, wait := 0, retryWait; attempt < 5; attempt, wait = attempt+1, wait*2 {
		var vecs [][]float32
		if vecs, err = emb.Embed(ctx, texts); err == nil {
			return vecs, nil
		}
		var le *LLMError
		if errors.As(err, &le) && le.Status >= 400 && le.Status < 500 {
			return nil, err // a bad request does not get better
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, err
}

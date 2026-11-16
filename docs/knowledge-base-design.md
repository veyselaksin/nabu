# Knowledge base and retrieval: comparison and design (Phase 2)

Status 2026-11-16: the vector-store comparison is measured (section 1). The
embedding-model comparison (section 2) is designed but not run: it needs
model downloads over 1 GB, which wait for approval. Sections 3 to 6 are the
proposed design; nothing in them is built yet except what section 5 marks as
existing.

## 1. Vector store

### What the store has to do

- Hold about 126,000 vectors of 512 dimensions today (Egyptian). Sumerian,
  Akkadian and Hittite will add records, mostly sentences and lexicon
  entries; a few hundred thousand more is a reasonable guess, not a measurement.
- Filter by metadata on every query (kind, language, stage, source).
- Be called from Go, in the API process, with no Python at request time.
- Live under `$MODELS_ROOT`, start with `./start.sh`, and be rebuilt from the
  corpus by one resumable command.

### Measured on this machine (Apple M4, 16 GB)

Same data for every store: the real index (125,879 × 512), 200 queries, top
10 among lexicon entries that are not Coptic (33,412 of the documents pass
the filter). Recall is against exact search.

| Store (version) | How it runs | Ingest | On disk | p50 | p95 | recall@10 |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Exact search, NumPy (reference) | in process | – | 129 MB | 1.2 ms | 1.3 ms | 1.000 |
| LanceDB 0.41, no index | embedded (Python) | 0.8 s | 258 MB | 28.1 ms | 30.1 ms | 1.000 |
| LanceDB 0.41, IVF_PQ, default search settings | embedded (Python) | 15.7 s | 268 MB | 3.4 ms | 6.2 ms | 0.455 |
| Chroma 1.5.9, HNSW | embedded (Python) | 26.6 s | 333 MB | 52.9 ms | 71.0 ms | 0.988 |
| Milvus Lite 3.2.2, default index | embedded (Python) | 15.8 s | 335 MB | 1,402 ms | 2,085 ms | 1.000 |
| Qdrant 1.20.2, HNSW, payload indexes | separate server, HTTP | 14.2 s | 353 MB | 1.1 ms | 1.4 ms | 0.978 |

Every store was run with its defaults; none was tuned. LanceDB's IVF_PQ
recall and Milvus Lite's latency would both improve with tuning, so read
those two rows as "what you get out of the box", not as limits. pgvector was
not measured: it needs a PostgreSQL server, and installing a system service
was outside what had been agreed. The benchmark script and its environment
are in `$MODELS_ROOT/apps/nabu-vdb-bench/` (not part of the repository).

The current Go implementation, measured separately with
`go test ./translate -bench VectorSearchReal` (single-threaded, float32 in
memory, loaded from float16 on disk in 0.8 s, 293 MB heap):

| Query | Time per query |
| --- | ---: |
| top 10 lexicon entries, filtered | 12.5 ms |
| top 16 earlier-stage sentences, filtered | 23.9 ms |
| top 10, no filter | 45.2 ms |

For scale: one translation makes two or three calls to a local 4B language
model, which take seconds each.

### Comparison

| | Setup for Nabu | Called from Go | Metadata filter | Keyword / hybrid search | License |
| --- | --- | --- | --- | --- | --- |
| Flat file, in process (current) | none: two files under the data root | native | a Go function per query | Nabu's own BM25 and exact lookups, fused in Go | Nabu (Apache-2.0) |
| Qdrant | one more server process in `start.sh` and compose | official client | yes, indexed | sparse vectors with built-in fusion | Apache-2.0 |
| pgvector | PostgreSQL server plus extension | through any PostgreSQL driver | SQL | PostgreSQL full-text search, combined in SQL | PostgreSQL |
| LanceDB | embedded, but in Python, TypeScript or Rust | no official Go SDK known | SQL-like | full-text index and hybrid queries | Apache-2.0 |
| Chroma | embedded only in Python; otherwise a server | community client, against the server | yes | limited | Apache-2.0 |
| Milvus Lite | embedded only in Python; otherwise a full Milvus server | client exists for the server, not for Lite | yes | sparse and hybrid in full Milvus | Apache-2.0 |

The "Called from Go" and "Keyword / hybrid" columns are from the projects'
documentation as recalled, not from tests run here. Check them again before
adopting any of these.

### Decision

**Keep the in-process flat index, behind a small interface. Do not add a
vector database now.**

- At this size exact search is fast enough by two orders of magnitude next
  to the language model, and it is the only option that is exact.
- The three embedded stores are embedded only for Python. From Go they mean
  a server or a foreign-function bridge, which is the complexity "embedded
  and simple" was meant to avoid.
- What made retrieval better today was an exact dictionary lookup, not a
  different store. The hybrid logic (exact, BM25, dense, fusion) is a few
  hundred lines of Go that are easy to test and to debug.
- The current search loop is plain scalar Go. The NumPy row shows the same
  exact search at 1.2 ms, so there is a tenfold reserve before any index is
  needed.

Revisit when any of these happens: more than about one million vectors,
filtered p95 above 100 ms, or a need to update the index while serving.
Qdrant is then the first candidate: fastest measured, indexed filters, and
an official Go client. Its cost is one more process to run and supervise.

Follow-up in the repository: the README still lists "PostgreSQL + pgvector"
as the vector store; it should say what the code does.

## 2. Embedding model (not yet run)

The index uses Qwen3-Embedding-0.6B (Apache-2.0, 512 of its dimensions,
through Ollama). It was chosen without a comparison.

Candidates, all with permissive licenses to be re-verified at download time:

| Model | Why | Download |
| --- | --- | --- |
| Qwen3-Embedding-0.6B | current baseline | present |
| BGE-M3 | multilingual; also produces sparse vectors | about 1.2 GB via Ollama |
| multilingual-e5-large (or -base) | common strong multilingual baseline | about 2.2 GB (1.1 GB) |

No transliteration-aware embedding model for Egyptian was found. The plan
does not depend on one: transliterated forms are matched exactly (section 4),
and embeddings only compare the English and German sides.

Protocol: re-embed the 44,661 lexicon entries with each model and run
`nabu-translate eval-glossary --mode similar` on the 112-query set, then the
same for sentences once a sentence query set exists. Keep the current model
unless another is clearly better on both.

Measured so far, current model only, dictionary retrieval:

| Glossary | recall@1 | recall@5 | recall@10 | MRR |
| --- | ---: | ---: | ---: | ---: |
| vector search only | 28.6% | 63.4% | 79.5% | 0.439 |
| exact gloss lookup + vector search | 67.0% | 82.1% | 88.4% | 0.739 |

The exact lookup moved recall@1 by 38 points. No embedding swap is likely to
matter as much for dictionary entries, which is why it comes second.

## 3. Record schema

One record type for everything retrievable. Fields marked * exist today.

| Field | Meaning |
| --- | --- |
| `id`* | stable, `<source>:<source id>` |
| `kind`* | `sentence`, `lexicon`, `grammar`, `phonology`, `convention` |
| `lang` | ISO 639-3: `egy`, `cop`, `sux`, `akk`, `hit` |
| `stage`* | language stage or dialect (`earlier`, `late`, `demotic`, `sahidic`, …); empty when the source does not settle it |
| `date`* | as given by the source |
| `translit`* | normalized transliteration (the project's convention for that language) |
| `translit_orig` | transliteration exactly as the source wrote it |
| `script` | hieroglyphs or cuneiform in Unicode, when the source has them |
| `text` | translations and glosses by language: `de`*, `en`*, `fr` |
| `text_origin` | `editor` or `machine` for each translation, so machine English is never mistaken for an edition |
| `lemmas` | lemma IDs of the tokens (sentences) or the entry's own ID (lexicon) |
| `source`* | key in `sources/registry.yaml` |
| `source_ref` | where in the source: text ID, line, page or section |
| `license` | SPDX identifier, copied from the registry at build time |
| `tier` | reliability tier, copied from the registry |
| `confidence` | `attested`, `reconstructed`, `conjectural` for the claim the record makes |

`license`, `tier` and `source_ref` travel with the record into every API
response, so an answer can say what each choice rests on and how far to trust it.

## 4. Chunking and retrieval per content type

| Content | Chunk | Retrieved by |
| --- | --- | --- |
| Lexicon | one entry, never split | exact gloss match (built), exact lemma match, then embedding of the glosses |
| Parallel text | one aligned sentence | BM25 on the translation, embedding of the translation, exact match on lemma IDs once the glossary has picked lemmas |
| Grammar | one section with its heading path | embedding and BM25 on the section text, filtered by phenomenon tags |
| Phonology | one rule or correspondence | exact match on the grapheme or phoneme; not embedded |
| Conventions | one rule of a transliteration system | exact match; not embedded |

Three changes to retrieval follow from the audit and from today's measurement:

1. **Lemma-driven examples.** After the glossary picks lemmas for the words
   of the text, retrieve sentences that contain those lemma IDs. Today
   examples come only from the German side, through keywords the language
   model guesses.
2. **BM25 over the whole corpus**, not only TLA Earlier Egyptian.
3. **Stage as a filter with a known value.** Use the ORAEC metadata and the
   UD treebank's stage labels to settle the 32,969 unknown-stage AES
   sentences, or leave them out of Middle Egyptian retrieval.

A reranker is tried only after these, and kept only if it improves the
query-set numbers.

Open-access grammar text is the gap: no openly licensed Middle Egyptian
grammar was found in Phase 1. The `grammar` kind starts with what can be
derived from annotated data (the UD treebank's constructions with real
examples) and stays empty of prose until a licensed source exists.

## 5. Ingestion

Existing: pinned downloads with checksums (`nabu-data fetch`), a merge with
normalization and deduplication (`build-corpus`), a resumable index build
(`nabu-translate index`).

To add:

- one loader per new source (ORAEC etymologies and metadata, Wiktionary,
  UD), each writing records in the schema above;
- a data-quality report per source, written next to the corpus: record
  counts, empty fields, characters outside the language's alphabet,
  duplicate rate, length outliers;
- `translit_orig` kept alongside `translit` for every source that is
  normalized (AES, UD, Wiktionary);
- the Go registry generated from `sources/registry.yaml`, so that a source
  cannot be fetched unless its registry entry says `ingest`.

## 6. Transliteration normalization

One normalizer per language, with the target convention fixed per language.
For Egyptian the target is the TLA convention already in use (`ꞽ`, `i̯`,
`u̯`, `.` before endings, `=` before suffix pronouns). Known source
conventions to map: BBAW 2018 (AES, done), Tübingen/Schenkel (UD: `ś`, `č`,
`ṭ`, `č̣`, `ḳ`), English Wiktionary (`j`, no separators), and Manuel de
Codage ASCII (`A`, `a`, `H`, `x`, `X`, `S`, `T`, `D`). `LemmaKey` in
`translate/eval_gloss.go` is a first, lossy step toward comparing across
them and is used only for evaluation.

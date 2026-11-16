# translate

English → Middle Egyptian transliteration with a local language model, grounded
by retrieval (RAG) from the open Egyptological corpora: TLA, AES, the AED
dictionary. Go module
`github.com/veyselaksin/nabu/translate`, CLI `nabu-translate`, and the API's
`POST /v1/translate`.

> [!IMPORTANT]
> The output is unchecked machine translation. A general-purpose model that
> has seen a few example sentences makes grammatical mistakes and invents words.
> Modern concepts have no ancient equivalent. Treat every result as a draft
> for a human to check.

## How it works

The corpora translate their sentences into **German**, not English, so a
request runs in these steps:

1. The model turns the English text into up to 8 German search terms. A BM25
   index over the TLA's German translations ranks sentences: the best match
   for each term first, then the best overall. A term of 4 or more letters
   also matches words that start with it ("Brot" matches "Brotes").
2. With the vector index (below), the English text itself is embedded and
   compared with every corpus sentence and dictionary entry. The embedding
   model is multilingual, so English finds German translations by meaning,
   without the keyword step's guesses. Only sentences usable as Middle
   Egyptian are retrieved (stage "earlier", or undated AES texts; not Late
   Egyptian or Demotic), and no Coptic dictionary entries. The two sentence
   rankings are merged by reciprocal-rank fusion (RRF, k = 60). The 8 best
   sentences become examples. Sentences of more than 40 words are not
   retrieved.

   The glossary (10 or more dictionary entries: lemma, English and German
   meaning) is built in two ways. First, every word of the text, every pair
   of adjacent words and every German keyword is looked up among the
   dictionary glosses: an entry matches when one of its glosses is exactly
   that word ("live" finds `ꜥnḫ: to live; to be alive`, not the title
   `ḥqꜣ-ꜥnḫ.w`). Entries sharing a gloss are ranked by how often the lemma
   occurs in the lemmatized TLA, and at most 2 are kept per word. The
   remaining places, at least 3, go to the entries nearest by embedding.
3. The model translates the English text, with the glossary, the examples
   (German translation → transliteration, the editor's brackets removed) and
   the corpus alphabet: every grapheme (a letter with its combining marks, so
   "i̯" is one) that makes up at least 1/20,000 of the corpus forms. This keeps
   the TLA's signs and markers and drops stray characters, e.g. a bare "i"
   (3 times in v18), where the TLA writes "ꞽ" or "i̯".
4. If the answer still uses graphemes outside that alphabet, the model is asked
   once to correct those words; the correction is kept only if it has fewer
   such words (`corrected` in the response). Words that remain list their
   graphemes in `foreign`: the script and G2P stages would reject them.

Every output word is then checked, and the response says for each one:

- `gloss`: what the word means according to the dictionary, through the
  lemma the TLA most often analyses that form as. This is independent of
  what the model intended, so a wrong word shows up as a wrong meaning.
- `evidence`: `dictionary` (its lemma is an entry matched to a word of the
  input), `example` (it occurs in a retrieved example), `corpus` (it occurs
  somewhere in the corpus) or `none`.
- `attested`, `count`, `foreign`: whether and how often the form occurs in
  the corpus, and graphemes the corpus never writes.

`coverage` lists each content word of the input and the output words whose
dictionary entry has it as a gloss. A word that is not matched may be
missing from the translation, or translated by a lemma that only has a
German gloss (about half of the AED entries have no English; the corpus
build fills in 218 of them from Wiktionary, marked `en_source`).

**Confidence** is the share of output words with `dictionary` or `example`
evidence, times the share of input words matched in `coverage`. It measures
support, not correctness: grammar and word order are not checked.
`attested_share` is the plain vocabulary check.

### Measuring dictionary retrieval

```sh
nabu-translate eval-glossary --build <data>/wiktionary-egy/kaikki.org-dictionary-Egyptian.jsonl
nabu-translate eval-glossary --mode similar   # vector search only, for comparison
```

`--build` derives a query set from English Wiktionary's Egyptian entries
(first gloss → headword, for lemmas that occur at least 20 times in the
TLA) and stores it under the data root; it is CC BY-SA and stays local.
Nobody has reviewed the pairs, and some differ from the AED only by
convention, so read the numbers as a comparison between settings. On the
112 queries built on 2026-11-16, with `qwen3-embedding:0.6b`:

| Glossary | recall@1 | recall@5 | recall@10 | MRR |
| --- | ---: | ---: | ---: | ---: |
| vector search only | 28.6% | 63.4% | 79.5% | 0.439 |
| exact lookup + vector search | 67.0% | 82.1% | 88.4% | 0.739 |
| the same, after Wiktionary glosses joined the lexicon | 67.9% | 83.0% | 89.3% | 0.748 |

The last row is not an independent gain: the queries come from Wiktionary
too, and 6 of the 112 have an answer whose English gloss is now
Wiktionary's own.

Responses include the examples and glossary entries with their record IDs and
sources (`tla:…`, `aes:…`, `aed:…`), the German search terms, and `retrieval`:
`hybrid` or `bm25`. If the embedding server fails, translation falls back to
BM25 and says why in `retrieval_note`.

### The corpus and the vector index

```sh
make corpus       # fetch all sources (~400 MB) and merge them into $(CORPUS)
make embed-pull   # qwen3-embedding:0.6b into Ollama (~640 MB, Apache-2.0)
make rag-index    # embed ~126k sentences and entries into $(RAG_INDEX) (~1 h the first time, resumable; a rebuild embeds only new or changed texts)
```

`nabu-data build-corpus` merges TLA Earlier Egyptian v18, Late Egyptian v19,
Demotic v18 and AES (converted to the TLA's transliteration conventions,
intact sentences only, TLA duplicates dropped) into `sentences.jsonl`
(81,700 sentences), and AED plus the Coptic lexicon into `lexicon.jsonl`
(44,700 entries). `manifest.json` lists the pinned source files and the
counts. The index stores 512-dimensional vectors (Qwen3-Embedding is
Matryoshka-trained, so a prefix is still a good embedding) as float16:
`vectors.f16`, `docs.jsonl`, `manifest.json`. Search is brute-force cosine in
memory (~260 MB). There is no vector database.

## Running it

You need:

- the TLA corpus: `make data-fetch` (CC BY-SA 4.0, checksum-pinned, stays in
  the local data root; `make paths` prints it);
- a server implementing the OpenAI chat-completions API. The default is
  [Ollama](https://ollama.com) (MIT) with `qwen3:4b-instruct`
  (Qwen3-4B-Instruct-2507, Apache-2.0):

```sh
make run-llm          # terminal 1: ollama serve
make llm-pull         # once: download qwen3:4b-instruct (~2.5 GB)
make run-api          # terminal 2: enables translation when the corpus exists
make run-web          # terminal 3
```

Command line:

```sh
export NABU_LLM_URL=http://127.0.0.1:11434/v1 NABU_LLM_MODEL=qwen3:4b-instruct
go run ./translate/cmd/nabu-translate "may the king live"
go run ./translate/cmd/nabu-translate --json "bread and beer"
go run ./translate/cmd/nabu-translate --search "König Leben"   # retrieval only, no model
```

Any OpenAI-compatible server works. For example, llama.cpp (MIT):

```sh
llama-server -hf Qwen/Qwen3-8B-GGUF:Q4_K_M --port 8090
make run-api LLM_URL=http://127.0.0.1:8090/v1
```

| Variable | Meaning |
| --- | --- |
| `NABU_LLM_URL` | API root, e.g. `http://127.0.0.1:11434/v1`. Unset: translation disabled (503). |
| `NABU_LLM_MODEL` | Model name sent to the server (Ollama needs it; llama-server ignores it). |
| `NABU_LLM_API_KEY` | Optional bearer token. |
| `NABU_TLA_CORPUS` | `<data root>/tla/train.jsonl` (`make run-api` sets it); required with `NABU_LLM_URL`. |
| `NABU_RAG_INDEX` | Vector index directory (`make run-api` sets it when the index exists). Unset: BM25 only. |
| `NABU_EMBED_MODEL` | Embedding model; must be the one the index was built with. Default `qwen3-embedding:0.6b`. |
| `NABU_EMBED_URL` | Embeddings API root. Default: `NABU_LLM_URL`. |
| `NABU_TRANSLATE_TIMEOUT` | Default `3m`. Other routes keep `NABU_REQUEST_TIMEOUT`. |

`GET /v1/translator` reports whether translation is configured and whether the
model server answers.

## Licenses

- **Model weights** must be under an OSI-approved license. Check each
  checkpoint, because licenses differ within a family. For example,
  Qwen2.5-3B is under a custom license, while Qwen2.5-7B and Qwen3-4B/8B are
  Apache-2.0. Llama and Gemma licenses are not allowed. Nabu never downloads
  or commits weights itself: Ollama or llama.cpp stores them.
- **TLA, AES, AED, Coptic lexicon** (all CC BY-SA 4.0). The merged corpus and
  the vector index are adaptations: they stay local and are never committed
  (see the decision in [DATA_LICENSES.md](../DATA_LICENSES.md)).
  `/v1/translate` returns the examples and dictionary entries it used, with
  the attribution of every source they came from.
- Text goes only to the configured `NABU_LLM_URL`.

## Not done yet

- Synthetic English for the corpus sentences (`backfill-en`, translated from
  the editors' German by the local model), for retrieval and fine-tuning.
- LoRA fine-tuning (mlx-lm) on the corpus, with retrieval in the training
  prompts.
- An evaluation on held-out texts, with chrF.
- Akkadian, Sumerian and Hittite.

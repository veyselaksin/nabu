# translate

English → Middle Egyptian transliteration with a local language model, grounded
by retrieval from the TLA corpus (RAG). Go module
`github.com/veyselaksin/nabu/translate`, CLI `nabu-translate`, and the API's
`POST /v1/translate`.

> [!IMPORTANT]
> The output is unchecked machine translation. A general-purpose model that
> has seen a few example sentences makes grammatical mistakes and invents words.
> Modern concepts have no ancient equivalent. Treat every result as a draft
> for a human to check.

## How it works

The TLA's sentences are translated into **German**, not English, so a request
runs in three steps:

1. The model turns the English text into up to 8 German search terms.
2. A BM25 index over the TLA's German translations retrieves up to 8 sentences:
   the best match for each term first, then the best overall. A term of 4 or
   more letters also matches words that start with it ("Brot" matches
   "Brotes"). Duplicate sentences and sentences of more than 40 words are not
   indexed.
3. The model translates the English text, with those sentences as examples
   (German translation → transliteration, the editor's brackets removed).

Every output word is then looked up among the corpus's transliterated forms.
**Confidence** is the share of output words found there. It is a vocabulary
check only. A sentence made of real words can still be ungrammatical or mean
something else.

Responses include the examples and the German search terms, so you can see what
the model was given.

## Running it

You need:

- the TLA corpus: `make data-fetch` (CC BY-SA 4.0, checksum-pinned, stays in
  `data/corpora/`);
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
| `NABU_TLA_CORPUS` | `data/corpora/tla/train.jsonl`; required with `NABU_LLM_URL`. |
| `NABU_TRANSLATE_TIMEOUT` | Default `3m`. Other routes keep `NABU_REQUEST_TIMEOUT`. |

`GET /v1/translator` reports whether translation is configured and whether the
model server answers.

## Licenses

- **Model weights** must be under an OSI-approved license. Check each
  checkpoint, because licenses differ within a family. For example,
  Qwen2.5-3B is under a custom license, while Qwen2.5-7B and Qwen3-4B/8B are
  Apache-2.0. Llama and Gemma licenses are not allowed. Nabu never downloads
  or commits weights itself: Ollama or llama.cpp stores them.
- **TLA** (CC BY-SA 4.0). The corpus is read at startup and never written
  anywhere. `/v1/translate` returns the example sentences it used, together
  with the TLA attribution. See [DATA_LICENSES.md](../DATA_LICENSES.md) for
  the open questions about share-alike.
- Text goes only to the configured `NABU_LLM_URL`.

## Not done yet

- LoRA fine-tuning (`translate/train/`, Python) on corpus sentence pairs. The
  pairs are German–Egyptian, so the source language needs a decision first.
- An evaluation set of held-out expert translations, with chrF.
- Lemma-level dictionary glosses, which would let retrieval find single words
  and not only sentences.
- Sumerian and Hittite.

# translate/train: fine-tuning the translation model

Python package `nabu-llm`: LoRA fine-tuning of the translation model with
[mlx-lm](https://github.com/ml-explore/mlx-lm) (MIT) on Apple Silicon, and an
OpenAI-compatible server for the result. Base model:
`mlx-community/Qwen3-4B-Instruct-2507-4bit` (Apache-2.0, ~2.3 GB, pinned
revision in `configs/`).

```sh
make corpus rag-index        # corpus + vector index (see ../README.md)
make backfill-en             # optional: synthetic English inputs (~1 day, resumable)
make llm-data                # nabu-translate export-sft → $NABU_CHECKPOINT_DIR/llm/data
make llm-train CONFIG=smoke  # a few minutes: checks the pipeline
make llm-train CONFIG=full   # hours
make llm-serve CONFIG=full   # http://127.0.0.1:8090/v1
make run-api LLM_URL=http://127.0.0.1:8090/v1 LLM_MODEL=mlx-community/Qwen3-4B-Instruct-2507-4bit
```

## Data

`nabu-translate export-sft` (Go) writes chat-format `train.jsonl`,
`valid.jsonl` and `test.jsonl`, plus `*-meta.jsonl` for evaluation. The prompts
come from the same code as at inference (`Translator.Prompt`), with retrieval:
glossary and example sentences from the vector index and BM25. This way the
model learns to use retrieved material (RAFT-style), and training and serving
prompts cannot drift apart.

- **Inputs:** the editors' German translation, and the synthetic English
  (`backfill-en`, marked `machine_input`) where it exists.
- **Target:** the transliteration, editor's brackets removed. Only this
  assistant turn is trained on (`mask_prompt`).
- **No leakage:** splits are by text (`SplitOf(group)`, 90/5/5). Retrieval for
  a sentence never returns its own text or any sentence with the same
  transliteration.
- Only sentences usable as Middle Egyptian: stage "earlier", or AES texts of
  unknown stage.

## Storage and licenses

Model downloads go to `HF_HOME` (on the SSD with `MODELS_ROOT`; training
refuses to start without it), adapters to `$NABU_CHECKPOINT_DIR/llm/<run>`, and
logs and the mlx-lm config to `$NABU_RUNS_DIR/llm/<run>`. The training data and
the adapters are derived from CC BY-SA 4.0 corpora: they stay local (see
[DATA_LICENSES.md](../../DATA_LICENSES.md)).

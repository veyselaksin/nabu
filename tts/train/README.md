# nabu-tts (training)

Offline data preparation, training and ONNX export for Nabu's voice: a
**multi-speaker VITS model that reads IPA**. Nothing here runs at request
time. The API synthesizes in Go (`tts/`, ONNX Runtime). Python is used only
in this directory.

> [!IMPORTANT]
> The voice learns to pronounce IPA symbols from recordings of **modern**
> languages ("phonetic donors"). It reads a scholarly reconstruction with a
> modern speaker's voice and accent. It is not, and cannot be, a recording of
> an ancient language.

## Components and licenses

Every component is OSI-licensed and allows commercial use. No GPL tools
(espeak-ng included) and no NC weights. No pretrained TTS weights are used:
the model is trained from scratch.

| Piece | Choice | License |
|---|---|---|
| Model | VITS, adapted from [jaywalnut310/vits](https://github.com/jaywalnut310/vits) @ `2e561ba` in `src/nabu_tts/vits/` (see `vits/__init__.py` for changes) | MIT |
| Training audio | [Mozilla Common Voice](https://commonvoice.mozilla.org/) | CC0-1.0 |
| Orthography → IPA labels | [Epitran](https://github.com/dmort27/epitran) | MIT |
| Audio decoding | [pyminiaudio](https://github.com/irmen/pyminiaudio) (not ffmpeg/libsndfile, which are LGPL/GPL) | MIT |
| Resampling | SciPy | BSD-3-Clause |
| Training | PyTorch, numba | BSD-3-Clause, BSD-2-Clause |
| Experiment tracking | `mlflow-skinny` + SQLAlchemy/Alembic (sqlite store) | Apache-2.0, MIT |
| Export / parity check | ONNX, onnxscript, ONNX Runtime | Apache-2.0, MIT, MIT |

`uv.lock` pins PyTorch's **CPU-only** wheels on Linux, because the default PyPI
Linux wheels pull in NVIDIA CUDA libraries under NVIDIA's proprietary license.
See [GPU training and licenses](#gpu-training-and-licenses).

## Setup

```sh
uv sync --all-packages --all-extras    # or: make py-train
```

The base package (`nabu-tts`) only has the symbol table and tokenizer. The
`data` extra adds Epitran, pyminiaudio and SciPy. The `train` extra adds
PyTorch, numba, MLflow and ONNX.

## The shared symbol table: `tts/symbols.txt`

The model's input tokens are IPA code points. `tts/symbols.txt` lists them,
and both this package and the Go runtime (which embeds the file) read it, so
the two cannot drift apart:

- tokenization is defined once (NFD, collapse whitespace, one id per code
  point, error on anything unknown) and checked against shared vectors in
  `tts/testdata/tokenize.jsonl` from both Go and Python tests;
- checkpoints and exported models record the table's SHA-256. Training refuses
  to resume, export refuses to export and the runtime refuses to load when it
  doesn't match.

The file is **append-only**. Appending changes the hash, so models must be
retrained to use new symbols.

## 1. Training data: Common Voice → manifest

Nabu does not download Common Voice. Get each locale from Mozilla (the
download page asks you to accept its terms, including not trying to identify
speakers) and extract it, e.g. to `~/cv/cv-corpus-XX/<locale>/` with
`validated.tsv` and `clips/`.

### Choose donors: the coverage report

Candidate donor locales live in `configs/donors.yaml`. Each Epitran code there
was verified to exist in Epitran 1.35.3, and each locale was checked against
the Common Voice languages list. The list is *candidates*, not a decision.

```sh
# No data needed: which candidates' Epitran map tables can output each symbol
uv run nabu-tts-coverage --epitran-maps

# Real counts from Common Voice text (vote-filtered, converted with Epitran)
uv run nabu-tts-coverage --cv kab=~/cv/cv-corpus-XX/kab mt=~/cv/cv-corpus-XX/mt \
    --out runs/coverage.md --json runs/coverage.json

# Final check on prepared training data
uv run nabu-tts-coverage --manifest runs/data/*/manifest.jsonl
```

The report lists every IPA code point that `g2p/rules/egy-allen.yaml` can emit
(and multi-symbol outputs such as affricates, counted as substrings), with
counts per language. Symbols with zero total are flagged **ZERO**, and those
under `--low` (default 200) are flagged **LOW**. It also shows:

- rule outputs missing from `tts/symbols.txt`, which is an error (exit 1);
- Epitran output missing from the table, counted per language;
- the dominant script of each locale's text against the Epitran map's script;
- Arabic vowel marks per Arabic letter, a measure of the problem below.

> While `egy-allen.yaml` is a placeholder, the report only covers its toy
> symbols. Re-run it once the rules are sourced.

### The unvocalized-script problem

Some candidate scripts are **abjads**: everyday text leaves short vowels
unwritten (e.g. Arabic in Common Voice). Epitran converts what is written, so
the IPA labels would lack vowels that the speakers clearly pronounce, and the
model would learn wrong alignments. `donors.yaml` marks these `abjad: true`.
`nabu-tts-prepare` **refuses them** unless `--allow-abjad` is passed. No
option below is implemented yet; this is a TODO(decision).

| Option | Tools (license) | Trade-off |
|---|---|---|
| A. Exclude abjad donors (current default) | none | Simplest and cleanest labels. Loses donors that may be the main source of some symbols (check the coverage report). |
| B. Keep only fully vocalized sentences | none (filter on vowel-mark density) | Clean labels, possibly very little data. Depends on how much vocalized text a release contains. |
| C. Automatic diacritization before Epitran | [CAMeL Tools](https://github.com/CAMeL-Lab/camel_tools) (code MIT; its downloadable data/model packages have their own terms, to verify); [CATT](https://github.com/abjadai/catt) (code Apache-2.0; verify the weights' license) | More data, but diacritizer errors become label noise. Arabic only. Excluded: Mishkal (GPL-3.0); Shakkala (no license file found, so all rights reserved). |
| D. Pronunciation lexicon lookup | [WikiPron](https://github.com/CUNY-CL/wikipron) (code Apache-2.0; scraped Wiktionary data is **CC BY-SA**, share-alike) | Vowelled pronunciations for in-lexicon words only, and the share-alike terms need a decision on derived datasets/models. |
| E. Label audio with a phoneme recognizer | Allosaurus is GPL-3.0 (excluded); other recognizers need case-by-case license review, and some were trained on espeak-ng output | Avoids orthography entirely, but recognizer errors become labels. Conflicts with the "no espeak-ng, even for labels" rule if the recognizer learned from espeak-ng. |

Other candidates carry "verify" notes in `donors.yaml` (e.g. how fully certain
Arabic-based alphabets and abugidas write vowels or gemination). Someone who
knows those orthographies should check them before they are used.

### Prepare a locale

```sh
uv run nabu-tts-prepare --cv-dir ~/cv/cv-corpus-XX/kab --out runs/data/kab \
    --top-speakers 20 --max-clips-per-speaker 2000 --workers 8
```

Steps, each with its rejection count in `<out>/report.json`:

1. **votes:** `up_votes > down_votes` and `up_votes >= --min-up-votes` (2).
2. **text:** Epitran. Punctuation the map doesn't use as a letter becomes a
   space. Sentences with letters, marks or digits that Epitran leaves unmapped
   are rejected, as are sentences whose IPA has a code point missing from
   `tts/symbols.txt`. Nothing is dropped silently.
3. **speakers:** `--top-speakers N` keeps the N speakers with the most clips
   (a small set of consistent voices instead of thousands of noisy ones).
   `--max-clips-per-speaker` caps each one.
4. **audio:** decode, then reject `too_quiet` (peak < 0.05), `clipped`
   (> 0.1 % of samples at full scale) and `noisy` (percentile SNR estimate
   < 15 dB, a heuristic). Trim leading/trailing silence, keep
   `--min-duration` to `--max-duration` (1–12 s), resample to `--sample-rate`
   (22050) and write 16-bit mono WAV.

Output: `runs/data/<locale>/wavs/*.wav`, `manifest.jsonl` and `report.json`.
Manifest lines are JSON with `audio_path` (relative to the manifest),
`ipa`, `language`, `speaker_id` (a hash, not the Common Voice `client_id`),
`duration`, plus `text` and `source`.

## 2. Training

```sh
# Smoke test: synthetic data, ~300 steps on CPU, ~5 minutes on a laptop
make tts-smoke            # = nabu-tts-smoke-data + nabu-tts-train (smoke.yaml) + nabu-tts-export

# Full training on GPU, after listing your manifests in configs/full.yaml
uv run nabu-tts-train --config tts/train/configs/full.yaml
```

- **Smoke data** (`nabu-tts-smoke-data`) is *tones*, one pitch per IPA symbol,
  not speech. It proves that loading, MAS alignment, the GAN losses,
  checkpointing, MLflow, export and the Go runtime all work. The resulting
  model is useless for listening.
- **Configs** are YAML (`configs/smoke.yaml`, `configs/full.yaml`).
  `--set key=value` overrides a value, e.g. `--set train.max_steps=1000`.
  Unknown keys are errors.
- **Checkpoints:** `runs/<run>/checkpoints/step_XXXXXXXX.pt` plus `latest.pt`
  (the newest `keep_checkpoints` are kept). They hold generator, discriminator,
  optimizers, schedulers, RNG state, the speaker list, data statistics and the
  symbol-table hash.
- **Resuming:** re-run the same command. If `latest.pt` exists, training
  resumes into the same MLflow run (`--fresh` refuses instead). Ctrl-C saves
  a checkpoint first. A resumed run restarts its current epoch's batch order.
- **MLflow:** losses, gradient norms, learning rate, validation reconstruction
  mel L1, and validation audio samples as artifacts, all in `runs/mlflow.db`.
  View them with:
  `uvx mlflow ui --backend-store-uri sqlite:///runs/mlflow.db`
  (`uvx` runs the full MLflow UI as a throwaway tool. It is not a project
  dependency, and full `mlflow` pulls in matplotlib, whose license is
  PSF-style.)
- **Speakers:** one embedding per `speaker_id` across all manifests. The
  speaker list is fixed when a run starts; changing manifests mid-run is an error.

### GPU training and licenses

The lockfile's Linux torch build is CPU-only, which keeps the default install
free of NVIDIA's proprietary CUDA libraries. To train on a GPU, install a CUDA
build into the environment explicitly, e.g.

```sh
uv pip install --reinstall torch --index-url https://download.pytorch.org/whl/cu128
```

The CUDA runtime is under NVIDIA's license, not an OSI license. It is used
only on the training machine and never redistributed with Nabu.
TODO(decision): confirm this is acceptable under the project's license rules.

## 3. Export to ONNX and use it from the API

```sh
uv run nabu-tts-export --checkpoint runs/full/checkpoints/latest.pt --out runs/full/model.onnx
```

This writes:

- `model.onnx`, one file. Inputs: `ipa_token_ids` int64 [1, T],
  `speaker_id` int64 [1], `noise_scale`, `length_scale`, `noise_scale_w`
  float32 [1]. Output: `waveform` float32 [N]. Metadata keys `nabu.*` hold
  the sample rate, add_blank, speakers, languages, default scales and the
  symbol-table hash.
- A **parity check** runs the same inputs through PyTorch and ONNX Runtime with
  all noise at 0, at three input lengths. It fails if lengths differ or any
  sample differs by more than `--atol` (1e-3). The smoke model matches to about
  1e-8.
- `model.model-card.md`: [`MODEL_CARD.md`](MODEL_CARD.md) filled in with
  the training data. Complete its TODOs before publishing a model.

Point the API at it (the Go runtime needs ONNX Runtime 1.31.0. `make ort`
downloads it, checksum-verified, to `.ort/`):

```sh
make ort
NABU_TTS_MODEL=runs/full/model.onnx make run-api
curl -X POST localhost:8080/v1/speak -d '{"text":"ꜥnḫ","ruleset":"egy-allen"}' -o out.wav
```

Listen across checkpoints with `make tts-samples MODEL=... LABEL=...` (see
`eval/samples/README.md`).

**Never commit audio, manifests' WAVs, checkpoints or `.onnx` files.**
`runs/`, `*.wav`, `*.pt` and `*.onnx` are git-ignored.

## Layout

```
tts/train/
├── configs/            smoke.yaml, full.yaml, donors.yaml
├── MODEL_CARD.md       model card template (filled in by nabu-tts-export)
├── src/nabu_tts/
│   ├── symbols.py      tts/symbols.txt reader + tokenizer (mirrors tts/symbols.go)
│   ├── donors.py       donor candidates, Epitran conversion with explicit rejection
│   ├── commonvoice.py  validated.tsv reader, vote filter, speaker hashing
│   ├── audio.py        decode, resample, quality checks, trim, WAV I/O
│   ├── prepare.py      nabu-tts-prepare
│   ├── coverage.py     nabu-tts-coverage
│   ├── smoke_data.py   nabu-tts-smoke-data
│   ├── dataset.py      manifest dataset, collate, length buckets
│   ├── train.py        nabu-tts-train (checkpoints, resume, MLflow)
│   ├── export.py       nabu-tts-export (ONNX, metadata, parity, model card)
│   └── vits/           VITS adapted from jaywalnut310/vits (MIT)
└── tests/              pytest (training/export tests need the extras)
```

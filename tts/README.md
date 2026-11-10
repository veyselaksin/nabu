# tts: speech synthesis runtime (Go)

Package `tts` (module `github.com/veyselaksin/nabu/tts`) speaks IPA with a
model exported by [`train/`](train/README.md). It runs the ONNX model through
[onnxruntime_go](https://github.com/yalue/onnxruntime_go) (MIT) and
[ONNX Runtime](https://github.com/microsoft/onnxruntime) (MIT). No Python runs
at request time.

```
transliteration ──g2p──▶ IPA ──tts.SymbolTable──▶ token ids ──ONNX Runtime──▶ float32 samples ──EncodeWAV──▶ .wav
```

- `symbols.txt`: the IPA symbol table, embedded in the Go binary and read by
  the Python trainer. The format and tokenization rules are in the file's header.
- `symbols.go`: parser, tokenizer (`Tokenize` returns `*UnknownSymbolError` for
  any code point not in the table; nothing is dropped), `SHA256`.
- `model.go`: `Open` loads a model, checking its `nabu.*` metadata, its I/O
  signature and that its symbol-table hash matches the embedded table.
  `Synthesize` is safe for concurrent use and stops ONNX Runtime early if the
  context is cancelled.
- `wav.go`: `EncodeWAV`, 16-bit mono PCM in pure Go.
- `cmd/nabu-tts`: CLI for single utterances and for rendering
  `eval/testsets/tts-samples.tsv` into `eval/samples/`.

## ONNX Runtime

onnxruntime_go v1.38.0 is built against the ONNX Runtime **1.31.0** C API, so
load exactly that version. It needs cgo, and the shared library is opened at
run time:

```sh
make ort    # downloads to .ort/lib/, verifying the release's SHA-256
```

Set `NABU_ORT_LIB` to the library path (the Makefile targets and the Docker
image do this). Without it, the platform's default name is looked up on the
loader's search path.

## Tests

```sh
go test ./tts/...                         # tokenizer (shared vectors with Python), WAV, parsing
make tts-smoke && make test-tts-integration   # real model: needs the Python training extras
```

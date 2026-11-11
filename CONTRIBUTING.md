# Contributing to Nabu

Thanks for helping! Nabu is licensed under Apache-2.0 (see `LICENSE`), and
contributions are accepted under the same license.

## Development setup

Requirements: Go ≥ 1.24 with a C compiler (cgo, for the ONNX Runtime binding in
`tts/`), [uv](https://docs.astral.sh/uv/), Node.js ≥ 22. No GPU is needed.

```sh
uv sync                 # Python workspace (tts, eval) + ruff/pytest
make test               # go test + pytest
make lint               # gofmt, go vet, WASM build check, ruff, tsc
make run-api            # API on http://localhost:8080
make run-web            # web demo on http://localhost:5173 (proxies to the API)
make g2p && bin/nabu-g2p --rules g2p/rules/egy-allen.yaml "ꜥnḫ"
make script && bin/nabu-script "ꜥnḫ"
make lexicon            # optional: download TLA, build the local spelling lexicon

# Speech (optional): training extras, CPU smoke model, ONNX Runtime, Go integration test
make py-train tts-smoke ort test-tts-integration
```

`make help` lists every target.

## Ground rules

### 1. Linguistic claims need sources

- Every G2P rule must cite the published reconstruction it comes from: add a
  page or section reference in a YAML comment next to the rule. The file's
  `reference` field holds the full citation.
- Don't add mappings from memory, intuition, or "what sounds right". If you
  need a value you can't source yet, mark it:

  ```yaml
  # TODO(source): cite reference
  ```

  and make sure the rule file's `name`/`notes` say it's a placeholder.
- Find all open placeholders with `grep -rn "TODO(source)" .`
- Where scholars disagree, don't merge the views. Add a separate rule file
  (`egy-<author>.yaml`) for each reconstruction.

### 2. Dependency licenses

Every dependency (direct or transitive, in any language) must be under an
OSI-approved license that permits commercial use.

- **Allowed:** MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause, ISC, 0BSD,
  MPL-2.0, PostgreSQL, BlueOak-1.0.0, Zlib. For fonts only: OFL-1.1.
- **Data:** see [DATA_LICENSES.md](DATA_LICENSES.md). Unicode data files are
  Unicode-3.0. Share-alike corpus data (e.g. TLA, CC BY-SA 4.0) is never
  committed or embedded; only tools that build from it are.
- **Not allowed:** GPL/LGPL/AGPL, SSPL, any non-commercial (NC) license, and
  custom or "responsible AI" model licenses. Model weights and voices count as
  dependencies.
- If you're unsure, open an issue before adding the dependency.
- `make licenses` checks the npm tree. Check Go and Python dependencies by
  hand in review.

### 3. No corpus data, audio or model weights in git

Only scripts that *download and clean* data are committed. Downloads go to
`data/corpora/`, which is git-ignored. Record each source's terms in
`DATA_LICENSES.md`. Audio (`*.wav`, `*.mp3`), checkpoints (`*.pt`), exported
models (`*.onnx`) and training runs (`runs/`) are git-ignored too.

### 4. The TTS symbol table is append-only

`tts/symbols.txt` defines the voice model's input tokens for both the Python
trainer and the Go runtime. Only append to it; never reorder or delete lines.
Token ids are baked into trained models, and models record the table's hash.

## G2P rule files

The schema and matching semantics are documented in `g2p/rules/README.md`.
When you change a rule file:

1. Bump its `version`.
2. Run `make golden` and review the golden-file diff line by line.
3. Run `make test`.

## Pull requests

- Keep PRs focused. Include tests: table-driven for Go, pytest for Python.
- CI must pass: go vet/test, the WASM build, ruff, pytest, and the web build.

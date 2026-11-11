GO      ?= go
UV      ?= uv
NPM     ?= npm
GO_PKGS := ./g2p/... ./script/... ./data/... ./translate/... ./tts/... ./api/...
GO_DIRS := g2p script data translate tts api
RULES   ?= g2p/rules/egy-allen.yaml
# ONNX Runtime shared library for the Go TTS runtime. Recipes that need it
# fetch it on demand (scripts/fetch-ort.sh prints the path); set NABU_ORT_LIB
# to use your own copy of ONNX Runtime 1.31.0.
ORT_LIB = $${NABU_ORT_LIB:-$$(scripts/fetch-ort.sh .ort)}
MODEL   ?= runs/smoke/model.onnx
LABEL   ?=
# Optional TLA spelling lexicon (CC BY-SA 4.0, built locally by `make lexicon`).
LEXICON ?= data/corpora/tla/lexicon-egy.tsv
# Translation: an OpenAI-compatible server (default: Ollama, MIT) running an
# Apache-2.0 model, plus the TLA corpus for retrieval (make data-fetch).
TLA       ?= data/corpora/tla/train.jsonl
LLM_URL   ?= http://127.0.0.1:11434/v1
LLM_MODEL ?= qwen3:4b-instruct

.DEFAULT_GOAL := help
.PHONY: help test test-go test-py lint lint-go lint-py lint-web fmt build-web \
        run-api run-web g2p wasm licenses golden clean ort py-train \
        tts-smoke tts-smoke-data tts-smoke-train tts-export tts-samples test-tts-integration \
        script data-fetch signs lexicon translate llm-pull run-llm

help: ## Show this help
	@awk 'BEGIN{FS=":.*## "} /^[a-z-]+:.*## /{printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: test-go test-py ## Run Go and Python tests

test-go:
	$(GO) test $(GO_PKGS)

test-py:
	$(UV) run pytest

lint: lint-go lint-py lint-web ## gofmt + go vet + WASM build, ruff, tsc

lint-go: wasm
	@out=$$(gofmt -l $(GO_DIRS)); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	$(GO) vet $(GO_PKGS)

lint-py:
	$(UV) run ruff check .
	$(UV) run ruff format --check .

lint-web: web/node_modules
	cd web && $(NPM) run typecheck

fmt: ## Format Go and Python code
	gofmt -w $(GO_DIRS)
	$(UV) run ruff format .
	$(UV) run ruff check --fix .

wasm: ## Check that the g2p and script packages build for GOOS=js GOARCH=wasm
	GOOS=js GOARCH=wasm $(GO) build ./g2p/... ./script/...

build-web: web/node_modules ## Production build of the web demo
	cd web && $(NPM) run build

# An empty NABU_* variable in the environment counts as unset. To run without
# speech, lexicon or translation, clear the make variable instead:
# make run-api MODEL= LEXICON= LLM_URL=
# Translation is enabled when $(TLA) exists; start the model with make run-llm.
run-api: ## Run the API on :8080 (uses $(LEXICON), $(MODEL), $(TLA) + $(LLM_URL) if present)
	NABU_LEXICON=$${NABU_LEXICON:-$(if $(wildcard $(LEXICON)),$(LEXICON))} \
	NABU_TTS_MODEL=$${NABU_TTS_MODEL:-$(if $(wildcard $(MODEL)),$(MODEL))} \
	NABU_TLA_CORPUS=$${NABU_TLA_CORPUS:-$(if $(wildcard $(TLA)),$(TLA))} \
	NABU_LLM_URL=$${NABU_LLM_URL:-$(if $(and $(LLM_URL),$(wildcard $(TLA))),$(LLM_URL))} \
	NABU_LLM_MODEL=$${NABU_LLM_MODEL:-$(LLM_MODEL)} \
	NABU_ORT_LIB=$${NABU_ORT_LIB:-$(abspath $(firstword $(wildcard .ort/lib/libonnxruntime*)))} \
	  $(GO) run ./api/cmd/nabu-api

run-web: web/node_modules ## Run the web demo on :5173 (proxies /v1 to :8080)
	cd web && $(NPM) run dev

g2p: ## Build the CLI to bin/nabu-g2p (try: make g2p && bin/nabu-g2p --rules $(RULES) ꜥnḫ)
	$(GO) build -o bin/nabu-g2p ./g2p/cmd/nabu-g2p

script: ## Build the CLI to bin/nabu-script (try: bin/nabu-script ꜥnḫ)
	$(GO) build -o bin/nabu-script ./script/cmd/nabu-script

translate: ## Build the CLI to bin/nabu-translate (try: bin/nabu-translate --search König)
	$(GO) build -o bin/nabu-translate ./translate/cmd/nabu-translate

llm-pull: ## Download $(LLM_MODEL) into Ollama's model store (qwen3:4b-instruct is ~2.5 GB, Apache-2.0)
	@command -v ollama >/dev/null || { echo "Ollama (MIT) is not installed: https://ollama.com/download"; exit 1; }
	ollama pull $(LLM_MODEL)

run-llm: ## Serve models with Ollama on :11434 (run make llm-pull once first)
	@command -v ollama >/dev/null || { echo "Ollama (MIT) is not installed: https://ollama.com/download"; exit 1; }
	ollama serve

data-fetch: ## Download Unikemet + TLA (pinned, checksum-verified) into data/corpora/
	$(GO) run ./data/cmd/nabu-data fetch unikemet tla

signs: data-fetch ## Rebuild script/signs/egy-unikemet-uniliterals.tsv from Unikemet + TLA (review the diff!)
	$(GO) run ./data/cmd/nabu-data build-signs

lexicon: data-fetch ## Build the TLA spelling lexicon into $(LEXICON) (CC BY-SA 4.0; never commit it)
	$(GO) run ./data/cmd/nabu-data build-lexicon --out $(LEXICON)

golden: ## Regenerate g2p golden files after an intentional output change (review the diff!)
	$(GO) test ./g2p/... -run 'TestGolden|TestRun' -update
	$(GO) test ./script/... -run 'TestRun' -update

licenses: web/node_modules ## Check npm dependency licenses against the allowlist
	cd web && $(NPM) run licenses

web/node_modules: web/package-lock.json
	cd web && $(NPM) ci
	@touch web/node_modules

ort: ## Download the pinned ONNX Runtime library (MIT, checksum-verified) into .ort/
	scripts/fetch-ort.sh .ort

py-train: ## Install the Python training extras (torch, epitran, mlflow, onnx, ...)
	$(UV) sync --all-packages --all-extras

tts-smoke: tts-smoke-data tts-smoke-train tts-export ## CPU smoke run: synthetic data → train → ONNX (minutes)

tts-smoke-data:
	$(UV) run nabu-tts-smoke-data --out runs/smoke-data

tts-smoke-train:
	$(UV) run nabu-tts-train --config tts/train/configs/smoke.yaml

tts-export: ## Export runs/smoke (or CKPT=..., MODEL=...) to ONNX with a parity check
	$(UV) run nabu-tts-export --checkpoint $${CKPT:-runs/smoke/checkpoints/latest.pt} --out $(MODEL)

tts-samples: ## Render eval/testsets/tts-samples.tsv with MODEL into eval/samples/[LABEL]
	$(GO) run ./tts/cmd/nabu-tts --model $(MODEL) --ort-lib "$(ORT_LIB)" \
	  --samples eval/testsets/tts-samples.tsv --out-dir eval/samples $(if $(LABEL),--label $(LABEL))

test-tts-integration: ## Go integration tests against MODEL (default runs/smoke/model.onnx)
	lib="$(ORT_LIB)" && NABU_TTS_MODEL=$(abspath $(MODEL)) NABU_ORT_LIB="$$(cd "$$(dirname "$$lib")" && pwd)/$$(basename "$$lib")" \
	  $(GO) test -count=1 -run Integration -v ./tts/

clean:
	rm -rf bin web/dist

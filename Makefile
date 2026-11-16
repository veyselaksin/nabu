GO      ?= go
UV      ?= uv
NPM     ?= npm
GO_PKGS := ./g2p/... ./script/... ./data/... ./translate/... ./tts/... ./api/...
GO_DIRS := g2p script data translate tts api
RULES   ?= g2p/rules/egy-allen.yaml

# Heavy artifacts (datasets, checkpoints, training runs) are never committed.
# With MODELS_ROOT set (a shared model drive) they live under
# $(MODELS_ROOT)/projects/nabu, the ML caches under $(MODELS_ROOT)/cache and
# uv-managed Pythons under $(MODELS_ROOT)/apps/uv-python
# (the same layout as models-env.zsh);
# otherwise in the git-ignored data/corpora/ and runs/. NABU_HOME,
# NABU_DATA_DIR, NABU_RUNS_DIR and NABU_CHECKPOINT_DIR override each part.
# The Go and Python tools resolve the same defaults (data/source.Root,
# nabu_tts/paths.py). Paths may contain spaces: keep them quoted in recipes.
ifneq ($(MODELS_ROOT),)
ifeq ($(shell [ -d "$(MODELS_ROOT)" ] && echo ok),)
$(error MODELS_ROOT="$(MODELS_ROOT)" does not exist (drive not mounted?). Mount it, or unset MODELS_ROOT)
endif
NABU_HOME ?= $(MODELS_ROOT)/projects/nabu
export HF_HOME       := $(MODELS_ROOT)/cache/hf
export TORCH_HOME    := $(MODELS_ROOT)/cache/torch
export PIP_CACHE_DIR := $(MODELS_ROOT)/cache/pip
export UV_CACHE_DIR  := $(MODELS_ROOT)/cache/uv
export UV_PYTHON_INSTALL_DIR := $(MODELS_ROOT)/apps/uv-python
export OLLAMA_MODELS := $(MODELS_ROOT)/ollama
export PYTORCH_ENABLE_MPS_FALLBACK := 1
endif
ifneq ($(NABU_HOME),)
NABU_DATA_DIR       ?= $(NABU_HOME)/datasets
NABU_RUNS_DIR       ?= $(NABU_HOME)/training-runs
NABU_CHECKPOINT_DIR ?= $(NABU_HOME)/checkpoints
endif
NABU_DATA_DIR       ?= data/corpora
NABU_RUNS_DIR       ?= runs
NABU_CHECKPOINT_DIR ?= runs/checkpoints
export NABU_DATA_DIR NABU_RUNS_DIR NABU_CHECKPOINT_DIR
SMOKE_CKPT := $(NABU_CHECKPOINT_DIR)/vits/smoke
# ONNX Runtime shared library for the Go TTS runtime. Recipes that need it
# fetch it on demand (scripts/fetch-ort.sh prints the path); set NABU_ORT_LIB
# to use your own copy of ONNX Runtime 1.31.0.
ORT_LIB = $${NABU_ORT_LIB:-$$(scripts/fetch-ort.sh .ort)}
MODEL   ?= $(SMOKE_CKPT)/model.onnx
LABEL   ?=
# Optional TLA spelling lexicon (CC BY-SA 4.0, built locally by `make lexicon`).
LEXICON ?= $(NABU_DATA_DIR)/tla/lexicon-egy.tsv
# Translation: an OpenAI-compatible server (default: Ollama, MIT) running an
# Apache-2.0 model, plus the TLA corpus for retrieval (make data-fetch).
TLA       ?= $(NABU_DATA_DIR)/tla/train.jsonl
LLM_URL   ?= http://127.0.0.1:11434/v1
LLM_MODEL ?= qwen3:4b-instruct
# Retrieval by meaning: the merged corpus (make corpus) embedded with an
# Apache-2.0 embedding model (make embed-pull, make rag-index). CC BY-SA 4.0
# derived data: stays under $(NABU_DATA_DIR), never committed.
CORPUS      ?= $(NABU_DATA_DIR)/corpus/egy
RAG_INDEX   ?= $(NABU_DATA_DIR)/rag/egy
EMBED_MODEL ?= qwen3-embedding:0.6b

.DEFAULT_GOAL := help
.PHONY: help test test-go test-py lint lint-go lint-py lint-web fmt build-web \
        run-api run-web g2p wasm licenses golden clean ort py-train \
        tts-smoke tts-smoke-data tts-smoke-train tts-export tts-samples test-tts-integration \
        script data-fetch signs lexicon translate llm-pull run-llm paths \
        corpus-fetch corpus embed-pull rag-index backfill-en llm-data llm-train llm-serve llm-eval

help: ## Show this help
	@awk 'BEGIN{FS=":.*## "} /^[a-z-]+:.*## /{printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

paths: ## Show where datasets, checkpoints, training runs and ML caches go
	@echo "NABU_DATA_DIR=$(NABU_DATA_DIR)"
	@echo "CORPUS=$(CORPUS)"
	@echo "RAG_INDEX=$(RAG_INDEX)"
	@echo "NABU_RUNS_DIR=$(NABU_RUNS_DIR)"
	@echo "NABU_CHECKPOINT_DIR=$(NABU_CHECKPOINT_DIR)"
	@echo "HF_HOME=$${HF_HOME:-(unset)}"
	@echo "TORCH_HOME=$${TORCH_HOME:-(unset)}"
	@echo "UV_CACHE_DIR=$${UV_CACHE_DIR:-(unset)}"
	@echo "UV_PYTHON_INSTALL_DIR=$${UV_PYTHON_INSTALL_DIR:-(unset)}"
	@echo "OLLAMA_MODELS=$${OLLAMA_MODELS:-(unset)}"

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
# (Shell tests rather than $(wildcard): the paths may contain spaces.)
run-api: ## Run the API on :8080 (uses $(LEXICON), $(MODEL), $(TLA) + $(LLM_URL) if present)
	@have() { [ -n "$$1" ] && [ -f "$$1" ] && printf '%s' "$$1"; }; \
	tla="$$(have "$(TLA)")"; \
	NABU_LEXICON="$${NABU_LEXICON:-$$(have "$(LEXICON)")}" \
	NABU_TTS_MODEL="$${NABU_TTS_MODEL:-$$(have "$(MODEL)")}" \
	NABU_TLA_CORPUS="$${NABU_TLA_CORPUS:-$$tla}" \
	NABU_LLM_URL="$${NABU_LLM_URL:-$${tla:+$(LLM_URL)}}" \
	NABU_LLM_MODEL="$${NABU_LLM_MODEL:-$(LLM_MODEL)}" \
	NABU_RAG_INDEX="$${NABU_RAG_INDEX:-$$([ -f "$(RAG_INDEX)/manifest.json" ] && printf '%s' "$(RAG_INDEX)")}" \
	NABU_EMBED_MODEL="$${NABU_EMBED_MODEL:-$(EMBED_MODEL)}" \
	NABU_ORT_LIB="$${NABU_ORT_LIB:-$$(ls "$$PWD"/.ort/lib/libonnxruntime* 2>/dev/null | head -n 1)}" \
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

data-fetch: ## Download Unikemet + TLA (pinned, checksum-verified) into $(NABU_DATA_DIR)/
	$(GO) run ./data/cmd/nabu-data fetch unikemet tla

signs: data-fetch ## Rebuild script/signs/egy-unikemet-uniliterals.tsv from Unikemet + TLA (review the diff!)
	$(GO) run ./data/cmd/nabu-data build-signs

lexicon: data-fetch ## Build the TLA spelling lexicon into $(LEXICON) (CC BY-SA 4.0; never commit it)
	$(GO) run ./data/cmd/nabu-data build-lexicon --out "$(LEXICON)"

corpus-fetch: ## Download every Egyptian corpus source (TLA x3, AES, AED, Coptic lexicon; ~400 MB, CC BY-SA 4.0)
	$(GO) run ./data/cmd/nabu-data fetch tla tla-late tla-demotic aes aed coptic-dict wiktionary-egy oraec-coptic-etymologies

corpus: corpus-fetch ## Merge all sources into $(CORPUS)/{sentences,lexicon}.jsonl (CC BY-SA 4.0; never commit it)
	$(GO) run ./data/cmd/nabu-data build-corpus --out "$(CORPUS)"

embed-pull: ## Download $(EMBED_MODEL) into Ollama's model store (~640 MB, Apache-2.0)
	@command -v ollama >/dev/null || { echo "Ollama (MIT) is not installed: https://ollama.com/download"; exit 1; }
	ollama pull $(EMBED_MODEL)

rag-index: ## Embed $(CORPUS) into the vector index $(RAG_INDEX) (needs Ollama + make embed-pull; resumable)
	$(GO) run ./translate/cmd/nabu-translate index --corpus "$(CORPUS)" --out "$(RAG_INDEX)" \
	  --embed-url "$(LLM_URL)" --embed-model "$(EMBED_MODEL)"

backfill-en: ## Machine-translate the corpus's German into English with $(LLM_MODEL) (~1 day; resumable; LIMIT=n)
	$(GO) run ./translate/cmd/nabu-translate backfill-en --corpus "$(CORPUS)" \
	  --llm-url "$(LLM_URL)" --model "$(LLM_MODEL)" --limit "$(or $(LIMIT),0)"

# Fine-tuning (translate/train, mlx-lm, Apple Silicon). CONFIG=smoke|full.
CONFIG   ?= smoke
LLM_DATA ?= $(NABU_CHECKPOINT_DIR)/llm/data

llm-data: ## Export fine-tuning data with retrieval prompts into $(LLM_DATA) (needs rag-index + Ollama)
	$(GO) run ./translate/cmd/nabu-translate export-sft --corpus "$(CORPUS)" --tla "$(TLA)" \
	  --rag "$(RAG_INDEX)" --embed-url "$(LLM_URL)" --out "$(LLM_DATA)"

llm-train: ## LoRA fine-tune with translate/train/configs/$(CONFIG).yaml (base model ~2.3 GB into HF_HOME)
	$(UV) run --frozen nabu-llm-train --config translate/train/configs/$(CONFIG).yaml

# make llm-eval [RAG=none] [EVAL_LANG=English] [LIMIT=200] [LLM_URL=... LLM_MODEL=...]
llm-eval: ## Score translations of held-out test sentences (chrF); report into $(NABU_RUNS_DIR)/llm/
	$(GO) run ./translate/cmd/nabu-translate eval --data "$(LLM_DATA)" --tla "$(TLA)" \
	  --rag "$(or $(RAG),$(RAG_INDEX))" --llm-url "$(LLM_URL)" --model "$(LLM_MODEL)" \
	  --lang "$(EVAL_LANG)" --limit "$(or $(LIMIT),200)"

llm-serve: ## Serve the base model + $(CONFIG) adapters on :8090 (OpenAI-compatible)
	$(UV) run --frozen nabu-llm-serve --config translate/train/configs/$(CONFIG).yaml

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
	$(UV) run nabu-tts-smoke-data --out "$(NABU_DATA_DIR)/tts/smoke"

tts-smoke-train:
	$(UV) run nabu-tts-train --config tts/train/configs/smoke.yaml

tts-export: ## Export the smoke checkpoint (or CKPT=..., MODEL=...) to ONNX with a parity check
	$(UV) run nabu-tts-export --checkpoint "$${CKPT:-$(SMOKE_CKPT)/latest.pt}" --out "$(MODEL)"

tts-samples: ## Render eval/testsets/tts-samples.tsv with MODEL into eval/samples/[LABEL]
	$(GO) run ./tts/cmd/nabu-tts --model "$(MODEL)" --ort-lib "$(ORT_LIB)" \
	  --samples eval/testsets/tts-samples.tsv --out-dir eval/samples $(if $(LABEL),--label $(LABEL))

test-tts-integration: ## Go integration tests against MODEL (default: the smoke checkpoint's model.onnx)
	abs() { printf '%s/%s' "$$(cd "$$(dirname "$$1")" && pwd)" "$$(basename "$$1")"; }; \
	lib="$(ORT_LIB)" && NABU_TTS_MODEL="$$(abs "$(MODEL)")" NABU_ORT_LIB="$$(abs "$$lib")" \
	  $(GO) test -count=1 -run Integration -v ./tts/

clean:
	rm -rf bin web/dist

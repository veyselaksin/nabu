GO      ?= go
UV      ?= uv
NPM     ?= npm
GO_PKGS := ./g2p/... ./api/...
RULES   ?= g2p/rules/egy-allen.yaml

.DEFAULT_GOAL := help
.PHONY: help test test-go test-py lint lint-go lint-py lint-web fmt build-web \
        run-api run-web g2p wasm licenses golden clean

help: ## Show this help
	@awk 'BEGIN{FS=":.*## "} /^[a-z-]+:.*## /{printf "  %-10s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: test-go test-py ## Run Go and Python tests

test-go:
	$(GO) test $(GO_PKGS)

test-py:
	$(UV) run pytest

lint: lint-go lint-py lint-web ## gofmt + go vet + WASM build, ruff, tsc

lint-go: wasm
	@out=$$(gofmt -l g2p api); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	$(GO) vet $(GO_PKGS)

lint-py:
	$(UV) run ruff check .
	$(UV) run ruff format --check .

lint-web: web/node_modules
	cd web && $(NPM) run typecheck

fmt: ## Format Go and Python code
	gofmt -w g2p api
	$(UV) run ruff format .
	$(UV) run ruff check --fix .

wasm: ## Check that the g2p package builds for GOOS=js GOARCH=wasm
	GOOS=js GOARCH=wasm $(GO) build ./g2p/...

build-web: web/node_modules ## Production build of the web demo
	cd web && $(NPM) run build

run-api: ## Run the API on :8080 (env: NABU_ADDR, NABU_RULES_DIR, NABU_LOG_LEVEL, ...)
	$(GO) run ./api/cmd/nabu-api

run-web: web/node_modules ## Run the web demo on :5173 (proxies /v1 to :8080)
	cd web && $(NPM) run dev

g2p: ## Build the CLI to bin/nabu-g2p (try: make g2p && bin/nabu-g2p --rules $(RULES) ꜥnḫ)
	$(GO) build -o bin/nabu-g2p ./g2p/cmd/nabu-g2p

golden: ## Regenerate g2p golden files after an intentional output change (review the diff!)
	$(GO) test ./g2p/... -run 'TestGolden|TestRun' -update

licenses: web/node_modules ## Check npm dependency licenses against the allowlist
	cd web && $(NPM) run licenses

web/node_modules: web/package-lock.json
	cd web && $(NPM) ci
	@touch web/node_modules

clean:
	rm -rf bin web/dist

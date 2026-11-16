#!/usr/bin/env bash
# Start the whole Nabu stack on this machine, like `docker compose up` but
# native (Ollama uses the GPU on Apple Silicon, which it cannot inside Docker).
#
#   ./start.sh            prepare what is missing, then run ollama + api + web
#   ./start.sh --no-llm   skip Ollama (everything except /v1/translate works)
#
# Ctrl-C stops every service this script started.
#
# Steps, each skipped when already done:
#   1. ONNX Runtime library (.ort/, ~30 MB)          make ort
#   2. web dependencies (web/node_modules)           make web/node_modules
#   3. TLA corpus + spelling lexicon (~10 MB)         make lexicon
#   4. Ollama server on :11434                        ollama serve
#   5. the language model (LLM_MODEL, ~2.5 GB)        ollama pull
#      and, if the vector index exists (make corpus rag-index), the
#      embedding model it was built with (EMBED_MODEL, ~640 MB)
#   6. API on :8080, then the web demo on :5173       make run-api / run-web
#
# Where heavy files go: with MODELS_ROOT set (in the environment or in .env),
# datasets/checkpoints under $MODELS_ROOT/projects/nabu and Ollama models under
# $MODELS_ROOT/ollama; the script refuses to start if MODELS_ROOT is set but
# not mounted. Without MODELS_ROOT: data/corpora/, runs/ and Ollama's default.
#
# Overrides (environment or .env): MODELS_ROOT, LLM_MODEL, EMBED_MODEL, NABU_DATA_DIR,
# NABU_CHECKPOINT_DIR, OLLAMA_MODELS, NABU_SKIP_LLM=1.

set -euo pipefail
cd "$(dirname "$0")"

skip_llm="${NABU_SKIP_LLM:-}"
for arg in "$@"; do
	case "$arg" in
	--no-llm) skip_llm=1 ;;
	-h | --help)
		sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "unknown option: $arg (try --help)" >&2
		exit 2
		;;
	esac
done

# ── output ──────────────────────────────────────────────────────────────
if [ -t 1 ]; then
	bold=$'\033[1m' dim=$'\033[2m' red=$'\033[31m' green=$'\033[32m' yellow=$'\033[33m' reset=$'\033[0m'
else
	bold='' dim='' red='' green='' yellow='' reset=''
fi
step() { printf '%s==>%s %s\n' "$bold" "$reset" "$*"; }
ok() { printf '    %s✓%s %s\n' "$green" "$reset" "$*"; }
warn() { printf '    %s!%s %s\n' "$yellow" "$reset" "$*"; }
die() {
	printf '%serror:%s %s\n' "$red" "$reset" "$*" >&2
	exit 1
}

# ── configuration ───────────────────────────────────────────────────────
# .env holds KEY=VALUE lines (values may contain spaces, no quotes needed);
# variables already set in the environment win.
if [ -f .env ]; then
	while IFS= read -r line || [ -n "$line" ]; do
		case "$line" in '' | \#*) continue ;; esac
		key="${line%%=*}" value="${line#*=}"
		[[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue
		[ -n "${!key:-}" ] || export "$key=$value"
	done <.env
fi

if [ -n "${MODELS_ROOT:-}" ]; then
	[ -d "$MODELS_ROOT" ] || die "MODELS_ROOT=\"$MODELS_ROOT\" does not exist (drive not mounted?). Mount it, or unset MODELS_ROOT."
	export MODELS_ROOT
	export OLLAMA_MODELS="${OLLAMA_MODELS:-$MODELS_ROOT/ollama}"
fi
LLM_MODEL="${LLM_MODEL:-qwen3:4b-instruct}"
EMBED_MODEL="${EMBED_MODEL:-qwen3-embedding:0.6b}"
export LLM_MODEL EMBED_MODEL
OLLAMA_HOST_ADDR=127.0.0.1:11434
API_URL=http://127.0.0.1:8080
WEB_URL=http://localhost:5173

# All path logic lives in the Makefile; ask it instead of duplicating it.
make_var() { make -s paths | sed -n "s/^$1=//p"; }
data_dir="$(make_var NABU_DATA_DIR)"
ckpt_dir="$(make_var NABU_CHECKPOINT_DIR)"

need() { command -v "$1" >/dev/null || die "$1 is not installed ($2)"; }
need go "https://go.dev/dl/"
need npm "https://nodejs.org/"
need curl "brew install curl"
[ -n "$skip_llm" ] || need ollama "https://ollama.com/download, or run ./start.sh --no-llm"

port_busy() { lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1; }
for port in 8080 5173; do
	port_busy "$port" && die "port $port is already in use (another ./start.sh or make run-*?): lsof -iTCP:$port"
done

# ── services ────────────────────────────────────────────────────────────
# Each service runs in its own process group (job control on while forking),
# so stopping it also stops what it spawned (make → go run → nabu-api,
# npm → vite). Everything else stays in the script's group, so Ctrl-C
# reaches the script and its EXIT trap stops the services.
pids=()
names=()
cleanup() {
	trap - INT TERM EXIT
	[ ${#pids[@]} -gt 0 ] || exit 0
	echo
	step "stopping ${names[*]}"
	for pid in "${pids[@]}"; do kill -TERM -- "-$pid" 2>/dev/null || true; done
	wait 2>/dev/null || true
	ok "stopped"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# run NAME COLOR COMMAND...: start COMMAND with each output line prefixed.
run() {
	local name="$1" color="$2"
	shift 2
	local prefix
	prefix="$(printf '%s%-6s|%s ' "$color" "$name" "$reset")"
	set -m
	("$@" 2>&1 | while IFS= read -r l; do printf '%s%s\n' "$prefix" "$l"; done) &
	set +m
	pids+=("$!")
	names+=("$name")
}

# wait_http NAME URL SECONDS: wait until URL answers, fail if the service died.
wait_http() {
	local name="$1" url="$2" secs="$3" i pid="${pids[${#pids[@]}-1]}"
	for ((i = 0; i < secs; i++)); do
		curl -fsS -o /dev/null "$url" 2>/dev/null && return 0
		kill -0 "$pid" 2>/dev/null || die "$name exited during startup (see its output above)"
		sleep 1
	done
	die "$name did not answer at $url within ${secs}s"
}

# ── 1–3: one-time setup ─────────────────────────────────────────────────
step "checking prerequisites"
[ -n "${MODELS_ROOT:-}" ] && ok "heavy files under $MODELS_ROOT" || warn "MODELS_ROOT not set: using data/corpora/ and runs/ in the repository"

if ls .ort/lib/libonnxruntime* >/dev/null 2>&1; then ok "ONNX Runtime"; else
	step "downloading ONNX Runtime (~30 MB) into .ort/"
	make ort >/dev/null
	ok "ONNX Runtime"
fi

make -s web/node_modules
ok "web dependencies"

if [ -s "$data_dir/tla/train.jsonl" ] && [ -s "$data_dir/tla/lexicon-egy.tsv" ]; then
	ok "TLA corpus and lexicon in $data_dir/tla"
else
	step "downloading the TLA corpus and building the lexicon into $data_dir"
	make lexicon
fi

if [ -f "$ckpt_dir/vits/smoke/model.onnx" ]; then
	ok "voice $ckpt_dir/vits/smoke/model.onnx"
else
	warn "no voice at $ckpt_dir/vits/smoke/model.onnx: speech is off (make py-train tts-smoke builds one)"
fi

# ── 4–5: language model ────────────────────────────────────────────────
if [ -n "$skip_llm" ]; then
	warn "Ollama skipped (--no-llm): translation is off"
else
	export OLLAMA_HOST="$OLLAMA_HOST_ADDR"
	if curl -fsS -o /dev/null "http://$OLLAMA_HOST_ADDR/api/version" 2>/dev/null; then
		warn "Ollama is already running (not started by this script): models go wherever it was started with"
		warn "if that is the Ollama app or brew services, it ignores OLLAMA_MODELS; quit it to let ./start.sh run its own"
	else
		# Ollama keeps its key in ~/.ollama even with OLLAMA_MODELS set; a
		# symlink there to an unmounted or moved drive makes it fail obscurely.
		if [ -L "$HOME/.ollama" ] && [ ! -e "$HOME/.ollama" ]; then
			die "~/.ollama points to $(readlink "$HOME/.ollama"), which does not exist. Mount the drive or repoint it: ln -sfn \"${OLLAMA_MODELS:-<dir>}\" ~/.ollama"
		fi
		step "starting Ollama${OLLAMA_MODELS:+ (models in $OLLAMA_MODELS)}"
		[ -z "${OLLAMA_MODELS:-}" ] || mkdir -p "$OLLAMA_MODELS"
		run ollama "$dim" ollama serve
		wait_http ollama "http://$OLLAMA_HOST_ADDR/api/version" 30
		ok "Ollama on :11434"
	fi
	# `ollama list` names models as name:tag; a bare name means :latest.
	ensure_model() { # MODEL SIZE-NOTE
		local want="$1"
		[[ "$want" == *:* ]] || want="$want:latest"
		if ollama list | awk 'NR > 1 { print $1 }' | grep -qxF "$want"; then
			ok "model $1"
		else
			step "downloading $1 ($2)${OLLAMA_MODELS:+ into $OLLAMA_MODELS}"
			ollama pull "$1"
			ok "model $1"
		fi
	}
	ensure_model "$LLM_MODEL" "qwen3:4b-instruct is ~2.5 GB"
	rag_index="$(make_var RAG_INDEX)"
	if [ -f "$rag_index/manifest.json" ]; then
		ensure_model "$EMBED_MODEL" "qwen3-embedding:0.6b is ~640 MB"
		ok "vector index in $rag_index (retrieval by meaning)"
	else
		warn "no vector index in $rag_index: retrieval uses German keywords only (build it: make corpus embed-pull rag-index, ~1 h)"
	fi
fi

# ── 6: api + web ────────────────────────────────────────────────────────
step "starting the API"
run api "$green" make -s run-api
wait_http api "$API_URL/healthz" 180 # the first start compiles the server
ok "API on $API_URL"

step "starting the web demo"
run web "$yellow" make -s run-web
wait_http web "$WEB_URL" 60
ok "web on $WEB_URL"

echo
printf '%s  Nabu is up:%s  %s   (API %s)   Ctrl-C stops everything\n\n' "$bold" "$reset" "$WEB_URL" "$API_URL"

# Stay in the foreground until a service exits or Ctrl-C.
while :; do
	for i in "${!pids[@]}"; do
		kill -0 "${pids[$i]}" 2>/dev/null || die "${names[$i]} stopped (see its output above)"
	done
	sleep 2
done

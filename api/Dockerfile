# syntax=docker/dockerfile:1
# Build from the repository root (the Go workspace spans g2p/, script/, data/,
# translate/, tts/ and api/):
#   docker build -f api/Dockerfile -t nabu-api .
#
# Speech synthesis needs a model exported by nabu-tts-export. Models are not
# baked into the image (they are not committed to git); mount one:
#   docker run -p 8080:8080 -v "$NABU_CHECKPOINT_DIR/vits/egy:/models:ro" \
#     -e NABU_TTS_MODEL=/models/model.onnx nabu-api
# Without NABU_TTS_MODEL the API runs and POST /v1/speak returns 503.
#
# The hieroglyph sign list is embedded. The optional TLA spelling lexicon
# (CC BY-SA 4.0) is never baked in either; build it with `make lexicon` and
# mount it:  -v "$NABU_DATA_DIR/tla:/lexicon:ro" -e NABU_LEXICON=/lexicon/lexicon-egy.tsv
# ($NABU_DATA_DIR / $NABU_CHECKPOINT_DIR: see `make paths`; quote them, they may contain spaces.)
#
# Translation needs the TLA corpus (CC BY-SA 4.0, mounted the same way) and an
# OpenAI-compatible model server reachable from the container:
#   -v "$NABU_DATA_DIR/tla:/tla:ro" -e NABU_TLA_CORPUS=/tla/train.jsonl \
#   -e NABU_LLM_URL=http://host.docker.internal:11434/v1 -e NABU_LLM_MODEL=qwen3:4b-instruct
#
# docker-compose.yml at the repository root wires all of this up (Ollama, the
# model pull, the TLA download, the API and the web demo): docker compose up --build

FROM golang:1.24-bookworm AS build
WORKDIR /src
# cgo is required by github.com/yalue/onnxruntime_go (it dlopens ONNX Runtime).
ENV CGO_ENABLED=1 GOTOOLCHAIN=local
COPY scripts/fetch-ort.sh ./scripts/
# ONNX Runtime 1.31.0 (MIT), checksum-verified; must match onnxruntime_go v1.38.0.
RUN ./scripts/fetch-ort.sh /ort
COPY go.work ./
COPY g2p/go.mod g2p/go.sum ./g2p/
COPY script/go.mod script/go.sum ./script/
COPY data/go.mod data/go.sum ./data/
COPY translate/go.mod translate/go.sum ./translate/
COPY tts/go.mod tts/go.sum ./tts/
COPY api/go.mod api/go.sum ./api/
RUN go mod download
COPY g2p/ ./g2p/
COPY script/ ./script/
COPY data/ ./data/
COPY translate/ ./translate/
COPY tts/ ./tts/
COPY api/ ./api/
RUN go test ./g2p/... ./script/... ./data/... ./translate/... ./tts/... ./api/... \
 && go build -trimpath -ldflags="-s -w" -o /out/nabu-api ./api/cmd/nabu-api \
 && go build -trimpath -ldflags="-s -w" -o /out/nabu-data ./data/cmd/nabu-data \
 && mkdir -p /out/lib /out/licenses \
 && cp /ort/lib/libonnxruntime.so.* /out/lib/ \
 && cp /ort/LICENSE /out/licenses/onnxruntime-LICENSE \
 && cp /ort/ThirdPartyNotices.txt /out/licenses/onnxruntime-ThirdPartyNotices.txt

# nabu-data with a shell, for docker compose's one-shot `data` service that
# downloads TLA and builds the lexicon into the mounted NABU_DATA_DIR.
# Not the default target: docker build -f api/Dockerfile --target data .
FROM debian:bookworm-slim AS data
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/nabu-data /usr/local/bin/nabu-data
ENV NABU_DATA_DIR=/data/corpora
ENTRYPOINT ["nabu-data"]

# distroless/cc: glibc + libstdc++, which the ONNX Runtime shared library needs.
FROM gcr.io/distroless/cc-debian12:nonroot
COPY --from=build /out/nabu-api /nabu-api
COPY --from=build /out/lib/ /usr/lib/nabu/
COPY --from=build /out/licenses/ /usr/share/doc/nabu/
ENV NABU_ADDR=:8080 \
    NABU_ORT_LIB=/usr/lib/nabu/libonnxruntime.so.1.31.0
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/nabu-api"]

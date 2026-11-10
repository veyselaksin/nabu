# syntax=docker/dockerfile:1
# Build from the repository root (the Go workspace spans g2p/, tts/ and api/):
#   docker build -f api/Dockerfile -t nabu-api .
#
# Speech synthesis needs a model exported by nabu-tts-export. Models are not
# baked into the image (they are not committed to git); mount one:
#   docker run -p 8080:8080 -v "$PWD/runs/full:/models:ro" \
#     -e NABU_TTS_MODEL=/models/model.onnx nabu-api
# Without NABU_TTS_MODEL the API runs and POST /v1/speak returns 503.

FROM golang:1.24-bookworm AS build
WORKDIR /src
# cgo is required by github.com/yalue/onnxruntime_go (it dlopens ONNX Runtime).
ENV CGO_ENABLED=1 GOTOOLCHAIN=local
COPY scripts/fetch-ort.sh ./scripts/
# ONNX Runtime 1.31.0 (MIT), checksum-verified; must match onnxruntime_go v1.38.0.
RUN ./scripts/fetch-ort.sh /ort
COPY go.work ./
COPY g2p/go.mod g2p/go.sum ./g2p/
COPY tts/go.mod tts/go.sum ./tts/
COPY api/go.mod api/go.sum ./api/
RUN go mod download
COPY g2p/ ./g2p/
COPY tts/ ./tts/
COPY api/ ./api/
RUN go test ./g2p/... ./tts/... ./api/... \
 && go build -trimpath -ldflags="-s -w" -o /out/nabu-api ./api/cmd/nabu-api \
 && mkdir -p /out/lib /out/licenses \
 && cp /ort/lib/libonnxruntime.so.* /out/lib/ \
 && cp /ort/LICENSE /out/licenses/onnxruntime-LICENSE \
 && cp /ort/ThirdPartyNotices.txt /out/licenses/onnxruntime-ThirdPartyNotices.txt

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

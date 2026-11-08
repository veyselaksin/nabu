# syntax=docker/dockerfile:1
# Build from the repository root (the Go workspace spans g2p/ and api/):
#   docker build -f api/Dockerfile -t nabu-api .

FROM golang:1.24-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.work ./
COPY g2p/go.mod g2p/go.sum ./g2p/
COPY api/go.mod api/go.sum ./api/
RUN go mod download
COPY g2p/ ./g2p/
COPY api/ ./api/
RUN go test ./g2p/... ./api/... \
 && go build -trimpath -ldflags="-s -w" -o /out/nabu-api ./api/cmd/nabu-api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/nabu-api /nabu-api
ENV NABU_ADDR=:8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/nabu-api"]

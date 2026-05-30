# syntax=docker/dockerfile:1.7

# Debian + glibc instead of Alpine + musl because sqlite-vec's C source
# relies on glibc-style BSD typedefs (u_int16_t, u_int64_t) that musl lacks.
FROM golang:1.26-bookworm AS builder
WORKDIR /src
RUN apt-get update && \
    apt-get install -y --no-install-recommends git build-essential libsqlite3-dev && \
    rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/ingest ./cmd/ingest

FROM debian:bookworm-slim
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates tzdata libsqlite3-0 && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd -r app && useradd -r -g app app
COPY --from=builder /out/ingest /usr/local/bin/ingest
USER app
ENTRYPOINT ["/usr/local/bin/ingest"]

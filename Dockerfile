# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git gcc musl-dev sqlite-dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/ingest ./cmd/ingest

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata sqlite-libs && \
    addgroup -S app && adduser -S -G app app
COPY --from=builder /out/ingest /usr/local/bin/ingest
USER app
ENTRYPOINT ["/usr/local/bin/ingest"]

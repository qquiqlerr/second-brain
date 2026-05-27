.PHONY: all build test test-int test-prompts race lint mocks env-example tidy clean docker-build run

GO        ?= go
PKG       := ./...
COVERFILE := coverage.out

all: lint test race

build:
	$(GO) build -o bin/ingest ./cmd/ingest

test:
	$(GO) test -count=1 $(PKG)

test-int:
	$(GO) test -count=1 -tags=integration $(PKG)

test-prompts:
	$(GO) test -count=1 -tags=prompts -timeout=10m ./internal/adapter/out/openrouter/...

race:
	$(GO) test -race -count=1 $(PKG)

lint:
	golangci-lint run

mocks:
	mockery

env-example:
	$(GO) run ./cmd/envexample > .env.example

tidy:
	$(GO) mod tidy

clean:
	rm -rf bin/ dist/ $(COVERFILE)

docker-build:
	docker build -t second-brain/ingest:dev .

run:
	docker compose up --build

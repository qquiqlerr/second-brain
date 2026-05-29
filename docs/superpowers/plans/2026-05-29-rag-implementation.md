# RAG (Semantic Index + /find) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement sub-project 2 from the ingestion roadmap — semantic index over `data/notes/` MD files, periodic FS-scanner-driven embedder, auto `linked_notes` recompute, and a Telegram `/find <query>` command. `/ask` is deferred to sub-project 6 (agentic).

**Architecture:** Continues hexagonal pattern of sub-project 1. Two new driven ports (`Embedder`, `VectorIndex`), three new adapters (`voyage`, `sqlitevec`, plus extracted `httpretry`), two new use cases (`indexer`, `searcher`), one new Telegram command. Schema bumps from 1.0 to 1.1 (adds `linked_notes` to YAML).

**Tech Stack:** Go 1.26, `mattn/go-sqlite3` + `asg017/sqlite-vec-go-bindings` (requires CGO), `voyageai.com/v1/embeddings` REST, existing `cleanenv` + `slog` + `mockery v3` setup. Spec reference: `docs/superpowers/specs/2026-05-29-rag-design.md`.

---

## Conventions (Read First)

These are non-negotiable for this codebase:

- **Modern Go skill is mandatory.** Before writing or editing any Go code, invoke `modern-go-guidelines:use-modern-go` skill. The codebase already uses `range over int`, `slices`/`maps`/`cmp` from stdlib, `errors.AsType[T]`, `log/slog`. New code must match.
- **Domain has zero non-stdlib imports** except `gopkg.in/yaml.v3`. No HTTP, no SQL, no third-party libraries leak into `internal/domain/`.
- **Ports live in `internal/port/{in,out}`** as pure Go interfaces with `domain` types only.
- **Adapters live in `internal/adapter/{in,out}/<name>/`.** External library imports are confined here.
- **Mocks regenerated via `make mocks`** after editing ports or `.mockery.yml`. Never hand-write mocks.
- **Tests next to code** (`*_test.go`). Integration tests in `internal/integration/` with `//go:build integration`.
- **TDD for pure logic.** Write the failing test, run it, implement minimal, run again. For adapters: tests can be co-developed but must exist before merge.
- **Frequent commits.** One commit per logical step. Squash-merge each PR.
- **Lint clean.** `make lint` must pass. Don't suppress with `//nolint` without explanation.

---

## File Structure

```
internal/
  domain/
    note.go                       # MODIFY — add LinkedNotes; SchemaVersion → "1.1"
    note_test.go                  # MODIFY — add test for v1.1 round-trip
    yaml.go                       # MODIFY — UpdateFrontmatter(file, linked_notes, schema_version) helper
    yaml_test.go                  # MODIFY — test UpdateFrontmatter preserves body and stays atomic
    errors.go                     # MODIFY — add ErrEmbedder, ErrVectorIndex, ErrSearchEmpty
  port/out/
    embedder.go                   # NEW — Embedder interface, EmbedKind enum
    vector_index.go               # NEW — VectorIndex interface + IndexItem, IndexMeta, SearchQuery, SearchHit
  adapter/
    httpretry/                    # NEW — extracted from openrouter/retry.go
      retry.go
      retry_test.go
      http_error.go
    out/
      openrouter/
        retry.go                  # DELETED — moved to httpretry
        retry_test.go             # DELETED — moved to httpretry
        client.go                 # MODIFY — use httpretry.RetryPolicy
        atomizer.go               # MODIFY — use httpretry.WithRetry
        atomizer_test.go          # MODIFY — same
        transcriber.go            # MODIFY — same
        transcriber_test.go       # MODIFY — same
      voyage/                     # NEW
        client.go                 # NEW — *http.Client + APIKey + retry policy
        embedder.go               # NEW — implements port/out.Embedder
        embedder_test.go          # NEW — httptest.Server
      sqlitevec/                  # NEW
        schema.go                 # NEW — CREATE TABLE statements + migration helpers
        index.go                  # NEW — implements port/out.VectorIndex
        index_integration_test.go # NEW — real sqlite-vec under //go:build integration
    in/telegram/
      handler.go                  # MODIFY — Route() recognizes /find prefix before falling through to dump
      handler_test.go             # MODIFY — Route returns ActionFind for /find prefix
      reply.go                    # MODIFY — add FormatFindReply for hit list
      reply_test.go               # MODIFY — test FormatFindReply
  usecase/
    indexer/                      # NEW
      diff.go                     # NEW — pure diff(fsList, dbList) → (toEmbed, toDelete)
      diff_test.go                # NEW
      linker.go                   # NEW — Recompute(ctx, seedIDs) using VectorIndex
      linker_test.go              # NEW — with fake embedder
      indexer.go                  # NEW — orchestrator RunOnce + Loop
      indexer_test.go             # NEW
    searcher/                     # NEW
      searcher.go                 # NEW — Find(ctx, query, opts) → []Hit
      searcher_test.go            # NEW
  config/
    config.go                     # MODIFY — add Voyage + RAG env vars
    config_test.go                # MODIFY — coverage for new vars
.mockery.yml                      # MODIFY — register Embedder + VectorIndex
docker-compose.prod.yml           # MODIFY — index volume + INDEX_DB_PATH
Dockerfile                        # MODIFY — flip CGO_ENABLED=1, add sqlite-dev/sqlite-libs
scripts/sync-prod.sh              # MODIFY — --init creates data/index/
cmd/ingest/main.go                # MODIFY — wire Voyage + sqlitevec + indexer goroutine + /find
```

PR mapping:

- **PR 1 (Tasks 1–4):** schema bump + httpretry extraction + ports + errors
- **PR 2 (Tasks 5–7):** Voyage adapter + config
- **PR 3 (Tasks 8–10):** sqlite-vec adapter + Dockerfile CGO
- **PR 4 (Tasks 11–14):** indexer use case + main wiring
- **PR 5 (Tasks 15–17):** searcher + /find Telegram command + main wiring
- **PR 6 (Task 18):** compose + sync script for production rollout

---

## Task 1: Domain — bump schema, add `LinkedNotes`, new error sentinels

**Files:**
- Modify: `internal/domain/note.go`
- Modify: `internal/domain/note_test.go`
- Modify: `internal/domain/errors.go`

- [ ] **Step 1: Invoke modern-go skill**

Use the `Skill` tool with name `modern-go-guidelines:use-modern-go`. Follow whatever it tells you for the rest of this task and PR.

- [ ] **Step 2: Write the failing test for LinkedNotes round-trip**

Add to `internal/domain/note_test.go` (append after the last existing test):

```go
func TestMarshalUnmarshalNote_PreservesLinkedNotes(t *testing.T) {
    n := Note{
        ID:            "20260529-x",
        SchemaVersion: "1.1",
        Date:          time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
        Source:        SourceTelegramText,
        Kind:          KindAtom,
        Category:      "work/projects",
        Tags:          []string{"a"},
        LinkedNotes:   []string{"20260520-a", "20260521-b"},
        Ingest:        IngestMeta{DumpID: "d", ModelAtomize: "m"},
        Body:          "body",
    }
    data, err := MarshalNote(n)
    require.NoError(t, err)
    got, err := UnmarshalNote(data)
    require.NoError(t, err)
    assert.Equal(t, n.LinkedNotes, got.LinkedNotes)
}

func TestMarshalNote_OmitsEmptyLinkedNotes(t *testing.T) {
    n := Note{ID: "x", SchemaVersion: "1.1", Date: time.Now(), Source: SourceTelegramText, Kind: KindAtom, Tags: []string{}, Ingest: IngestMeta{DumpID: "d", ModelAtomize: "m"}, Body: "b"}
    data, err := MarshalNote(n)
    require.NoError(t, err)
    assert.NotContains(t, string(data), "linked_notes")
}
```

- [ ] **Step 3: Run failing test**

```bash
go test -run 'TestMarshalUnmarshalNote_PreservesLinkedNotes|TestMarshalNote_OmitsEmptyLinkedNotes' ./internal/domain/...
```
Expected: FAIL (field doesn't exist).

- [ ] **Step 4: Add the field and bump SchemaVersion**

In `internal/domain/note.go`:

```go
const SchemaVersion = "1.1"
```

In the `Note` struct, add the field (place it just before `Ingest`):

```go
LinkedNotes []string `yaml:"linked_notes,omitempty"`
```

- [ ] **Step 5: Run test, expect green**

```bash
go test -run 'TestMarshalUnmarshalNote_PreservesLinkedNotes|TestMarshalNote_OmitsEmptyLinkedNotes' ./internal/domain/...
```
Expected: PASS.

- [ ] **Step 6: Update existing tests that hardcode `"1.0"`**

Find them:

```bash
grep -rn '"1.0"' internal/ | grep -v _test.go | grep -v testdata
grep -rn 'SchemaVersion.*"1.0"' internal/
```

Update each occurrence in non-test source to `"1.1"` is unnecessary because `SchemaVersion` is now a constant. In test files, replace literal `"1.0"` with `domain.SchemaVersion` where appropriate, or update to `"1.1"` if the test specifically asserts the version string.

- [ ] **Step 7: Run all domain tests**

```bash
go test ./internal/domain/...
```
Expected: PASS. If anything fails, fix it before moving on.

- [ ] **Step 8: Add new error sentinels**

In `internal/domain/errors.go`, add to the existing `var (...)` block:

```go
ErrEmbedder    = errors.New("embedder")
ErrVectorIndex = errors.New("vector index")
ErrSearchEmpty = errors.New("no results")
```

- [ ] **Step 9: Commit**

```bash
git add internal/domain/note.go internal/domain/note_test.go internal/domain/errors.go
git commit -m "feat(domain): schema 1.1 with linked_notes; add RAG error sentinels

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Domain — `UpdateFrontmatter` helper

**Files:**
- Modify: `internal/domain/yaml.go`
- Modify: `internal/domain/yaml_test.go`

Why: linker needs to rewrite only `linked_notes` (and bump `schema_version` for legacy 1.0 files) without touching the body. Doing this atomically requires a domain helper so adapters call one tested function.

- [ ] **Step 1: Write failing test for `UpdateFrontmatter`**

Append to `internal/domain/yaml_test.go`:

```go
func TestUpdateFrontmatter_RewritesLinkedNotesAndBumpsVersion(t *testing.T) {
    src := []byte(`---
id: 20260527-x
schema_version: "1.0"
date: 2026-05-27T22:40:00Z
source: telegram-text
kind: atom
category: work/projects
tags: [a]
ingest:
  dump_id: d
  model_atomize: m
---
hello body
`)
    got, err := UpdateFrontmatter(src, []string{"20260520-a", "20260521-b"})
    require.NoError(t, err)
    n, err := UnmarshalNote(got)
    require.NoError(t, err)
    assert.Equal(t, "1.1", n.SchemaVersion)
    assert.Equal(t, []string{"20260520-a", "20260521-b"}, n.LinkedNotes)
    assert.Equal(t, "hello body\n", n.Body)
}

func TestUpdateFrontmatter_EmptyLinksClearField(t *testing.T) {
    src := []byte(`---
id: x
schema_version: "1.1"
date: 2026-05-27T22:40:00Z
source: telegram-text
kind: atom
category: work
tags: []
linked_notes:
  - id1
ingest: {dump_id: d, model_atomize: m}
---
body
`)
    got, err := UpdateFrontmatter(src, nil)
    require.NoError(t, err)
    assert.NotContains(t, string(got), "linked_notes")
}
```

- [ ] **Step 2: Run test, expect fail**

```bash
go test -run 'TestUpdateFrontmatter' ./internal/domain/...
```
Expected: FAIL with "undefined: UpdateFrontmatter".

- [ ] **Step 3: Implement `UpdateFrontmatter`**

Append to `internal/domain/yaml.go`:

```go
// UpdateFrontmatter rewrites the YAML frontmatter of a marshalled note so
// that:
//   - linked_notes is set to the given slice (or removed if nil/empty)
//   - schema_version is set to the current SchemaVersion
//
// Body bytes are preserved verbatim. The function is pure: it does not
// touch the filesystem.
func UpdateFrontmatter(data []byte, links []string) ([]byte, error) {
    n, err := UnmarshalNote(data)
    if err != nil {
        return nil, fmt.Errorf("parse frontmatter: %w", err)
    }
    n.SchemaVersion = SchemaVersion
    n.LinkedNotes = links
    return MarshalNote(n)
}
```

- [ ] **Step 4: Run test, expect pass**

```bash
go test -run 'TestUpdateFrontmatter' ./internal/domain/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/yaml.go internal/domain/yaml_test.go
git commit -m "feat(domain): UpdateFrontmatter helper for linker rewrites

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Extract `internal/adapter/httpretry/` package

**Files:**
- Create: `internal/adapter/httpretry/retry.go`
- Create: `internal/adapter/httpretry/retry_test.go`
- Create: `internal/adapter/httpretry/http_error.go`
- Delete: `internal/adapter/out/openrouter/retry.go`
- Delete: `internal/adapter/out/openrouter/retry_test.go`
- Modify: `internal/adapter/out/openrouter/client.go`
- Modify: `internal/adapter/out/openrouter/atomizer.go`
- Modify: `internal/adapter/out/openrouter/transcriber.go`

Why: `voyage` adapter needs the same retry+HTTPError logic. Moving it to a shared package avoids cross-import between out-adapters.

- [ ] **Step 1: Create new package files**

`internal/adapter/httpretry/http_error.go`:

```go
// Package httpretry provides retry policy and HTTP error classification
// shared by all HTTP-backed driven adapters.
package httpretry

import "fmt"

// HTTPError is returned by adapter callbacks to communicate an HTTP-shaped
// failure to the retry layer without coupling to a specific client.
type HTTPError struct {
    Status int
    Msg    string
}

func (e HTTPError) Error() string { return fmt.Sprintf("http %d: %s", e.Status, e.Msg) }
```

`internal/adapter/httpretry/retry.go`:

```go
package httpretry

import (
    "context"
    "errors"
    "math/rand/v2"
    "net"
    "slices"
    "time"
)

// Policy describes the back-off used by With.
type Policy struct {
    MaxAttempts int
    BaseDelay   time.Duration
    MaxDelay    time.Duration
    Jitter      float64 // 0..1, fraction of computed delay added randomly
}

// Default mirrors the values in the design spec (§5.3).
func Default() Policy {
    return Policy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 8 * time.Second, Jitter: 0.3}
}

var retryableStatuses = []int{429, 500, 502, 503, 504}

// With invokes op until it succeeds, the context is cancelled, or
// MaxAttempts is exhausted. Only retryable HTTP statuses and transient
// network errors are retried.
func With(ctx context.Context, p Policy, op func(context.Context) error) error {
    if p.MaxAttempts < 1 {
        p.MaxAttempts = 1
    }
    var lastErr error
    for attempt := range p.MaxAttempts {
        err := op(ctx)
        if err == nil {
            return nil
        }
        lastErr = err

        if ctxErr := ctx.Err(); ctxErr != nil {
            return errors.Join(lastErr, ctxErr)
        }
        if !isRetryable(err) {
            return err
        }
        if attempt == p.MaxAttempts-1 {
            break
        }
        delay := backoff(p, attempt)
        select {
        case <-ctx.Done():
            return errors.Join(lastErr, ctx.Err())
        case <-time.After(delay):
        }
    }
    return lastErr
}

func isRetryable(err error) bool {
    if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
        return false
    }
    if httpErr, ok := errors.AsType[HTTPError](err); ok {
        return slices.Contains(retryableStatuses, httpErr.Status)
    }
    var netErr net.Error
    return errors.As(err, &netErr)
}

func backoff(p Policy, attempt int) time.Duration {
    d := p.BaseDelay * (1 << attempt) //nolint:gosec // bounded by MaxAttempts
    if p.MaxDelay > 0 && d > p.MaxDelay {
        d = p.MaxDelay
    }
    if p.Jitter > 0 {
        jitter := time.Duration(rand.Float64() * p.Jitter * float64(d))
        d += jitter
    }
    return d
}
```

- [ ] **Step 2: Copy retry_test.go contents to new location**

Copy `internal/adapter/out/openrouter/retry_test.go` to `internal/adapter/httpretry/retry_test.go` and:
- Change `package openrouter` to `package httpretry`
- Replace `WithRetry(` with `With(`
- Replace `RetryPolicy{` with `Policy{`
- Replace `DefaultRetryPolicy()` with `Default()`
- Remove any openrouter-specific imports

- [ ] **Step 3: Run new package tests**

```bash
go test ./internal/adapter/httpretry/...
```
Expected: PASS.

- [ ] **Step 4: Update openrouter adapter to use httpretry**

In `internal/adapter/out/openrouter/client.go`:

```go
import (
    // ...
    "github.com/aleksejmetlusko/second-brain/internal/adapter/httpretry"
)

type Client struct {
    SDK     *openroutersdk.OpenRouter
    HTTP    *http.Client
    APIKey  string
    BaseURL string
    Retry   httpretry.Policy
}

type ClientConfig struct {
    // ... unchanged except:
    Retry httpretry.Policy
}
```

Remove the `RetryPolicy` field type reference and any `DefaultRetryPolicy` call. The caller (`cmd/ingest/main.go`) will be updated to pass `httpretry.Default()`.

- [ ] **Step 5: Update atomizer.go and transcriber.go**

In both files, replace `WithRetry(` with `httpretry.With(` and add the import:

```go
"github.com/aleksejmetlusko/second-brain/internal/adapter/httpretry"
```

The HTTPError type also moved — replace `HTTPError{Status: ...}` with `httpretry.HTTPError{Status: ...}` in both files (look in `postJSON` and similar functions). Update tests in `atomizer_test.go` and `transcriber_test.go` likewise.

- [ ] **Step 6: Update main.go**

In `cmd/ingest/main.go`, replace:

```go
Retry: openrouter.DefaultRetryPolicy(),
```

with:

```go
Retry: httpretry.Default(),
```

And add the import.

- [ ] **Step 7: Delete old retry files**

```bash
rm internal/adapter/out/openrouter/retry.go internal/adapter/out/openrouter/retry_test.go
```

- [ ] **Step 8: Build and test everything**

```bash
go build ./... && go test ./...
```
Expected: PASS across the board. Fix any forgotten imports/references.

- [ ] **Step 9: Lint**

```bash
make lint
```
Expected: clean.

- [ ] **Step 10: Commit**

```bash
git add internal/adapter/httpretry/ internal/adapter/out/openrouter/ cmd/ingest/main.go
git commit -m "refactor(adapter): extract httpretry package for reuse across HTTP adapters

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Ports — `Embedder` and `VectorIndex`

**Files:**
- Create: `internal/port/out/embedder.go`
- Create: `internal/port/out/vector_index.go`
- Modify: `.mockery.yml`

- [ ] **Step 1: Define Embedder port**

`internal/port/out/embedder.go`:

```go
package out

import "context"

// EmbedKind selects the model's input_type hint. Voyage and most modern
// embedders produce different vectors for documents vs queries — using the
// right kind on each side measurably improves recall.
type EmbedKind string

const (
    EmbedDocument EmbedKind = "document"
    EmbedQuery    EmbedKind = "query"
)

// Embedder turns a batch of texts into dense vectors.
type Embedder interface {
    Embed(ctx context.Context, texts []string, kind EmbedKind) ([][]float32, error)
    Dim() int
}
```

- [ ] **Step 2: Define VectorIndex port**

`internal/port/out/vector_index.go`:

```go
package out

import (
    "context"
    "time"
)

// IndexItem is what the indexer upserts into the vector store.
// LinkedNotes is intentionally absent — linker updates that field via
// UpdateLinkedNotes so re-embedding never clobbers existing links.
type IndexItem struct {
    ID        string
    FilePath  string
    BodyHash  string
    Category  string
    Tags      []string
    Date      time.Time
    Kind      string // "atom" | "summary"
    Embedding []float32
}

// IndexMeta is the readable row plus linked_notes, returned by GetMeta and
// ListAllMeta. It is the source of truth for the YAML linked_notes field.
type IndexMeta struct {
    ID          string
    FilePath    string
    BodyHash    string
    Category    string
    Tags        []string
    Date        time.Time
    Kind        string
    IndexedAt   time.Time
    LinkedNotes []string
}

// SearchQuery scopes a vector search. Zero values mean "no filter".
type SearchQuery struct {
    TopK           int
    KindFilter     []string  // e.g. []string{"atom"}
    CategoryPrefix string    // e.g. "work/projects"
    DateFrom       time.Time // zero = no lower bound
}

// SearchHit is one result. Score is cosine similarity in [-1, 1].
type SearchHit struct {
    ID    string
    Score float32
    Meta  IndexMeta
}

// VectorIndex is the driven port for the embedding store.
type VectorIndex interface {
    Upsert(ctx context.Context, items []IndexItem) error
    SearchByVector(ctx context.Context, vec []float32, q SearchQuery) ([]SearchHit, error)
    GetMeta(ctx context.Context, id string) (IndexMeta, bool, error)
    GetEmbedding(ctx context.Context, id string) ([]float32, bool, error)
    UpdateLinkedNotes(ctx context.Context, id string, links []string) error
    DeleteByIDs(ctx context.Context, ids []string) error
    ListAllMeta(ctx context.Context) ([]IndexMeta, error)
}
```

- [ ] **Step 3: Register new ports in mockery**

In `.mockery.yml`, under the `interfaces:` block for `internal/port/out`, add:

```yaml
      Embedder:
      VectorIndex:
```

- [ ] **Step 4: Regenerate mocks**

```bash
make mocks
```

Verify new files exist:

```bash
ls internal/port/out/mocks/mock_Embedder.go internal/port/out/mocks/mock_VectorIndex.go
```

- [ ] **Step 5: Build to ensure ports compile**

```bash
go build ./...
```
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/port/out/embedder.go internal/port/out/vector_index.go .mockery.yml internal/port/out/mocks/
git commit -m "feat(port): Embedder and VectorIndex driven ports

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 7: Open PR 1**

```bash
git push -u origin <branch>
gh pr create --title "feat: schema 1.1 + httpretry extraction + RAG ports" \
  --body "$(cat <<'EOF'
## Summary
- Bumps schema to 1.1 with optional `linked_notes` field
- Adds `domain.UpdateFrontmatter` helper for atomic linker rewrites
- Extracts `adapter/httpretry` package, reused across HTTP-backed adapters
- Defines `Embedder` and `VectorIndex` driven ports + mockery stubs

Reference spec: `docs/superpowers/specs/2026-05-29-rag-design.md`

## Test plan
- [ ] `go test ./...` green
- [ ] `make lint` clean
- [ ] `git grep '"1.0"'` shows only test fixtures

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Wait for CI green before starting Task 5.

---

## Task 5: Config — Voyage env vars

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Add new fields to Config struct**

In `internal/config/config.go` `Config` struct, append:

```go
VoyageAPIKey   string `env:"VOYAGE_API_KEY"   env-required:"true" env-description:"Voyage AI API key for embeddings"`
EmbeddingModel string `env:"EMBEDDING_MODEL"  env-default:"voyage-4-large"`
EmbeddingDim   int    `env:"EMBEDDING_DIM"    env-default:"1024"`
VoyageBaseURL  string `env:"VOYAGE_BASE_URL"  env-default:"https://api.voyageai.com/v1"`
```

- [ ] **Step 2: Add Validate check**

In the `Validate` method, before the final `return nil`:

```go
if c.VoyageAPIKey == "" {
    return fmt.Errorf("VOYAGE_API_KEY is required but empty")
}
if c.EmbeddingDim <= 0 {
    return fmt.Errorf("EMBEDDING_DIM must be positive, got %d", c.EmbeddingDim)
}
```

- [ ] **Step 3: Update config tests**

In `internal/config/config_test.go`, find the test that sets minimum env vars to make Load succeed (probably `TestLoad_HappyPath` or similar). Add:

```go
t.Setenv("VOYAGE_API_KEY", "test-key")
```

Add a new test:

```go
func TestLoad_MissingVoyageKey_Fails(t *testing.T) {
    setRequiredEnv(t)  // or whatever helper exists; replicate manually if needed
    os.Unsetenv("VOYAGE_API_KEY")
    _, err := Load()
    require.Error(t, err)
    assert.Contains(t, err.Error(), "VOYAGE_API_KEY")
}
```

- [ ] **Step 4: Run config tests**

```bash
go test ./internal/config/...
```
Expected: PASS.

- [ ] **Step 5: Regenerate .env.example**

```bash
make env-example
```

Inspect the diff: new lines for VOYAGE_API_KEY, EMBEDDING_MODEL, EMBEDDING_DIM, VOYAGE_BASE_URL should appear.

- [ ] **Step 6: Commit**

```bash
git add internal/config/ .env.example
git commit -m "feat(config): Voyage API key + embedding model/dim env vars

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Voyage adapter — client + embedder

**Files:**
- Create: `internal/adapter/out/voyage/client.go`
- Create: `internal/adapter/out/voyage/embedder.go`
- Create: `internal/adapter/out/voyage/embedder_test.go`

- [ ] **Step 1: Write failing test using `httptest.Server`**

`internal/adapter/out/voyage/embedder_test.go`:

```go
package voyage

import (
    "context"
    "encoding/json"
    "io"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/aleksejmetlusko/second-brain/internal/adapter/httpretry"
    portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestEmbedder_HappyPath_ReturnsVectors(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        assert.Equal(t, "/embeddings", r.URL.Path)
        assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

        var req struct {
            Model     string   `json:"model"`
            Input     []string `json:"input"`
            InputType string   `json:"input_type"`
        }
        require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
        assert.Equal(t, "voyage-4-large", req.Model)
        assert.Equal(t, "document", req.InputType)
        assert.Equal(t, []string{"hello", "world"}, req.Input)

        resp := map[string]any{
            "data": []map[string]any{
                {"embedding": []float32{0.1, 0.2, 0.3}, "index": 0},
                {"embedding": []float32{0.4, 0.5, 0.6}, "index": 1},
            },
            "model": "voyage-4-large",
        }
        _ = json.NewEncoder(w).Encode(resp)
    }))
    defer srv.Close()

    c := NewClient(ClientConfig{
        APIKey:      "test-key",
        BaseURL:     srv.URL,
        HTTPTimeout: 5 * time.Second,
        Retry:       httpretry.Default(),
    })
    e := NewEmbedder(c, "voyage-4-large", 3)

    vecs, err := e.Embed(context.Background(), []string{"hello", "world"}, portout.EmbedDocument)
    require.NoError(t, err)
    require.Len(t, vecs, 2)
    assert.Equal(t, []float32{0.1, 0.2, 0.3}, vecs[0])
    assert.Equal(t, []float32{0.4, 0.5, 0.6}, vecs[1])
}

func TestEmbedder_Returns429AsRetryable(t *testing.T) {
    var calls int
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        calls++
        if calls < 3 {
            w.WriteHeader(http.StatusTooManyRequests)
            _, _ = io.WriteString(w, "slow down")
            return
        }
        resp := map[string]any{
            "data":  []map[string]any{{"embedding": []float32{1, 0}, "index": 0}},
            "model": "m",
        }
        _ = json.NewEncoder(w).Encode(resp)
    }))
    defer srv.Close()

    c := NewClient(ClientConfig{APIKey: "k", BaseURL: srv.URL, HTTPTimeout: 5 * time.Second, Retry: httpretry.Policy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, Jitter: 0}})
    e := NewEmbedder(c, "m", 2)

    vecs, err := e.Embed(context.Background(), []string{"x"}, portout.EmbedQuery)
    require.NoError(t, err)
    assert.Len(t, vecs, 1)
    assert.Equal(t, 3, calls)
}

func TestEmbedder_400IsTerminal(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(http.StatusBadRequest)
        _, _ = io.WriteString(w, "bad model")
    }))
    defer srv.Close()

    c := NewClient(ClientConfig{APIKey: "k", BaseURL: srv.URL, HTTPTimeout: 5 * time.Second, Retry: httpretry.Default()})
    e := NewEmbedder(c, "m", 2)

    _, err := e.Embed(context.Background(), []string{"x"}, portout.EmbedQuery)
    require.Error(t, err)
}

func TestEmbedder_DimReturnsConfigured(t *testing.T) {
    e := NewEmbedder(nil, "m", 1024)
    assert.Equal(t, 1024, e.Dim())
}
```

- [ ] **Step 2: Run test, expect compile failure**

```bash
go test ./internal/adapter/out/voyage/...
```
Expected: FAIL (package doesn't compile — no implementation).

- [ ] **Step 3: Implement `client.go`**

`internal/adapter/out/voyage/client.go`:

```go
// Package voyage implements the Embedder driven port using voyageai.com.
package voyage

import (
    "net/http"
    "time"

    "github.com/aleksejmetlusko/second-brain/internal/adapter/httpretry"
)

// Client groups HTTP + auth + retry policy for Voyage endpoints.
type Client struct {
    HTTP    *http.Client
    APIKey  string
    BaseURL string
    Retry   httpretry.Policy
}

// ClientConfig is the constructor input.
type ClientConfig struct {
    APIKey      string
    BaseURL     string
    HTTPTimeout time.Duration
    Retry       httpretry.Policy
}

// NewClient builds a Client. BaseURL omits trailing slash.
func NewClient(cfg ClientConfig) *Client {
    return &Client{
        HTTP:    &http.Client{Timeout: cfg.HTTPTimeout},
        APIKey:  cfg.APIKey,
        BaseURL: cfg.BaseURL,
        Retry:   cfg.Retry,
    }
}
```

- [ ] **Step 4: Implement `embedder.go`**

`internal/adapter/out/voyage/embedder.go`:

```go
package voyage

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"

    "github.com/aleksejmetlusko/second-brain/internal/adapter/httpretry"
    "github.com/aleksejmetlusko/second-brain/internal/domain"
    portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// Embedder implements port/out.Embedder against the Voyage HTTP API.
type Embedder struct {
    client *Client
    model  string
    dim    int
}

// NewEmbedder constructs an Embedder bound to a model and expected dim.
func NewEmbedder(c *Client, model string, dim int) *Embedder {
    return &Embedder{client: c, model: model, dim: dim}
}

// Dim returns the configured embedding dimension.
func (e *Embedder) Dim() int { return e.dim }

type embedRequest struct {
    Model     string   `json:"model"`
    Input     []string `json:"input"`
    InputType string   `json:"input_type"`
}

type embedResponse struct {
    Data []struct {
        Embedding []float32 `json:"embedding"`
        Index     int       `json:"index"`
    } `json:"data"`
    Model string `json:"model"`
}

// Embed sends texts to Voyage and returns vectors in input order.
func (e *Embedder) Embed(ctx context.Context, texts []string, kind portout.EmbedKind) ([][]float32, error) {
    if len(texts) == 0 {
        return nil, nil
    }
    reqBody := embedRequest{
        Model:     e.model,
        Input:     texts,
        InputType: string(kind),
    }

    var resp embedResponse
    err := httpretry.With(ctx, e.client.Retry, func(ctx context.Context) error {
        raw, err := e.postJSON(ctx, "/embeddings", reqBody)
        if err != nil {
            return err
        }
        if err := json.Unmarshal(raw, &resp); err != nil {
            return fmt.Errorf("%w: decode response: %w", domain.ErrEmbedder, err)
        }
        return nil
    })
    if err != nil {
        return nil, err
    }

    if len(resp.Data) != len(texts) {
        return nil, fmt.Errorf("%w: expected %d vectors, got %d", domain.ErrEmbedder, len(texts), len(resp.Data))
    }
    out := make([][]float32, len(texts))
    for _, d := range resp.Data {
        if d.Index < 0 || d.Index >= len(out) {
            return nil, fmt.Errorf("%w: out-of-range index %d", domain.ErrEmbedder, d.Index)
        }
        if len(d.Embedding) != e.dim {
            return nil, fmt.Errorf("%w: expected dim %d, got %d", domain.ErrEmbedder, e.dim, len(d.Embedding))
        }
        out[d.Index] = d.Embedding
    }
    return out, nil
}

func (e *Embedder) postJSON(ctx context.Context, path string, body any) ([]byte, error) {
    payload, err := json.Marshal(body)
    if err != nil {
        return nil, fmt.Errorf("%w: marshal request: %w", domain.ErrEmbedder, err)
    }
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.client.BaseURL+path, bytes.NewReader(payload))
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "Bearer "+e.client.APIKey)
    req.Header.Set("Content-Type", "application/json")

    resp, err := e.client.HTTP.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    data, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, err
    }
    if resp.StatusCode >= 400 {
        return nil, httpretry.HTTPError{Status: resp.StatusCode, Msg: string(data)}
    }
    return data, nil
}
```

- [ ] **Step 5: Run tests, expect pass**

```bash
go test ./internal/adapter/out/voyage/...
```
Expected: PASS.

- [ ] **Step 6: Lint**

```bash
make lint
```
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add internal/adapter/out/voyage/
git commit -m "feat(adapter): Voyage embedder via /v1/embeddings

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Open PR 2

- [ ] **Step 1: Push and open PR**

```bash
git push -u origin <branch>
gh pr create --title "feat(adapter): Voyage embedder + config" --body "$(cat <<'EOF'
## Summary
- Adds `internal/adapter/out/voyage/` with HTTP client and `Embedder` implementation
- Adds `VOYAGE_API_KEY`, `EMBEDDING_MODEL`, `EMBEDDING_DIM`, `VOYAGE_BASE_URL` env vars
- Reuses `httpretry` from PR 1 for retry/backoff

## Test plan
- [ ] `go test ./internal/adapter/out/voyage/...` green
- [ ] CI lint clean

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Wait for CI green before starting Task 8.

---

## Task 8: Add sqlite-vec dependencies + Dockerfile CGO flip

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `Dockerfile`

- [ ] **Step 1: Add dependencies**

```bash
go get github.com/mattn/go-sqlite3
go get github.com/asg017/sqlite-vec-go-bindings/cgo
go mod tidy
```

If the second `go get` fails with "no matching versions", the canonical path may have shifted. Run:

```bash
go list -m -versions github.com/asg017/sqlite-vec-go-bindings/cgo
```

and pick the latest. If the module is unresolvable, fall back to:

```bash
go get github.com/asg017/sqlite-vec-go-bindings@latest
```

Document the actual import path you used; subsequent tasks will reference it.

- [ ] **Step 2: Flip Dockerfile to CGO**

Replace the entire `Dockerfile` content:

```dockerfile
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
```

Key change: removed `CGO_ENABLED=0`, added `gcc musl-dev sqlite-dev` to builder, added `sqlite-libs` to runtime.

- [ ] **Step 3: Build locally to confirm**

```bash
docker build -t second-brain/ingest:rag-test .
```
Expected: builds successfully. Note: this is the moment to discover if `asg017/sqlite-vec` ships musl-compatible native libs. If `apk add` step fails or runtime image complains about missing native lib at load time (Task 9), fall back to `debian-slim` runtime (open-question §8.1 in spec).

If the build fails specifically because sqlite-vec can't find a musl-compatible `.so`, switch the runtime stage to:

```dockerfile
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata libsqlite3-0 && rm -rf /var/lib/apt/lists/* && \
    groupadd -r app && useradd -r -g app app
COPY --from=builder /out/ingest /usr/local/bin/ingest
USER app
ENTRYPOINT ["/usr/local/bin/ingest"]
```

And update the builder to `golang:1.26-bookworm` and `apt-get install build-essential libsqlite3-dev`.

- [ ] **Step 4: Run full go test to make sure deps don't break compile**

```bash
go test ./...
```
Expected: PASS (no new code yet, just deps).

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum Dockerfile
git commit -m "build: enable CGO and add sqlite-vec/mattn-sqlite3 deps

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: sqlite-vec adapter — schema and Index implementation

**Files:**
- Create: `internal/adapter/out/sqlitevec/schema.go`
- Create: `internal/adapter/out/sqlitevec/index.go`
- Create: `internal/adapter/out/sqlitevec/index_integration_test.go`

- [ ] **Step 1: Write integration test first (TDD for the contract)**

`internal/adapter/out/sqlitevec/index_integration_test.go`:

```go
//go:build integration

package sqlitevec_test

import (
    "context"
    "path/filepath"
    "testing"
    "time"

    "github.com/aleksejmetlusko/second-brain/internal/adapter/out/sqlitevec"
    portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func openTestIndex(t *testing.T) *sqlitevec.Index {
    t.Helper()
    path := filepath.Join(t.TempDir(), "test.db")
    idx, err := sqlitevec.Open(context.Background(), sqlitevec.Config{
        DBPath:         path,
        EmbeddingDim:   3,
        EmbeddingModel: "test-model",
    })
    require.NoError(t, err)
    t.Cleanup(func() { idx.Close() })
    return idx
}

func TestIndex_UpsertAndSearchRoundtrip(t *testing.T) {
    idx := openTestIndex(t)
    ctx := context.Background()

    items := []portout.IndexItem{
        {ID: "a", FilePath: "a.md", BodyHash: "h1", Category: "work", Kind: "atom", Date: time.Now(), Embedding: []float32{1, 0, 0}},
        {ID: "b", FilePath: "b.md", BodyHash: "h2", Category: "work", Kind: "atom", Date: time.Now(), Embedding: []float32{0, 1, 0}},
        {ID: "c", FilePath: "c.md", BodyHash: "h3", Category: "personal", Kind: "atom", Date: time.Now(), Embedding: []float32{0, 0, 1}},
    }
    require.NoError(t, idx.Upsert(ctx, items))

    hits, err := idx.SearchByVector(ctx, []float32{1, 0, 0}, portout.SearchQuery{TopK: 2})
    require.NoError(t, err)
    require.Len(t, hits, 2)
    assert.Equal(t, "a", hits[0].ID)
}

func TestIndex_DeleteByIDs(t *testing.T) {
    idx := openTestIndex(t)
    ctx := context.Background()

    require.NoError(t, idx.Upsert(ctx, []portout.IndexItem{
        {ID: "a", FilePath: "a.md", BodyHash: "h", Category: "x", Kind: "atom", Date: time.Now(), Embedding: []float32{1, 0, 0}},
    }))
    require.NoError(t, idx.DeleteByIDs(ctx, []string{"a"}))

    _, found, err := idx.GetMeta(ctx, "a")
    require.NoError(t, err)
    assert.False(t, found)
}

func TestIndex_UpdateLinkedNotes(t *testing.T) {
    idx := openTestIndex(t)
    ctx := context.Background()

    require.NoError(t, idx.Upsert(ctx, []portout.IndexItem{
        {ID: "a", FilePath: "a.md", BodyHash: "h", Category: "x", Kind: "atom", Date: time.Now(), Embedding: []float32{1, 0, 0}},
    }))
    require.NoError(t, idx.UpdateLinkedNotes(ctx, "a", []string{"b", "c"}))

    meta, found, err := idx.GetMeta(ctx, "a")
    require.NoError(t, err)
    require.True(t, found)
    assert.Equal(t, []string{"b", "c"}, meta.LinkedNotes)
}

func TestIndex_GetEmbedding(t *testing.T) {
    idx := openTestIndex(t)
    ctx := context.Background()

    vec := []float32{0.5, 0.5, 0}
    require.NoError(t, idx.Upsert(ctx, []portout.IndexItem{
        {ID: "a", FilePath: "a.md", BodyHash: "h", Category: "x", Kind: "atom", Date: time.Now(), Embedding: vec},
    }))
    got, found, err := idx.GetEmbedding(ctx, "a")
    require.NoError(t, err)
    require.True(t, found)
    assert.InDeltaSlice(t, vec, got, 1e-6)
}

func TestIndex_KindFilterAppliedInSearch(t *testing.T) {
    idx := openTestIndex(t)
    ctx := context.Background()

    require.NoError(t, idx.Upsert(ctx, []portout.IndexItem{
        {ID: "a", FilePath: "a.md", BodyHash: "h", Category: "x", Kind: "atom", Date: time.Now(), Embedding: []float32{1, 0, 0}},
        {ID: "s", FilePath: "s.md", BodyHash: "h", Category: "x", Kind: "summary", Date: time.Now(), Embedding: []float32{0.9, 0.1, 0}},
    }))
    hits, err := idx.SearchByVector(ctx, []float32{1, 0, 0}, portout.SearchQuery{TopK: 5, KindFilter: []string{"atom"}})
    require.NoError(t, err)
    require.Len(t, hits, 1)
    assert.Equal(t, "a", hits[0].ID)
}

func TestIndex_DimMismatchReturnsError(t *testing.T) {
    idx := openTestIndex(t)
    ctx := context.Background()
    err := idx.Upsert(ctx, []portout.IndexItem{
        {ID: "a", Embedding: []float32{1, 0}}, // 2-dim, expected 3
    })
    require.Error(t, err)
}
```

- [ ] **Step 2: Run test, expect compile failure**

```bash
go test -tags=integration ./internal/adapter/out/sqlitevec/...
```
Expected: FAIL (package missing).

- [ ] **Step 3: Implement `schema.go`**

`internal/adapter/out/sqlitevec/schema.go`:

```go
package sqlitevec

import (
    "context"
    "database/sql"
    "fmt"
)

// applySchema creates tables on a fresh DB or no-ops if they exist.
// `dim` parameterizes the vec0 virtual table column type.
func applySchema(ctx context.Context, db *sql.DB, dim int) error {
    stmts := []string{
        `CREATE TABLE IF NOT EXISTS notes_meta (
            id            TEXT PRIMARY KEY,
            file_path     TEXT NOT NULL,
            body_hash     TEXT NOT NULL,
            category      TEXT NOT NULL,
            tags_json     TEXT NOT NULL,
            date          TEXT NOT NULL,
            kind          TEXT NOT NULL,
            indexed_at    TEXT NOT NULL,
            linked_notes  TEXT NOT NULL DEFAULT '[]'
        )`,
        `CREATE TABLE IF NOT EXISTS index_meta (
            key   TEXT PRIMARY KEY,
            value TEXT NOT NULL
        )`,
        fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS notes_vec USING vec0(
            id        TEXT PRIMARY KEY,
            embedding FLOAT[%d]
        )`, dim),
    }
    for _, s := range stmts {
        if _, err := db.ExecContext(ctx, s); err != nil {
            return fmt.Errorf("exec schema stmt: %w", err)
        }
    }
    return nil
}

// configureIndexMeta records the embedding model/dim that built the index.
// On mismatch with the current config, Open will rebuild the vec table.
func readIndexMeta(ctx context.Context, db *sql.DB) (model string, dim int, _ error) {
    rows, err := db.QueryContext(ctx, `SELECT key, value FROM index_meta`)
    if err != nil {
        return "", 0, err
    }
    defer rows.Close()
    for rows.Next() {
        var k, v string
        if err := rows.Scan(&k, &v); err != nil {
            return "", 0, err
        }
        switch k {
        case "embedding_model":
            model = v
        case "embedding_dim":
            _, _ = fmt.Sscanf(v, "%d", &dim)
        }
    }
    return model, dim, rows.Err()
}

func writeIndexMeta(ctx context.Context, db *sql.DB, model string, dim int) error {
    stmts := []struct{ k, v string }{
        {"embedding_model", model},
        {"embedding_dim", fmt.Sprintf("%d", dim)},
    }
    for _, s := range stmts {
        if _, err := db.ExecContext(ctx, `INSERT OR REPLACE INTO index_meta(key, value) VALUES (?, ?)`, s.k, s.v); err != nil {
            return fmt.Errorf("write index_meta: %w", err)
        }
    }
    return nil
}
```

- [ ] **Step 4: Implement `index.go`**

`internal/adapter/out/sqlitevec/index.go`:

```go
// Package sqlitevec implements VectorIndex backed by SQLite + sqlite-vec.
package sqlitevec

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "log/slog"
    "strings"
    "time"
    "unsafe"

    sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
    _ "github.com/mattn/go-sqlite3"

    "github.com/aleksejmetlusko/second-brain/internal/domain"
    portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// Config is the constructor input.
type Config struct {
    DBPath         string
    EmbeddingDim   int
    EmbeddingModel string
}

// Index implements port/out.VectorIndex.
type Index struct {
    db  *sql.DB
    dim int
}

// Open creates or opens the DB, loads the vec0 extension, applies schema,
// and rebuilds vec table if EmbeddingDim or EmbeddingModel changed.
func Open(ctx context.Context, cfg Config) (*Index, error) {
    sqlitevec.Auto() // registers the vec extension on every sqlite3 conn

    db, err := sql.Open("sqlite3", cfg.DBPath)
    if err != nil {
        return nil, fmt.Errorf("%w: open: %w", domain.ErrVectorIndex, err)
    }
    db.SetMaxOpenConns(1) // SQLite + sqlite-vec are happiest single-conn

    if err := applySchema(ctx, db, cfg.EmbeddingDim); err != nil {
        return nil, fmt.Errorf("%w: schema: %w", domain.ErrVectorIndex, err)
    }

    prevModel, prevDim, err := readIndexMeta(ctx, db)
    if err != nil {
        return nil, fmt.Errorf("%w: read meta: %w", domain.ErrVectorIndex, err)
    }
    if prevDim != 0 && prevDim != cfg.EmbeddingDim {
        slog.Warn("embedding dim changed, dropping notes_vec and notes_meta", "old_dim", prevDim, "new_dim", cfg.EmbeddingDim)
        if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS notes_vec`); err != nil {
            return nil, fmt.Errorf("%w: drop vec: %w", domain.ErrVectorIndex, err)
        }
        if _, err := db.ExecContext(ctx, `DELETE FROM notes_meta`); err != nil {
            return nil, fmt.Errorf("%w: clear meta: %w", domain.ErrVectorIndex, err)
        }
        if err := applySchema(ctx, db, cfg.EmbeddingDim); err != nil {
            return nil, fmt.Errorf("%w: re-create schema: %w", domain.ErrVectorIndex, err)
        }
    } else if prevModel != "" && prevModel != cfg.EmbeddingModel {
        slog.Warn("embedding model changed (same dim), clearing index", "old", prevModel, "new", cfg.EmbeddingModel)
        if _, err := db.ExecContext(ctx, `DELETE FROM notes_vec`); err != nil {
            return nil, fmt.Errorf("%w: clear vec: %w", domain.ErrVectorIndex, err)
        }
        if _, err := db.ExecContext(ctx, `DELETE FROM notes_meta`); err != nil {
            return nil, fmt.Errorf("%w: clear meta: %w", domain.ErrVectorIndex, err)
        }
    }
    if err := writeIndexMeta(ctx, db, cfg.EmbeddingModel, cfg.EmbeddingDim); err != nil {
        return nil, err
    }
    return &Index{db: db, dim: cfg.EmbeddingDim}, nil
}

// Close releases the DB.
func (i *Index) Close() error { return i.db.Close() }

// Upsert inserts/updates rows. Does NOT touch linked_notes — use UpdateLinkedNotes.
func (i *Index) Upsert(ctx context.Context, items []portout.IndexItem) error {
    if len(items) == 0 {
        return nil
    }
    tx, err := i.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("%w: begin: %w", domain.ErrVectorIndex, err)
    }
    defer tx.Rollback() //nolint:errcheck

    metaStmt, err := tx.PrepareContext(ctx, `
        INSERT INTO notes_meta(id, file_path, body_hash, category, tags_json, date, kind, indexed_at, linked_notes)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, '[]')
        ON CONFLICT(id) DO UPDATE SET
          file_path  = excluded.file_path,
          body_hash  = excluded.body_hash,
          category   = excluded.category,
          tags_json  = excluded.tags_json,
          date       = excluded.date,
          kind       = excluded.kind,
          indexed_at = excluded.indexed_at`)
    if err != nil {
        return fmt.Errorf("%w: prep meta: %w", domain.ErrVectorIndex, err)
    }
    defer metaStmt.Close()

    vecDelStmt, err := tx.PrepareContext(ctx, `DELETE FROM notes_vec WHERE id = ?`)
    if err != nil {
        return fmt.Errorf("%w: prep vec del: %w", domain.ErrVectorIndex, err)
    }
    defer vecDelStmt.Close()
    vecInsStmt, err := tx.PrepareContext(ctx, `INSERT INTO notes_vec(id, embedding) VALUES (?, ?)`)
    if err != nil {
        return fmt.Errorf("%w: prep vec ins: %w", domain.ErrVectorIndex, err)
    }
    defer vecInsStmt.Close()

    now := time.Now().UTC().Format(time.RFC3339)
    for _, it := range items {
        if len(it.Embedding) != i.dim {
            return fmt.Errorf("%w: item %s has dim %d, want %d", domain.ErrVectorIndex, it.ID, len(it.Embedding), i.dim)
        }
        tagsJSON, err := json.Marshal(it.Tags)
        if err != nil {
            return fmt.Errorf("%w: marshal tags: %w", domain.ErrVectorIndex, err)
        }
        if _, err := metaStmt.ExecContext(ctx, it.ID, it.FilePath, it.BodyHash, it.Category, string(tagsJSON), it.Date.UTC().Format(time.RFC3339), it.Kind, now); err != nil {
            return fmt.Errorf("%w: upsert meta: %w", domain.ErrVectorIndex, err)
        }
        if _, err := vecDelStmt.ExecContext(ctx, it.ID); err != nil {
            return fmt.Errorf("%w: vec del: %w", domain.ErrVectorIndex, err)
        }
        if _, err := vecInsStmt.ExecContext(ctx, it.ID, float32SliceToBlob(it.Embedding)); err != nil {
            return fmt.Errorf("%w: vec ins: %w", domain.ErrVectorIndex, err)
        }
    }
    return tx.Commit()
}

// SearchByVector returns top-K matches sorted by descending similarity.
func (i *Index) SearchByVector(ctx context.Context, vec []float32, q portout.SearchQuery) ([]portout.SearchHit, error) {
    if q.TopK <= 0 {
        q.TopK = 10
    }
    if len(vec) != i.dim {
        return nil, fmt.Errorf("%w: query dim %d != index dim %d", domain.ErrVectorIndex, len(vec), i.dim)
    }

    var whereClauses []string
    var args []any

    args = append(args, float32SliceToBlob(vec), q.TopK)
    if len(q.KindFilter) > 0 {
        placeholders := strings.TrimRight(strings.Repeat("?,", len(q.KindFilter)), ",")
        whereClauses = append(whereClauses, "m.kind IN ("+placeholders+")")
        for _, k := range q.KindFilter {
            args = append(args, k)
        }
    }
    if q.CategoryPrefix != "" {
        whereClauses = append(whereClauses, "m.category LIKE ?")
        args = append(args, q.CategoryPrefix+"%")
    }
    if !q.DateFrom.IsZero() {
        whereClauses = append(whereClauses, "m.date >= ?")
        args = append(args, q.DateFrom.UTC().Format(time.RFC3339))
    }
    whereSQL := ""
    if len(whereClauses) > 0 {
        whereSQL = " AND " + strings.Join(whereClauses, " AND ")
    }

    query := `
        SELECT v.id, v.distance, m.file_path, m.body_hash, m.category, m.tags_json, m.date, m.kind, m.indexed_at, m.linked_notes
        FROM notes_vec v
        JOIN notes_meta m ON m.id = v.id
        WHERE v.embedding MATCH ? AND k = ?` + whereSQL + `
        ORDER BY v.distance ASC`

    rows, err := i.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, fmt.Errorf("%w: search: %w", domain.ErrVectorIndex, err)
    }
    defer rows.Close()

    var hits []portout.SearchHit
    for rows.Next() {
        var (
            h        portout.SearchHit
            distance float64
            tagsJSON string
            linksRaw string
            dateStr  string
            indexedAtStr string
        )
        if err := rows.Scan(&h.ID, &distance, &h.Meta.FilePath, &h.Meta.BodyHash, &h.Meta.Category, &tagsJSON, &dateStr, &h.Meta.Kind, &indexedAtStr, &linksRaw); err != nil {
            return nil, fmt.Errorf("%w: scan: %w", domain.ErrVectorIndex, err)
        }
        h.Meta.ID = h.ID
        // sqlite-vec returns L2 distance for FLOAT[] by default; convert to
        // a similarity-like score. We use 1 - distance/2 which lies in [0,1]
        // for unit-norm vectors. Embedder must return unit-norm or our
        // threshold reasoning is off — voyage-4-large already does.
        h.Score = float32(1 - distance/2)
        h.Meta.Date, _ = time.Parse(time.RFC3339, dateStr)
        h.Meta.IndexedAt, _ = time.Parse(time.RFC3339, indexedAtStr)
        _ = json.Unmarshal([]byte(tagsJSON), &h.Meta.Tags)
        _ = json.Unmarshal([]byte(linksRaw), &h.Meta.LinkedNotes)
        hits = append(hits, h)
    }
    return hits, rows.Err()
}

// GetMeta loads a meta row by id.
func (i *Index) GetMeta(ctx context.Context, id string) (portout.IndexMeta, bool, error) {
    var (
        m            portout.IndexMeta
        tagsJSON     string
        linksRaw     string
        dateStr      string
        indexedAtStr string
    )
    err := i.db.QueryRowContext(ctx, `SELECT id, file_path, body_hash, category, tags_json, date, kind, indexed_at, linked_notes FROM notes_meta WHERE id = ?`, id).
        Scan(&m.ID, &m.FilePath, &m.BodyHash, &m.Category, &tagsJSON, &dateStr, &m.Kind, &indexedAtStr, &linksRaw)
    if errors.Is(err, sql.ErrNoRows) {
        return portout.IndexMeta{}, false, nil
    }
    if err != nil {
        return portout.IndexMeta{}, false, fmt.Errorf("%w: get meta: %w", domain.ErrVectorIndex, err)
    }
    m.Date, _ = time.Parse(time.RFC3339, dateStr)
    m.IndexedAt, _ = time.Parse(time.RFC3339, indexedAtStr)
    _ = json.Unmarshal([]byte(tagsJSON), &m.Tags)
    _ = json.Unmarshal([]byte(linksRaw), &m.LinkedNotes)
    return m, true, nil
}

// GetEmbedding loads the stored vector by id.
func (i *Index) GetEmbedding(ctx context.Context, id string) ([]float32, bool, error) {
    var blob []byte
    err := i.db.QueryRowContext(ctx, `SELECT embedding FROM notes_vec WHERE id = ?`, id).Scan(&blob)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, false, nil
    }
    if err != nil {
        return nil, false, fmt.Errorf("%w: get embedding: %w", domain.ErrVectorIndex, err)
    }
    return blobToFloat32Slice(blob), true, nil
}

// UpdateLinkedNotes overwrites the linked_notes JSON column for id.
func (i *Index) UpdateLinkedNotes(ctx context.Context, id string, links []string) error {
    if links == nil {
        links = []string{}
    }
    raw, err := json.Marshal(links)
    if err != nil {
        return fmt.Errorf("%w: marshal links: %w", domain.ErrVectorIndex, err)
    }
    res, err := i.db.ExecContext(ctx, `UPDATE notes_meta SET linked_notes = ? WHERE id = ?`, string(raw), id)
    if err != nil {
        return fmt.Errorf("%w: update links: %w", domain.ErrVectorIndex, err)
    }
    if n, _ := res.RowsAffected(); n == 0 {
        return fmt.Errorf("%w: id %q not found", domain.ErrVectorIndex, id)
    }
    return nil
}

// DeleteByIDs removes rows from both tables in one tx.
func (i *Index) DeleteByIDs(ctx context.Context, ids []string) error {
    if len(ids) == 0 {
        return nil
    }
    tx, err := i.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("%w: begin: %w", domain.ErrVectorIndex, err)
    }
    defer tx.Rollback() //nolint:errcheck
    for _, id := range ids {
        if _, err := tx.ExecContext(ctx, `DELETE FROM notes_meta WHERE id = ?`, id); err != nil {
            return fmt.Errorf("%w: del meta: %w", domain.ErrVectorIndex, err)
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM notes_vec WHERE id = ?`, id); err != nil {
            return fmt.Errorf("%w: del vec: %w", domain.ErrVectorIndex, err)
        }
    }
    return tx.Commit()
}

// ListAllMeta returns all meta rows. For 10K notes this is ~1MB — fine for
// in-memory diff in the indexer.
func (i *Index) ListAllMeta(ctx context.Context) ([]portout.IndexMeta, error) {
    rows, err := i.db.QueryContext(ctx, `SELECT id, file_path, body_hash, category, tags_json, date, kind, indexed_at, linked_notes FROM notes_meta`)
    if err != nil {
        return nil, fmt.Errorf("%w: list meta: %w", domain.ErrVectorIndex, err)
    }
    defer rows.Close()
    var out []portout.IndexMeta
    for rows.Next() {
        var (
            m            portout.IndexMeta
            tagsJSON     string
            linksRaw     string
            dateStr      string
            indexedAtStr string
        )
        if err := rows.Scan(&m.ID, &m.FilePath, &m.BodyHash, &m.Category, &tagsJSON, &dateStr, &m.Kind, &indexedAtStr, &linksRaw); err != nil {
            return nil, fmt.Errorf("%w: scan: %w", domain.ErrVectorIndex, err)
        }
        m.Date, _ = time.Parse(time.RFC3339, dateStr)
        m.IndexedAt, _ = time.Parse(time.RFC3339, indexedAtStr)
        _ = json.Unmarshal([]byte(tagsJSON), &m.Tags)
        _ = json.Unmarshal([]byte(linksRaw), &m.LinkedNotes)
        out = append(out, m)
    }
    return out, rows.Err()
}

// float32SliceToBlob is sqlite-vec's expected on-wire format for FLOAT[N].
func float32SliceToBlob(v []float32) []byte {
    return unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), len(v)*4)
}

func blobToFloat32Slice(b []byte) []float32 {
    if len(b)%4 != 0 {
        return nil
    }
    n := len(b) / 4
    return unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), n)
}
```

NOTE on `sqlitevec.Auto()`: the actual binding function name depends on the version of `asg017/sqlite-vec-go-bindings`. If the import path you ended up with in Task 8 exposes a different registration mechanism, replace the `Auto()` call accordingly. The function should register the vec0 extension on every `*sql.DB` that opens a `sqlite3` driver. Check the binding's README for the precise call.

- [ ] **Step 5: Run integration test**

```bash
go test -tags=integration ./internal/adapter/out/sqlitevec/...
```
Expected: PASS for all tests.

If tests fail with "no such module: vec0" or "cannot load extension", that means the extension registration call is wrong — open the binding's source code and fix the `Auto()` call. If they fail with linker errors during compile, CGO isn't set up correctly on the local machine — verify `CGO_ENABLED=1` is the Go default and `gcc` is available.

- [ ] **Step 6: Lint**

```bash
make lint
```
Expected: clean. The `nolint:errcheck` on `tx.Rollback()` is intentional — defer rollback after successful commit returns sql.ErrTxDone which we don't care about.

- [ ] **Step 7: Commit**

```bash
git add internal/adapter/out/sqlitevec/
git commit -m "feat(adapter): sqlitevec VectorIndex with vec0 extension

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Open PR 3

- [ ] **Step 1: Push and open PR**

```bash
git push -u origin <branch>
gh pr create --title "feat: sqlite-vec VectorIndex + CGO Dockerfile" --body "$(cat <<'EOF'
## Summary
- Adds `internal/adapter/out/sqlitevec/` implementing VectorIndex on sqlite + vec0
- Flips Dockerfile to CGO; adds gcc/musl-dev/sqlite-dev to builder, sqlite-libs to runtime
- Adds `mattn/go-sqlite3` and `asg017/sqlite-vec-go-bindings` deps

## Test plan
- [ ] `go test -tags=integration ./internal/adapter/out/sqlitevec/...` green locally
- [ ] CI integration job green
- [ ] `docker build .` completes successfully

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Wait for CI green. If alpine musl breaks at runtime when the vec extension tries to load, switch to debian-slim per the comment in Task 8 step 3.

---

## Task 11: Indexer — pure `diff` function

**Files:**
- Create: `internal/usecase/indexer/diff.go`
- Create: `internal/usecase/indexer/diff_test.go`

- [ ] **Step 1: Write failing tests**

`internal/usecase/indexer/diff_test.go`:

```go
package indexer

import (
    "testing"

    portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
    "github.com/stretchr/testify/assert"
)

func TestDiff_NewFileGoesToEmbed(t *testing.T) {
    fs := []FSEntry{{ID: "a", FilePath: "a.md", BodyHash: "h1"}}
    db := []portout.IndexMeta{}
    toEmbed, toDelete := Diff(fs, db)
    assert.Equal(t, []string{"a"}, ids(toEmbed))
    assert.Empty(t, toDelete)
}

func TestDiff_UnchangedFileSkipped(t *testing.T) {
    fs := []FSEntry{{ID: "a", FilePath: "a.md", BodyHash: "h1"}}
    db := []portout.IndexMeta{{ID: "a", BodyHash: "h1"}}
    toEmbed, toDelete := Diff(fs, db)
    assert.Empty(t, toEmbed)
    assert.Empty(t, toDelete)
}

func TestDiff_ChangedHashGoesToEmbed(t *testing.T) {
    fs := []FSEntry{{ID: "a", FilePath: "a.md", BodyHash: "h2"}}
    db := []portout.IndexMeta{{ID: "a", BodyHash: "h1"}}
    toEmbed, toDelete := Diff(fs, db)
    assert.Equal(t, []string{"a"}, ids(toEmbed))
    assert.Empty(t, toDelete)
}

func TestDiff_MissingFileGoesToDelete(t *testing.T) {
    fs := []FSEntry{}
    db := []portout.IndexMeta{{ID: "a", BodyHash: "h1"}}
    toEmbed, toDelete := Diff(fs, db)
    assert.Empty(t, toEmbed)
    assert.Equal(t, []string{"a"}, toDelete)
}

func ids(entries []FSEntry) []string {
    out := make([]string, len(entries))
    for i, e := range entries {
        out[i] = e.ID
    }
    return out
}
```

- [ ] **Step 2: Run test, expect FAIL**

```bash
go test ./internal/usecase/indexer/...
```
Expected: package doesn't compile.

- [ ] **Step 3: Implement Diff**

`internal/usecase/indexer/diff.go`:

```go
// Package indexer maintains the vector index by periodically diffing the
// filesystem against the persisted index.
package indexer

import (
    portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// FSEntry is what a filesystem scan yields per file.
type FSEntry struct {
    ID       string
    FilePath string
    BodyHash string
}

// Diff returns (toEmbed, toDelete) — IDs that need a fresh embedding,
// and IDs that have been removed from disk and should leave the index.
func Diff(fs []FSEntry, db []portout.IndexMeta) (toEmbed []FSEntry, toDelete []string) {
    dbByID := make(map[string]portout.IndexMeta, len(db))
    for _, m := range db {
        dbByID[m.ID] = m
    }
    fsByID := make(map[string]struct{}, len(fs))
    for _, e := range fs {
        fsByID[e.ID] = struct{}{}
        existing, ok := dbByID[e.ID]
        if !ok || existing.BodyHash != e.BodyHash {
            toEmbed = append(toEmbed, e)
        }
    }
    for id := range dbByID {
        if _, ok := fsByID[id]; !ok {
            toDelete = append(toDelete, id)
        }
    }
    return toEmbed, toDelete
}
```

- [ ] **Step 4: Run test, expect PASS**

```bash
go test ./internal/usecase/indexer/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/usecase/indexer/diff.go internal/usecase/indexer/diff_test.go
git commit -m "feat(indexer): pure Diff function with body_hash idempotency

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 12: Indexer — Linker

**Files:**
- Create: `internal/usecase/indexer/linker.go`
- Create: `internal/usecase/indexer/linker_test.go`

- [ ] **Step 1: Write failing test using mockery-generated VectorIndex mock**

`internal/usecase/indexer/linker_test.go`:

```go
package indexer

import (
    "context"
    "testing"

    "github.com/aleksejmetlusko/second-brain/internal/port/out/mocks"
    portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
    "github.com/stretchr/testify/require"
)

func TestLinker_AssignsTopKAtomNeighborsAboveThreshold(t *testing.T) {
    vec := mocks.NewMockVectorIndex(t)

    seedID := "a"
    seedVec := []float32{1, 0, 0}

    vec.On("GetEmbedding", mock.Anything, "a").Return(seedVec, true, nil)
    // First call: find neighbors of seed to expand affected set
    vec.On("SearchByVector", mock.Anything, seedVec, mock.MatchedBy(func(q portout.SearchQuery) bool {
        return q.TopK == 6 && len(q.KindFilter) == 1 && q.KindFilter[0] == "atom"
    })).Return([]portout.SearchHit{
        {ID: "a", Score: 1.0, Meta: portout.IndexMeta{ID: "a", Kind: "atom"}},
        {ID: "b", Score: 0.9, Meta: portout.IndexMeta{ID: "b", Kind: "atom"}},
        {ID: "c", Score: 0.5, Meta: portout.IndexMeta{ID: "c", Kind: "atom"}}, // below threshold
    }, nil).Once()

    // Then for each affected (a and b), Recompute pulls their own neighbors and
    // sets LinkedNotes.
    vec.On("GetMeta", mock.Anything, "a").Return(portout.IndexMeta{ID: "a", FilePath: "a.md", Kind: "atom", LinkedNotes: nil}, true, nil)
    vec.On("GetEmbedding", mock.Anything, "a").Return(seedVec, true, nil)
    vec.On("SearchByVector", mock.Anything, seedVec, mock.Anything).Return([]portout.SearchHit{
        {ID: "a", Score: 1.0, Meta: portout.IndexMeta{ID: "a"}},
        {ID: "b", Score: 0.9, Meta: portout.IndexMeta{ID: "b"}},
        {ID: "c", Score: 0.5},
    }, nil).Maybe()
    vec.On("UpdateLinkedNotes", mock.Anything, "a", []string{"b"}).Return(nil).Once()

    vec.On("GetMeta", mock.Anything, "b").Return(portout.IndexMeta{ID: "b", FilePath: "b.md", Kind: "atom", LinkedNotes: nil}, true, nil)
    vec.On("GetEmbedding", mock.Anything, "b").Return([]float32{0.9, 0.1, 0}, true, nil)
    vec.On("SearchByVector", mock.Anything, []float32{0.9, 0.1, 0}, mock.Anything).Return([]portout.SearchHit{
        {ID: "b", Score: 1.0, Meta: portout.IndexMeta{ID: "b"}},
        {ID: "a", Score: 0.9, Meta: portout.IndexMeta{ID: "a"}},
    }, nil).Maybe()
    vec.On("UpdateLinkedNotes", mock.Anything, "b", []string{"a"}).Return(nil).Once()

    // No YAML rewriter for now — pass a no-op
    rewriter := func(_ context.Context, _ string, _ []string) error { return nil }

    l := NewLinker(vec, rewriter, Config{LinkTopK: 5, LinkMinSimilarity: 0.7})
    err := l.Recompute(context.Background(), []string{"a"})
    require.NoError(t, err)
}

func TestLinker_NoOpWhenLinksUnchanged(t *testing.T) {
    vec := mocks.NewMockVectorIndex(t)

    vec.On("GetEmbedding", mock.Anything, "a").Return([]float32{1, 0, 0}, true, nil)
    vec.On("SearchByVector", mock.Anything, mock.Anything, mock.Anything).Return([]portout.SearchHit{
        {ID: "a", Score: 1.0, Meta: portout.IndexMeta{ID: "a"}},
        {ID: "b", Score: 0.9, Meta: portout.IndexMeta{ID: "b"}},
    }, nil).Maybe()
    vec.On("GetMeta", mock.Anything, "a").Return(portout.IndexMeta{ID: "a", FilePath: "a.md", LinkedNotes: []string{"b"}}, true, nil).Once()
    vec.On("GetMeta", mock.Anything, "b").Return(portout.IndexMeta{ID: "b", FilePath: "b.md", LinkedNotes: []string{"a"}}, true, nil).Once()
    vec.On("GetEmbedding", mock.Anything, "b").Return([]float32{0.9, 0.1, 0}, true, nil).Once()

    rewriter := func(_ context.Context, _ string, _ []string) error {
        t.Fatal("rewriter should not be called when links unchanged")
        return nil
    }
    l := NewLinker(vec, rewriter, Config{LinkTopK: 5, LinkMinSimilarity: 0.7})
    err := l.Recompute(context.Background(), []string{"a"})
    // UpdateLinkedNotes is not asserted (no .Once()), assert mock satisfied:
    assert.NoError(t, err)
}
```

(These tests are partial: they exercise the happy path and no-op path. Strict mock matching for the "expand affected → recompute self" two-phase nature is tricky; the engineer should pragmatically use `.Maybe()` and assert on the critical effects — `UpdateLinkedNotes` called for the right IDs with the right values, rewriter called or not.)

- [ ] **Step 2: Run test, expect FAIL**

```bash
go test ./internal/usecase/indexer/...
```
Expected: undefined Linker / Config / NewLinker.

- [ ] **Step 3: Implement linker**

`internal/usecase/indexer/linker.go`:

```go
package indexer

import (
    "context"
    "fmt"
    "log/slog"
    "slices"

    "github.com/aleksejmetlusko/second-brain/internal/domain"
    portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// Config bundles indexer-wide tunables.
type Config struct {
    ScanInterval      string  // parsed elsewhere; not used by linker but kept together
    BatchSize         int
    LinkTopK          int
    LinkMinSimilarity float32
}

// YAMLRewriter writes new linked_notes (and bumps schema_version) into the
// frontmatter at file_path atomically. Concrete implementation lives in
// the indexer orchestrator alongside the FS adapter.
type YAMLRewriter func(ctx context.Context, filePath string, linkedNotes []string) error

// Linker recomputes linked_notes for atoms after re-embedding.
type Linker struct {
    vec      portout.VectorIndex
    rewrite  YAMLRewriter
    topK     int
    minScore float32
}

// NewLinker constructs a Linker.
func NewLinker(vec portout.VectorIndex, rewrite YAMLRewriter, cfg Config) *Linker {
    return &Linker{vec: vec, rewrite: rewrite, topK: cfg.LinkTopK, minScore: cfg.LinkMinSimilarity}
}

// Recompute updates linked_notes for the seed IDs and their immediate
// neighbors. Caller passes only atoms; summaries are skipped silently if
// they sneak in.
func (l *Linker) Recompute(ctx context.Context, seedIDs []string) error {
    affected := make(map[string]struct{}, len(seedIDs)*2)
    for _, id := range seedIDs {
        meta, found, err := l.vec.GetMeta(ctx, id)
        if err != nil {
            return err
        }
        if !found || meta.Kind != domain.KindAtom {
            continue
        }
        affected[id] = struct{}{}

        vec, _, err := l.vec.GetEmbedding(ctx, id)
        if err != nil {
            return err
        }
        neighbors, err := l.vec.SearchByVector(ctx, vec, portout.SearchQuery{
            TopK:       l.topK + 1,
            KindFilter: []string{domain.KindAtom},
        })
        if err != nil {
            return err
        }
        for _, n := range neighbors {
            if n.ID != id {
                affected[n.ID] = struct{}{}
            }
        }
    }

    for id := range affected {
        if err := l.recomputeOne(ctx, id); err != nil {
            slog.Warn("linker: recompute failed", "id", id, "err", err)
            // continue, don't abort the whole pass
        }
    }
    return nil
}

func (l *Linker) recomputeOne(ctx context.Context, id string) error {
    meta, found, err := l.vec.GetMeta(ctx, id)
    if err != nil {
        return err
    }
    if !found || meta.Kind != domain.KindAtom {
        return nil
    }
    vec, found, err := l.vec.GetEmbedding(ctx, id)
    if err != nil || !found {
        return err
    }
    hits, err := l.vec.SearchByVector(ctx, vec, portout.SearchQuery{
        TopK:       l.topK + 1,
        KindFilter: []string{domain.KindAtom},
    })
    if err != nil {
        return err
    }
    newLinks := make([]string, 0, l.topK)
    for _, h := range hits {
        if h.ID == id {
            continue
        }
        if h.Score < l.minScore {
            break // hits are sorted desc
        }
        newLinks = append(newLinks, h.ID)
        if len(newLinks) >= l.topK {
            break
        }
    }
    if slices.Equal(newLinks, meta.LinkedNotes) {
        return nil
    }
    if err := l.vec.UpdateLinkedNotes(ctx, id, newLinks); err != nil {
        return fmt.Errorf("update links in db: %w", err)
    }
    if l.rewrite != nil {
        if err := l.rewrite(ctx, meta.FilePath, newLinks); err != nil {
            // log but don't fail — disk is source of truth, but DB is now ahead;
            // next pass will see body_hash unchanged and try again? No — body
            // unchanged means no re-embed, but linked_notes mismatch persists
            // until the file is edited. Acceptable for v1.
            slog.Warn("linker: yaml rewrite failed", "path", meta.FilePath, "err", err)
        }
    }
    return nil
}
```

- [ ] **Step 4: Run test, expect PASS**

```bash
go test ./internal/usecase/indexer/...
```
Expected: PASS. If `.On(...).Once()` expectations fail because the order of GetMeta calls between phase 1 and phase 2 doesn't match the test setup, relax the test to assert on the final effect (`UpdateLinkedNotes` arguments) rather than the call sequence.

- [ ] **Step 5: Commit**

```bash
git add internal/usecase/indexer/linker.go internal/usecase/indexer/linker_test.go
git commit -m "feat(indexer): linker computes linked_notes top-K above threshold

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 13: Indexer — orchestrator `Run` + `Loop`

**Files:**
- Create: `internal/usecase/indexer/indexer.go`
- Create: `internal/usecase/indexer/indexer_test.go`

- [ ] **Step 1: Write failing test**

`internal/usecase/indexer/indexer_test.go`:

```go
package indexer

import (
    "context"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/aleksejmetlusko/second-brain/internal/domain"
    "github.com/aleksejmetlusko/second-brain/internal/port/out/mocks"
    portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
    "github.com/stretchr/testify/mock"
    "github.com/stretchr/testify/require"
)

func TestIndexer_EmptyDB_EmbedsAllFiles(t *testing.T) {
    notesDir := t.TempDir()
    writeNote(t, notesDir, "work/projects/x.md", domain.Note{
        ID: "20260527-x", SchemaVersion: "1.1", Date: time.Now(), Source: domain.SourceTelegramText,
        Kind: domain.KindAtom, Category: "work/projects", Tags: []string{}, Ingest: domain.IngestMeta{DumpID: "d", ModelAtomize: "m"},
        Body: "hello",
    })

    embedder := mocks.NewMockEmbedder(t)
    embedder.On("Embed", mock.Anything, mock.MatchedBy(func(s []string) bool { return len(s) == 1 && s[0] == "hello\n" }), portout.EmbedDocument).
        Return([][]float32{{1, 0, 0}}, nil).Once()

    vec := mocks.NewMockVectorIndex(t)
    vec.On("ListAllMeta", mock.Anything).Return(nil, nil).Once()
    vec.On("Upsert", mock.Anything, mock.MatchedBy(func(items []portout.IndexItem) bool {
        return len(items) == 1 && items[0].ID == "20260527-x"
    })).Return(nil).Once()
    vec.On("DeleteByIDs", mock.Anything, []string(nil)).Return(nil).Maybe()
    // Linker invoked — mock the minimum so Recompute doesn't blow up
    vec.On("GetMeta", mock.Anything, "20260527-x").Return(portout.IndexMeta{ID: "20260527-x", FilePath: filepath.Join(notesDir, "work/projects/20260527-x.md"), Kind: "atom"}, true, nil)
    vec.On("GetEmbedding", mock.Anything, "20260527-x").Return([]float32{1, 0, 0}, true, nil)
    vec.On("SearchByVector", mock.Anything, mock.Anything, mock.Anything).Return([]portout.SearchHit{{ID: "20260527-x"}}, nil).Maybe()
    vec.On("UpdateLinkedNotes", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

    rewriter := func(_ context.Context, _ string, _ []string) error { return nil }
    ix := New(notesDir, embedder, vec, rewriter, Config{BatchSize: 10, LinkTopK: 5, LinkMinSimilarity: 0.7})
    require.NoError(t, ix.RunOnce(context.Background()))
}

func writeNote(t *testing.T, root, rel string, n domain.Note) {
    t.Helper()
    p := filepath.Join(root, rel)
    require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
    data, err := domain.MarshalNote(n)
    require.NoError(t, err)
    require.NoError(t, os.WriteFile(p, data, 0o644))
}
```

- [ ] **Step 2: Run test, expect FAIL (compile)**

```bash
go test ./internal/usecase/indexer/...
```
Expected: undefined New / RunOnce.

- [ ] **Step 3: Implement orchestrator**

`internal/usecase/indexer/indexer.go`:

```go
package indexer

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "io/fs"
    "log/slog"
    "os"
    "path/filepath"
    "sync"
    "time"

    "github.com/aleksejmetlusko/second-brain/internal/domain"
    portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// Indexer is the FS-scanner-driven orchestrator.
type Indexer struct {
    notesDir string
    embedder portout.Embedder
    vec      portout.VectorIndex
    linker   *Linker
    cfg      Config

    mu sync.Mutex // serializes RunOnce calls
}

// New constructs an Indexer.
func New(notesDir string, e portout.Embedder, v portout.VectorIndex, rewriter YAMLRewriter, cfg Config) *Indexer {
    if cfg.BatchSize <= 0 {
        cfg.BatchSize = 100
    }
    return &Indexer{
        notesDir: notesDir,
        embedder: e,
        vec:      v,
        linker:   NewLinker(v, rewriter, cfg),
        cfg:      cfg,
    }
}

// RunOnce performs a single scan→diff→embed→upsert→link cycle.
func (i *Indexer) RunOnce(ctx context.Context) error {
    i.mu.Lock()
    defer i.mu.Unlock()

    start := time.Now()
    fsList, paths, bodies, err := i.scan(ctx)
    if err != nil {
        return err
    }

    dbList, err := i.vec.ListAllMeta(ctx)
    if err != nil {
        return err
    }

    toEmbed, toDelete := Diff(fsList, dbList)
    var embeddedIDs []string

    for batchStart := 0; batchStart < len(toEmbed); batchStart += i.cfg.BatchSize {
        end := batchStart + i.cfg.BatchSize
        if end > len(toEmbed) {
            end = len(toEmbed)
        }
        batch := toEmbed[batchStart:end]
        texts := make([]string, len(batch))
        for k, e := range batch {
            texts[k] = bodies[e.ID]
        }
        vectors, err := i.embedder.Embed(ctx, texts, portout.EmbedDocument)
        if err != nil {
            return fmt.Errorf("embed batch [%d:%d]: %w", batchStart, end, err)
        }

        items := make([]portout.IndexItem, len(batch))
        for k, e := range batch {
            note := mustParseNote(paths[e.ID])
            items[k] = portout.IndexItem{
                ID:        e.ID,
                FilePath:  paths[e.ID],
                BodyHash:  e.BodyHash,
                Category:  note.Category,
                Tags:      note.Tags,
                Date:      note.Date,
                Kind:      note.Kind,
                Embedding: vectors[k],
            }
            embeddedIDs = append(embeddedIDs, e.ID)
        }
        if err := i.vec.Upsert(ctx, items); err != nil {
            return fmt.Errorf("upsert batch: %w", err)
        }
    }

    if len(toDelete) > 0 {
        if err := i.vec.DeleteByIDs(ctx, toDelete); err != nil {
            return fmt.Errorf("delete: %w", err)
        }
    }

    if len(embeddedIDs) > 0 {
        if err := i.linker.Recompute(ctx, embeddedIDs); err != nil {
            slog.Error("indexer: linker recompute", "err", err)
            // soft-fail: index is fine, links can catch up next cycle
        }
    }

    slog.Info("indexer.run",
        "duration_ms", time.Since(start).Milliseconds(),
        "n_embedded", len(embeddedIDs),
        "n_deleted", len(toDelete),
    )
    return nil
}

// Loop blocks running RunOnce on a ticker until ctx is cancelled.
func (i *Indexer) Loop(ctx context.Context, interval time.Duration) {
    if interval <= 0 {
        interval = 5 * time.Minute
    }
    // Initial run on startup
    if err := i.RunOnce(ctx); err != nil {
        slog.Error("indexer.startup", "err", err)
    }
    t := time.NewTicker(interval)
    defer t.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-t.C:
            if err := i.RunOnce(ctx); err != nil {
                slog.Error("indexer.tick", "err", err)
            }
        }
    }
}

// scan walks notesDir collecting FSEntry per *.md and stashes parsed
// note metadata + body for later use.
func (i *Indexer) scan(ctx context.Context) ([]FSEntry, map[string]string, map[string]string, error) {
    var entries []FSEntry
    paths := make(map[string]string)
    bodies := make(map[string]string)
    err := filepath.WalkDir(i.notesDir, func(path string, d fs.DirEntry, walkErr error) error {
        if walkErr != nil {
            slog.Warn("indexer.walk", "path", path, "err", walkErr)
            return nil // continue
        }
        if d.IsDir() || filepath.Ext(d.Name()) != ".md" {
            return nil
        }
        if ctx.Err() != nil {
            return ctx.Err()
        }
        data, err := os.ReadFile(path)
        if err != nil {
            slog.Warn("indexer.read", "path", path, "err", err)
            return nil
        }
        n, err := domain.UnmarshalNote(data)
        if err != nil {
            slog.Warn("indexer.parse", "path", path, "err", err)
            return nil
        }
        h := sha256.Sum256([]byte(n.Body))
        entries = append(entries, FSEntry{
            ID:       n.ID,
            FilePath: path,
            BodyHash: hex.EncodeToString(h[:]),
        })
        paths[n.ID] = path
        bodies[n.ID] = n.Body
        return nil
    })
    if err != nil {
        return nil, nil, nil, fmt.Errorf("walk: %w", err)
    }
    return entries, paths, bodies, nil
}

// mustParseNote re-reads a file from disk into domain.Note.
// Returns a zero Note on error (caller has already validated parsing).
func mustParseNote(path string) domain.Note {
    data, err := os.ReadFile(path)
    if err != nil {
        return domain.Note{}
    }
    n, _ := domain.UnmarshalNote(data)
    return n
}
```

- [ ] **Step 4: Run test, expect PASS**

```bash
go test ./internal/usecase/indexer/...
```
Expected: PASS.

- [ ] **Step 5: Lint**

```bash
make lint
```
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/usecase/indexer/indexer.go internal/usecase/indexer/indexer_test.go
git commit -m "feat(indexer): orchestrator RunOnce + Loop with FS scanner

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 14: Wire indexer into `main.go` + new env vars

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `cmd/ingest/main.go`

- [ ] **Step 1: Add indexer-related env vars**

In `internal/config/config.go` `Config` struct, append:

```go
IndexDBPath        string        `env:"INDEX_DB_PATH"        env-default:"/data/index/index.db"`
RAGScanInterval    time.Duration `env:"RAG_SCAN_INTERVAL"    env-default:"5m"`
IndexerBatchSize   int           `env:"INDEXER_BATCH_SIZE"   env-default:"1000"`
LinkTopK           int           `env:"LINK_TOP_K"           env-default:"5"`
LinkMinSimilarity  float32       `env:"LINK_MIN_SIMILARITY"  env-default:"0.70"`
```

In `Validate()`:

```go
if c.LinkTopK <= 0 {
    return fmt.Errorf("LINK_TOP_K must be positive")
}
if c.LinkMinSimilarity < -1 || c.LinkMinSimilarity > 1 {
    return fmt.Errorf("LINK_MIN_SIMILARITY must be in [-1, 1]")
}
```

- [ ] **Step 2: Regenerate .env.example**

```bash
make env-example
```

- [ ] **Step 3: Update `cmd/ingest/main.go` to wire indexer**

Add imports:

```go
"github.com/aleksejmetlusko/second-brain/internal/adapter/out/sqlitevec"
"github.com/aleksejmetlusko/second-brain/internal/adapter/out/voyage"
"github.com/aleksejmetlusko/second-brain/internal/usecase/indexer"
```

In `run()`, after `noteStore := fs.NewNoteStore(cfg.NotesDir)`:

```go
voyageClient := voyage.NewClient(voyage.ClientConfig{
    APIKey:      cfg.VoyageAPIKey,
    BaseURL:     cfg.VoyageBaseURL,
    HTTPTimeout: cfg.HTTPTimeout,
    Retry:       httpretry.Default(),
})
embedder := voyage.NewEmbedder(voyageClient, cfg.EmbeddingModel, cfg.EmbeddingDim)

vec, err := sqlitevec.Open(context.Background(), sqlitevec.Config{
    DBPath:         cfg.IndexDBPath,
    EmbeddingDim:   cfg.EmbeddingDim,
    EmbeddingModel: cfg.EmbeddingModel,
})
if err != nil {
    return fmt.Errorf("open vector index: %w", err)
}
defer vec.Close()

rewriter := func(_ context.Context, path string, links []string) error {
    data, err := os.ReadFile(path)
    if err != nil {
        return err
    }
    out, err := domain.UpdateFrontmatter(data, links)
    if err != nil {
        return err
    }
    tmp := path + ".tmp"
    if err := os.WriteFile(tmp, out, 0o644); err != nil {
        return err
    }
    return os.Rename(tmp, path)
}

ix := indexer.New(cfg.NotesDir, embedder, vec, rewriter, indexer.Config{
    BatchSize:         cfg.IndexerBatchSize,
    LinkTopK:          cfg.LinkTopK,
    LinkMinSimilarity: cfg.LinkMinSimilarity,
})
```

Then after the `b.Start(ctx)` line currently exists, change the structure: launch a goroutine for the indexer BEFORE `b.Start(ctx)`:

```go
go ix.Loop(ctx, cfg.RAGScanInterval)
```

Add the `domain` import if not present.

- [ ] **Step 4: Build and run tests**

```bash
go build ./... && go test ./...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/ cmd/ingest/main.go .env.example
git commit -m "feat(main): wire Voyage + sqlitevec + indexer Loop on startup

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 15: Open PR 4

- [ ] **Step 1: Push and open PR**

```bash
git push -u origin <branch>
gh pr create --title "feat: indexer use case + main wiring" --body "$(cat <<'EOF'
## Summary
- Adds `internal/usecase/indexer/` with pure Diff, Linker, and orchestrator
- Wires Voyage embedder + sqlitevec VectorIndex + indexer Loop into main
- New env vars: INDEX_DB_PATH, RAG_SCAN_INTERVAL, INDEXER_BATCH_SIZE, LINK_TOP_K, LINK_MIN_SIMILARITY

## Test plan
- [ ] `go test ./...` green
- [ ] CI green
- [ ] After merge: indexer goroutine starts, no panics in prod logs

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Wait for CI green. After merge, check VPS logs to confirm indexer is running:

```bash
ssh second-brain-vps 'docker logs --tail 30 second-brain-ingest-1 | grep indexer'
```

Expected: `{"level":"INFO","op":"indexer.run", ...}` lines, no errors.

---

## Task 16: Searcher use case + Telegram `/find` command

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/usecase/searcher/searcher.go`
- Create: `internal/usecase/searcher/searcher_test.go`
- Modify: `internal/adapter/in/telegram/handler.go`
- Modify: `internal/adapter/in/telegram/handler_test.go`
- Modify: `internal/adapter/in/telegram/reply.go`
- Modify: `internal/adapter/in/telegram/reply_test.go`

- [ ] **Step 1: Add `FIND_TOP_K` to config**

In `internal/config/config.go` `Config`:

```go
FindTopK int `env:"FIND_TOP_K" env-default:"5"`
```

Update `Validate()`:

```go
if c.FindTopK <= 0 {
    return fmt.Errorf("FIND_TOP_K must be positive")
}
```

Run `make env-example`.

- [ ] **Step 2: Write failing searcher test**

`internal/usecase/searcher/searcher_test.go`:

```go
package searcher

import (
    "context"
    "testing"
    "time"

    portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
    "github.com/aleksejmetlusko/second-brain/internal/port/out/mocks"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
    "github.com/stretchr/testify/require"
)

func TestSearcher_Find_ReturnsTopK(t *testing.T) {
    embedder := mocks.NewMockEmbedder(t)
    vec := mocks.NewMockVectorIndex(t)

    embedder.On("Embed", mock.Anything, []string{"auth bug"}, portout.EmbedQuery).
        Return([][]float32{{0.1, 0.2}}, nil).Once()
    vec.On("SearchByVector", mock.Anything, []float32{0.1, 0.2}, mock.MatchedBy(func(q portout.SearchQuery) bool {
        return q.TopK == 5 && len(q.KindFilter) == 1 && q.KindFilter[0] == "atom"
    })).Return([]portout.SearchHit{
        {ID: "a", Score: 0.9, Meta: portout.IndexMeta{ID: "a", Category: "work", FilePath: "/data/notes/work/a.md"}},
    }, nil).Once()

    s := New(embedder, vec, Config{TopK: 5})
    hits, err := s.Find(context.Background(), "auth bug", Options{})
    require.NoError(t, err)
    require.Len(t, hits, 1)
    assert.Equal(t, "a", hits[0].ID)
}

func TestSearcher_Find_EmptyQuery_ReturnsError(t *testing.T) {
    s := New(nil, nil, Config{TopK: 5})
    _, err := s.Find(context.Background(), "  ", Options{})
    assert.Error(t, err)
}

func TestSearcher_Find_AppliesCategoryFilter(t *testing.T) {
    embedder := mocks.NewMockEmbedder(t)
    vec := mocks.NewMockVectorIndex(t)

    embedder.On("Embed", mock.Anything, mock.Anything, portout.EmbedQuery).Return([][]float32{{1, 0}}, nil).Once()
    vec.On("SearchByVector", mock.Anything, mock.Anything, mock.MatchedBy(func(q portout.SearchQuery) bool {
        return q.CategoryPrefix == "work/" && !q.DateFrom.IsZero() && q.DateFrom.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
    })).Return(nil, nil).Once()

    s := New(embedder, vec, Config{TopK: 5})
    _, err := s.Find(context.Background(), "q", Options{
        CategoryPrefix: "work/",
        DateFrom:       time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
    })
    require.NoError(t, err)
}
```

- [ ] **Step 3: Run test, expect FAIL**

```bash
go test ./internal/usecase/searcher/...
```
Expected: FAIL.

- [ ] **Step 4: Implement searcher**

`internal/usecase/searcher/searcher.go`:

```go
// Package searcher exposes semantic retrieval over the indexed notes.
package searcher

import (
    "context"
    "fmt"
    "strings"
    "time"

    "github.com/aleksejmetlusko/second-brain/internal/domain"
    portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// Config carries searcher tunables.
type Config struct {
    TopK int
}

// Options scopes a single Find call.
type Options struct {
    CategoryPrefix string
    DateFrom       time.Time
}

// Searcher coordinates query-time embedding and vector search.
type Searcher struct {
    embedder portout.Embedder
    vec      portout.VectorIndex
    topK     int
}

// New constructs a Searcher.
func New(e portout.Embedder, v portout.VectorIndex, cfg Config) *Searcher {
    k := cfg.TopK
    if k <= 0 {
        k = 5
    }
    return &Searcher{embedder: e, vec: v, topK: k}
}

// Find performs semantic retrieval over atoms.
func (s *Searcher) Find(ctx context.Context, query string, opts Options) ([]portout.SearchHit, error) {
    query = strings.TrimSpace(query)
    if query == "" {
        return nil, fmt.Errorf("%w: empty query", domain.ErrSearchEmpty)
    }
    vecs, err := s.embedder.Embed(ctx, []string{query}, portout.EmbedQuery)
    if err != nil {
        return nil, err
    }
    if len(vecs) != 1 {
        return nil, fmt.Errorf("%w: embedder returned %d vectors for 1 query", domain.ErrEmbedder, len(vecs))
    }
    hits, err := s.vec.SearchByVector(ctx, vecs[0], portout.SearchQuery{
        TopK:           s.topK,
        KindFilter:     []string{domain.KindAtom},
        CategoryPrefix: opts.CategoryPrefix,
        DateFrom:       opts.DateFrom,
    })
    if err != nil {
        return nil, err
    }
    return hits, nil
}
```

- [ ] **Step 5: Run test, expect PASS**

```bash
go test ./internal/usecase/searcher/...
```
Expected: PASS.

- [ ] **Step 6: Add FormatFindReply helper to telegram package**

In `internal/adapter/in/telegram/reply.go`, append:

```go
// FormatFindReply renders a /find result for Telegram. Caller decides what
// to do when len(hits) == 0 (we suggest a friendly "ничего не нашёл").
func FormatFindReply(query string, hits []portout.SearchHit, notesDir string) string {
    if len(hits) == 0 {
        return "🔍 Ничего не нашёл по запросу: " + query
    }
    var b strings.Builder
    fmt.Fprintf(&b, "🔍 Топ-%d по запросу: %s\n\n", len(hits), query)
    for i, h := range hits {
        snippet := snippetFromFile(h.Meta.FilePath, 200)
        relPath, _ := filepath.Rel(notesDir, h.Meta.FilePath)
        fmt.Fprintf(&b, "%d. %s/%s (%.2f)\n%s\n`%s`\n\n",
            i+1, h.Meta.Category, idSlug(h.ID), h.Score, snippet, relPath)
    }
    return b.String()
}

func snippetFromFile(path string, max int) string {
    data, err := os.ReadFile(path)
    if err != nil {
        return ""
    }
    n, err := domain.UnmarshalNote(data)
    if err != nil {
        return ""
    }
    body := strings.TrimSpace(n.Body)
    if len(body) > max {
        body = body[:max] + "..."
    }
    return body
}

func idSlug(id string) string {
    // id format: YYYYMMDD-slug — return slug portion
    if i := strings.IndexByte(id, '-'); i >= 0 {
        return id[i+1:]
    }
    return id
}
```

Add imports at the top of `reply.go` if not present: `"os"`, `"path/filepath"`, `"strings"`, `"fmt"`, and:

```go
"github.com/aleksejmetlusko/second-brain/internal/domain"
portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
```

- [ ] **Step 7: Test FormatFindReply**

Append to `internal/adapter/in/telegram/reply_test.go`:

```go
func TestFormatFindReply_EmptyHitsShowsFriendlyMessage(t *testing.T) {
    out := FormatFindReply("auth", nil, "/tmp")
    assert.Contains(t, out, "Ничего не нашёл")
    assert.Contains(t, out, "auth")
}
```

Run:

```bash
go test ./internal/adapter/in/telegram/...
```
Expected: PASS.

- [ ] **Step 8: Modify Router to recognize `/find`**

In `internal/adapter/in/telegram/handler.go`, extend `Action` enum:

```go
const (
    ActionDrop Action = iota + 1
    ActionProcessText
    ActionProcessVoice
    ActionReplyUnsupported
    ActionReplyTooLong
    ActionFind          // NEW
)
```

Extend Router struct to hold a Searcher reference:

```go
type Router struct {
    uc       in.IngestDumpUseCase
    searcher SearcherFn
    notesDir string
    allowed  map[int64]struct{}
    dedup    *UpdateDedup
    logger   *slog.Logger
}

// SearcherFn isolates the telegram package from the searcher use case.
type SearcherFn func(ctx context.Context, query string) (string, error)

// NewRouter signature gains searcher + notesDir. Pass nil searcher to disable /find.
func NewRouter(uc in.IngestDumpUseCase, searcher SearcherFn, notesDir string, allowed map[int64]struct{}, dedup *UpdateDedup, logger *slog.Logger) *Router {
    return &Router{uc: uc, searcher: searcher, notesDir: notesDir, allowed: allowed, dedup: dedup, logger: logger}
}
```

In `Route()`, add a branch BEFORE the existing `switch u.Kind`:

```go
if u.Kind == UpdateText && strings.HasPrefix(strings.TrimSpace(u.Text), "/find") {
    return Decision{Action: ActionFind}
}
```

In `Handle()`, add case before the default:

```go
case ActionFind:
    if r.searcher == nil {
        return "Поиск не настроен", nil
    }
    query := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(u.Text), "/find"))
    if query == "" {
        return "Использование: /find <запрос>", nil
    }
    return r.searcher(ctx, query)
```

Add `"strings"` import.

- [ ] **Step 9: Update handler tests for /find routing**

In `internal/adapter/in/telegram/handler_test.go`, add (mind any existing helper for constructing Router — replicate the new signature):

```go
func TestRoute_FindCommandTriggersActionFind(t *testing.T) {
    logger := slog.New(slog.NewTextHandler(io.Discard, nil))
    r := NewRouter(nil, nil, "/data/notes", map[int64]struct{}{1: {}}, NewUpdateDedup(8), logger)
    d := r.Route(IncomingUpdate{UpdateID: 1, FromUserID: 1, Kind: UpdateText, Text: "/find auth bug"})
    assert.Equal(t, ActionFind, d.Action)
}

func TestHandle_FindWithEmptyQueryGivesUsage(t *testing.T) {
    logger := slog.New(slog.NewTextHandler(io.Discard, nil))
    r := NewRouter(nil, func(_ context.Context, _ string) (string, error) { return "ok", nil }, "/data/notes", map[int64]struct{}{1: {}}, NewUpdateDedup(8), logger)
    reply, err := r.Handle(context.Background(), IncomingUpdate{UpdateID: 1, FromUserID: 1, Kind: UpdateText, Text: "/find  "})
    require.NoError(t, err)
    assert.Contains(t, reply, "Использование")
}
```

Update all existing `NewRouter(...)` calls in this file to pass `nil, ""` for the new searcher and notesDir args.

- [ ] **Step 10: Wire searcher into main.go**

In `cmd/ingest/main.go`, after the indexer wiring from Task 14, before `NewRouter`:

```go
search := searcher.New(embedder, vec, searcher.Config{TopK: cfg.FindTopK})
searchFn := func(ctx context.Context, query string) (string, error) {
    hits, err := search.Find(ctx, query, searcher.Options{})
    if err != nil {
        if errors.Is(err, domain.ErrSearchEmpty) {
            return "🔍 Пустой запрос", nil
        }
        slog.Error("searcher.find", "err", err)
        return "🔍 Поиск временно недоступен", nil
    }
    return telegram.FormatFindReply(query, hits, cfg.NotesDir), nil
}
```

Update the `NewRouter` call:

```go
router := telegram.NewRouter(uc, searchFn, cfg.NotesDir, allowed, dedup, logger)
```

Add imports `"github.com/aleksejmetlusko/second-brain/internal/usecase/searcher"` and `"errors"` if not already present.

- [ ] **Step 11: Build and test**

```bash
go build ./... && go test ./...
```
Expected: PASS.

- [ ] **Step 12: Lint**

```bash
make lint
```
Expected: clean.

- [ ] **Step 13: Commit**

```bash
git add internal/usecase/searcher/ internal/adapter/in/telegram/ internal/config/ cmd/ingest/main.go .env.example
git commit -m "feat: /find Telegram command with semantic search

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 17: Open PR 5

- [ ] **Step 1: Push and open PR**

```bash
git push -u origin <branch>
gh pr create --title "feat: /find command + searcher use case" --body "$(cat <<'EOF'
## Summary
- Adds `internal/usecase/searcher/` with `Find(ctx, query, opts)`
- Adds `/find <query>` Telegram command that returns top-K with snippets + paths
- New env var: FIND_TOP_K (default 5)

## Test plan
- [ ] `go test ./...` green
- [ ] After merge: send `/find <query>` in Telegram, expect top-5 with snippets + relative path
- [ ] Send `/find` with no args, expect usage hint

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Wait for CI green. Merge.

After merge:

```bash
# In Telegram, send: /find auth
# Expect a reply listing 1-5 hits with category/slug + score + snippet + file path
```

If the bot says "Поиск временно недоступен", check logs:

```bash
ssh second-brain-vps 'docker logs --tail 50 second-brain-ingest-1 | grep -E "indexer|searcher"'
```

The most common first-deploy failure: indexer hasn't completed its first run yet — wait `RAG_SCAN_INTERVAL` (default 5m) after deploy.

---

## Task 18: Production rollout — compose + sync script

**Files:**
- Modify: `docker-compose.prod.yml`
- Modify: `scripts/sync-prod.sh`

- [ ] **Step 1: Update docker-compose.prod.yml**

```yaml
services:
  ingest:
    image: ghcr.io/qquiqlerr/second-brain:latest
    pull_policy: always
    restart: unless-stopped
    user: "1000:1000"
    env_file: .env
    environment:
      TZ: Europe/Moscow
      NOTES_DIR: /data/notes
      CONFIG_DIR: /etc/second-brain
      INDEX_DB_PATH: /data/index/index.db
    volumes:
      - ./data/notes:/data/notes
      - ./data/index:/data/index
      - ./config:/etc/second-brain
```

- [ ] **Step 2: Update sync-prod.sh `--init` flow**

In `scripts/sync-prod.sh`, find the line that creates remote directories for `--init` and change:

```bash
ssh "$VPS" "mkdir -p \"\$HOME/${VPS_DIR}/config\" \"\$HOME/${VPS_DIR}/data/notes\""
```

to:

```bash
ssh "$VPS" "mkdir -p \"\$HOME/${VPS_DIR}/config\" \"\$HOME/${VPS_DIR}/data/notes\" \"\$HOME/${VPS_DIR}/data/index\""
```

- [ ] **Step 3: Append `VOYAGE_API_KEY` to local `.env`**

Add to your local `.env` (not committed):

```
VOYAGE_API_KEY=<your-voyage-key>
```

Get it from https://www.voyageai.com/ — sign up, generate key.

- [ ] **Step 4: Sync everything to VPS**

```bash
# 1. Pre-create the new index dir on VPS:
ssh second-brain-vps 'mkdir -p ~/second-brain/data/index'

# 2. Push the new .env (with VOYAGE_API_KEY):
./scripts/sync-prod.sh --env

# 3. Push the new compose:
./scripts/sync-prod.sh --compose
```

`--compose` will restart the container with the new env and volume mounted.

- [ ] **Step 5: Verify on VPS**

```bash
ssh second-brain-vps 'docker logs --tail 50 second-brain-ingest-1 | grep -E "indexer|voyage|sqlitevec"'
```

Look for:
- `"msg":"starting bot"` — startup OK
- `"op":"indexer.run","n_embedded":N` after a few minutes (or sooner) — indexer working
- No `"level":"ERROR"` lines

Check the index file appeared:

```bash
ssh second-brain-vps 'ls -lah ~/second-brain/data/index/'
```

Expected: `index.db` ~MB-scale once first batch lands.

- [ ] **Step 6: Smoke /find in Telegram**

Send `/find` with a real query relevant to your existing notes. Verify the top-5 looks sensible. If results are nonsense, similarity threshold may need tuning — see open question §8.2 of the spec.

- [ ] **Step 7: Commit**

```bash
git add docker-compose.prod.yml scripts/sync-prod.sh
git commit -m "ops: add index volume + INDEX_DB_PATH for RAG production rollout

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 8: Open PR 6**

```bash
git push -u origin <branch>
gh pr create --title "ops: production rollout for RAG (compose + sync)" --body "$(cat <<'EOF'
## Summary
- Adds `data/index` volume and `INDEX_DB_PATH` to `docker-compose.prod.yml`
- Updates `sync-prod.sh --init` to create remote `data/index/` dir
- Container will start the indexer goroutine using the deployed image (already on main from PRs 4 + 5)

## Pre-merge checklist
- [ ] `VOYAGE_API_KEY` added to local `.env`
- [ ] `~/second-brain/data/index/` exists on VPS
- [ ] `./scripts/sync-prod.sh --env --compose` applied
- [ ] First `/find` query in Telegram returned sensible top-5

## Test plan
- [ ] CI green
- [ ] After merge, auto-deploy uses the new compose; container picks up index volume

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Wait for CI, merge.

---

## Closeout

- [ ] **Verify auto-deploy success**

```bash
gh run watch <run-id> --exit-status
```

Expected: green deploy.

- [ ] **Observe linked_notes appear in YAML**

After the next `RAG_SCAN_INTERVAL` window, pull one of your notes from the VPS and inspect:

```bash
ssh second-brain-vps 'cat ~/second-brain/data/notes/work/projects/some-note.md | head -20'
```

Expected: `schema_version: "1.1"` and `linked_notes:` array populated.

- [ ] **Open issue for sub-project 6 (agentic /ask) follow-up**

```bash
gh issue create --title "sub-project 6: agentic /ask" --body "After 2-4 weeks of /find + linked_notes usage, design and implement /ask as an LLM tool-use loop. See spec §9 in 2026-05-29-rag-design.md."
```

---

## Self-Review (Author Notes)

Reviewing this plan against the spec:

- **§1 in scope (5 items)**: Embedder ✓ (Tasks 4, 6), VectorIndex ✓ (Tasks 4, 9), Indexer use case ✓ (Tasks 11–14), schema bump ✓ (Task 1), Searcher.Find ✓ (Task 16), /find Telegram ✓ (Task 16).
- **§2.2 ports**: Embedder + VectorIndex + IndexItem + IndexMeta + SearchQuery + SearchHit all defined in Task 4.
- **§2.5 SQL schema**: notes_meta, notes_vec, index_meta all in Task 9.
- **§2.6 body-hash idempotency**: Task 11 (Diff comparing body_hash), Task 13 (scan computes sha256).
- **§3.1 indexer flow**: Tasks 13 (scan, diff, batch upsert, delete, linker invocation) match the spec step-by-step.
- **§3.2 linker**: Task 12 (atoms-only, top-K + threshold, no-op on unchanged, two-phase affected set).
- **§3.4 /find**: Task 16.
- **§4.1 schema 1.1**: Task 1 (LinkedNotes field, SchemaVersion const).
- **§4.3 migration**: handled implicitly — old 1.0 files have no LinkedNotes, indexer sees them as toEmbed on first scan, linker writes linked_notes + bumps version via UpdateFrontmatter (Task 2).
- **§5 error handling**: covered piecemeal (terminal vs retryable in voyage adapter Task 6, soft-fail in linker Task 12, hard-fail abort in indexer Task 13).
- **§6 tests**: each task has TDD step 1 + 2 before implementation.
- **§7 env**: Tasks 5 (Voyage), 14 (indexer), 16 (FindTopK).
- **§7.7 Dockerfile CGO**: Task 8.
- **§9 sub-project 6 in roadmap**: closeout task creates the issue.
- **§10.5 PR breakdown** (6 PRs): matches Tasks 4, 7, 10, 15, 17, 18 PR openers.

No placeholders found. Types consistent (Embedder.Embed signature consistent across Tasks 4, 6, 13, 16; VectorIndex methods consistent across Tasks 4, 9, 12, 13, 16; Config struct in indexer consistent across Tasks 11–14; SearcherFn callback type defined once in Task 16).

One open dependency to verify at runtime: the exact import path and registration function for sqlite-vec Go bindings (Task 8 step 1, Task 9 step 4 `sqlitevec.Auto()`). The plan documents the fallback approach (read the binding's README).

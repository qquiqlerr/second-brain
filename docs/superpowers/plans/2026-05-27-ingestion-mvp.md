# Ingestion MVP — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Before writing any Go code:** invoke `modern-go-guidelines:use-modern-go` skill via the Skill tool. The project targets Go 1.26 — use modern syntax (`cmp.Or`, `slices.*`, `t.Context()`, `errors.Join`, `errors.AsType[T]`, `new(val)`, `strings.SplitSeq`, `wg.Go()`, etc.).

**Goal:** Стоит ingestion-сервис, который превращает Telegram-сообщения (текст/голос) в атомарные Markdown-заметки на диске с валидацией по `taxonomy.yml`.

**Architecture:** Hexagonal (ports & adapters). Pure `domain/` (zero external deps) описывает сущности и валидацию. `usecase/` оркеструет pipeline (transcribe → atomize → validate → store) через `port.out` интерфейсы. Driving adapter `telegram/` + driven adapters `openrouter/` (chat + multipart whisper) и `fs/`. Композиция — в `cmd/ingest/main.go`.

**Tech Stack:** Go 1.26, OpenRouterTeam/go-sdk, go-telegram/bot, ilyakaznacheev/cleanenv, vektra/mockery v3, gopkg.in/yaml.v3, gosimple/slug, google/uuid, log/slog, testify, Docker.

**Spec:** `docs/superpowers/specs/2026-05-27-ingestion-mvp-design.md` — единственный источник истины по требованиям; ссылайся на конкретные секции в коммитах когда уместно.

---

## Task 1: Project bootstrap (go.mod, Makefile, golangci-lint, .gitignore)

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `.golangci.yml`
- Create: `.editorconfig`
- Create: `Makefile`
- Create: `README.md` (заглушка, расширим в финальной задаче)

- [ ] **Step 1: Инициализировать модуль и git**

Run:
```bash
git init
go mod init github.com/aleksejmetlusko/second-brain
```

- [ ] **Step 2: Создать `.gitignore`**

```
# binaries
/bin/
/dist/
ingest

# IDE
.idea/
.vscode/
*.swp

# env / secrets
.env
.env.local

# data & runtime
/data/
/config/taxonomy.yml

# test artifacts
coverage.out
*.test
```

- [ ] **Step 3: Создать `.editorconfig`**

```
root = true

[*]
charset = utf-8
end_of_line = lf
indent_style = tab
insert_final_newline = true
trim_trailing_whitespace = true

[*.{yml,yaml,md,json}]
indent_style = space
indent_size = 2
```

- [ ] **Step 4: Создать `.golangci.yml`**

```yaml
version: "2"

run:
  timeout: 3m

linters:
  default: none
  enable:
    - errcheck
    - govet
    - ineffassign
    - staticcheck
    - unused
    - gocritic
    - revive
    - misspell
    - gofmt
    - goimports
    - errorlint
    - nilerr
    - nilnil
    - bodyclose
    - contextcheck
    - copyloopvar
    - prealloc

issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - errcheck
        - revive
```

- [ ] **Step 5: Создать `Makefile`**

```makefile
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
```

- [ ] **Step 6: Минимальный README (расширим в финальной задаче)**

`README.md`:
````markdown
# Second Brain — Ingestion MVP

Persoнальный «второй мозг»: Telegram-бот превращает голос/текст в атомарные Markdown-заметки с YAML-фронтматтером.

## Setup

```bash
make tidy
make mocks
make test
```

See `docs/superpowers/specs/2026-05-27-ingestion-mvp-design.md` for full design.
````

- [ ] **Step 7: Запустить и проверить**

Run:
```bash
go mod tidy
make lint || echo "lint skipped (no Go files yet)"
```

Expected: `go.mod` создан, `tidy` без ошибок (нет зависимостей), `lint` ничего не находит — это норм для пустого репозитория.

- [ ] **Step 8: Commit**

```bash
git add .gitignore .editorconfig .golangci.yml Makefile README.md go.mod
git commit -m "chore: project bootstrap (go.mod, Makefile, linters, README)"
```

---

## Task 2: Mockery конфигурация

**Files:**
- Create: `.mockery.yml`

- [ ] **Step 1: Создать `.mockery.yml`**

```yaml
all: false
disable-version-string: true
exclude-regex: ""
keeptree: false
issue-845-fix: true
resolve-type-alias: false
with-expecter: true
filename: "mock_{{.InterfaceName}}.go"
mockname: "Mock{{.InterfaceName}}"
outpkg: "mocks"
template: testify
packages:
  github.com/aleksejmetlusko/second-brain/internal/port/in:
    config:
      dir: "internal/port/in/mocks"
    interfaces:
      IngestDumpUseCase:
  github.com/aleksejmetlusko/second-brain/internal/port/out:
    config:
      dir: "internal/port/out/mocks"
    interfaces:
      Transcriber:
      Atomizer:
      NoteStore:
      TaxonomyLoader:
```

- [ ] **Step 2: Установить mockery v3 как dev-tool через `tools.go`**

Create `tools/tools.go`:
```go
//go:build tools

package tools

import (
	_ "github.com/vektra/mockery/v3"
)
```

Run:
```bash
go get -tool github.com/vektra/mockery/v3@latest
go mod tidy
```

- [ ] **Step 3: Verify mockery installed**

Run:
```bash
go tool mockery --version
```

Expected: версия v3.x.x

- [ ] **Step 4: Commit**

```bash
git add .mockery.yml tools/tools.go go.mod go.sum
git commit -m "chore: add mockery v3 config and tool dependency"
```

---

## Task 3: Domain types (Note, DumpSource, IngestMeta, errors)

**Files:**
- Create: `internal/domain/note.go`
- Create: `internal/domain/errors.go`
- Test: `internal/domain/note_test.go`

- [ ] **Step 1: Написать тест на структуру Note (round-trip создания)**

`internal/domain/note_test.go`:
```go
package domain_test

import (
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestNote_FieldsRoundTrip(t *testing.T) {
	date := time.Date(2026, 5, 27, 22, 40, 0, 0, time.UTC)
	n := domain.Note{
		ID:            "20260527-tms-auth-bug",
		SchemaVersion: domain.SchemaVersion,
		Date:          date,
		Source:        domain.SourceTelegramVoice,
		Category:      "work/projects/tms",
		Tags:          []string{"bug", "auth"},
		Slug:          "tms-auth-bug",
		Body:          "Body text",
		Ingest: domain.IngestMeta{
			DumpID:          "uuid-1",
			ModelAtomize:    "anthropic/claude-3.5-haiku",
			ModelTranscribe: "openai/whisper-1",
		},
	}
	require.Equal(t, "1.0", n.SchemaVersion)
	require.Equal(t, domain.DumpSource("telegram-voice"), n.Source)
}
```

- [ ] **Step 2: Run test (должен упасть на отсутствии типов)**

Run: `go test ./internal/domain/...`
Expected: FAIL — `undefined: domain.Note`.

- [ ] **Step 3: Реализовать `internal/domain/note.go`**

```go
package domain

import "time"

// SchemaVersion is the YAML frontmatter schema version produced by MVP.
const SchemaVersion = "1.0"

// CategoryUncategorized is the fallback category used when an LLM proposes
// a category that does not pass the taxonomy whitelist.
const CategoryUncategorized = "uncategorized"

// DumpSource identifies the origin of a dump entering the pipeline.
type DumpSource string

const (
	SourceTelegramText  DumpSource = "telegram-text"
	SourceTelegramVoice DumpSource = "telegram-voice"
)

// IngestMeta records pipeline trace information attached to every note.
type IngestMeta struct {
	DumpID          string `yaml:"dump_id"`
	ModelAtomize    string `yaml:"model_atomize"`
	ModelTranscribe string `yaml:"model_transcribe,omitempty"`
}

// Note is a single atomic thought as stored in the filesystem.
// Slug and Body are internal: Slug feeds into ID + filename, Body becomes
// the markdown content after the YAML frontmatter.
type Note struct {
	ID               string     `yaml:"id"`
	SchemaVersion    string     `yaml:"schema_version"`
	Date             time.Time  `yaml:"date"`
	Source           DumpSource `yaml:"source"`
	Category         string     `yaml:"category"`
	OriginalCategory string     `yaml:"original_category,omitempty"`
	Tags             []string   `yaml:"tags"`
	Ingest           IngestMeta `yaml:"ingest"`
	Slug             string     `yaml:"-"`
	Body             string     `yaml:"-"`
}
```

- [ ] **Step 4: Создать `internal/domain/errors.go`**

```go
package domain

import "errors"

// Sentinel errors returned by domain logic and the use case.
var (
	ErrEmptyDump           = errors.New("dump is empty after transcription")
	ErrTaxonomyMissing     = errors.New("taxonomy.yml not found")
	ErrTaxonomyMalformed   = errors.New("taxonomy.yml is malformed")
	ErrAtomizerBadResponse = errors.New("atomizer returned invalid response")
	ErrAtomizerNoNotes     = errors.New("atomizer returned zero notes")
)
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/domain/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/note.go internal/domain/errors.go internal/domain/note_test.go
git commit -m "feat(domain): add Note, DumpSource, IngestMeta types and sentinel errors"
```

---

## Task 4: Slug normalization

**Files:**
- Create: `internal/domain/slug.go`
- Test: `internal/domain/slug_test.go`

- [ ] **Step 1: Написать тесты**

`internal/domain/slug_test.go`:
```go
package domain_test

import (
	"strings"
	"testing"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"TMS Auth Bug", "tms-auth-bug"},
		{"  spaces  around  ", "spaces-around"},
		{"моя идея", "moia-ideia"},
		{"привет, мир!", "privet-mir"},
		{"already-kebab", "already-kebab"},
		{"!!@@##", "n-a"},
		{"", "n-a"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			require.Equal(t, c.want, domain.Slugify(c.in))
		})
	}
}

func TestSlugify_LengthCap(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := domain.Slugify(long)
	require.LessOrEqual(t, len(got), 40)
}
```

- [ ] **Step 2: Run test, ожидать FAIL**

Run: `go test ./internal/domain/...`
Expected: FAIL — `undefined: domain.Slugify`.

- [ ] **Step 3: Добавить зависимость `gosimple/slug`**

Run: `go get github.com/gosimple/slug@latest`

- [ ] **Step 4: Реализовать `internal/domain/slug.go`**

```go
package domain

import (
	"github.com/gosimple/slug"
)

// MaxSlugLength caps the slug to keep filenames manageable on all filesystems.
const MaxSlugLength = 40

// FallbackSlug is used when input normalizes to an empty string.
const FallbackSlug = "n-a"

// Slugify normalizes a free-form title into an ASCII kebab-case slug.
// It transliterates non-ASCII characters (e.g. Cyrillic), lowercases,
// replaces whitespace and punctuation with hyphens, and caps the length.
func Slugify(s string) string {
	out := slug.Make(s)
	if out == "" {
		return FallbackSlug
	}
	if len(out) > MaxSlugLength {
		out = out[:MaxSlugLength]
		// trim trailing hyphen if cut landed on it
		for len(out) > 0 && out[len(out)-1] == '-' {
			out = out[:len(out)-1]
		}
	}
	return out
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/domain/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/slug.go internal/domain/slug_test.go go.mod go.sum
git commit -m "feat(domain): add Slugify with cyrillic translit and length cap"
```

---

## Task 5: BuildID

**Files:**
- Create: `internal/domain/id.go`
- Test: `internal/domain/id_test.go`

- [ ] **Step 1: Написать тесты**

`internal/domain/id_test.go`:
```go
package domain_test

import (
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestBuildID(t *testing.T) {
	moscow, _ := time.LoadLocation("Europe/Moscow")
	cases := []struct {
		name string
		date time.Time
		slug string
		want string
	}{
		{"basic", time.Date(2026, 5, 27, 22, 40, 0, 0, moscow), "tms-auth-bug", "20260527-tms-auth-bug"},
		{"midnight-moscow", time.Date(2026, 1, 1, 0, 0, 0, 0, moscow), "new-year", "20260101-new-year"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, domain.BuildID(c.date, c.slug))
		})
	}
}
```

- [ ] **Step 2: Run, ожидать FAIL**

Run: `go test ./internal/domain/...`
Expected: FAIL — `undefined: domain.BuildID`.

- [ ] **Step 3: Реализовать `internal/domain/id.go`**

```go
package domain

import (
	"fmt"
	"time"
)

// BuildID derives the canonical note identifier: YYYYMMDD-slug, using the
// date as-is (caller must convert to the target timezone first).
func BuildID(date time.Time, slug string) string {
	return fmt.Sprintf("%s-%s", date.Format("20060102"), slug)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/domain/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/id.go internal/domain/id_test.go
git commit -m "feat(domain): add BuildID for YYYYMMDD-slug identifiers"
```

---

## Task 6: Taxonomy — Load + AllCategoryPaths + CategoryAllowed

**Files:**
- Create: `internal/domain/taxonomy.go`
- Test: `internal/domain/taxonomy_test.go`
- Create: `testdata/taxonomy.yml.example`

- [ ] **Step 1: Создать `testdata/taxonomy.yml.example`** (стартовый шаблон)

```yaml
version: "1.0"
categories:
  work:
    projects:
      - tms
      - ingestion
    meetings:
    notes:
  personal:
    learning:
    finance:
    relationships:
  health:
    workouts:
    nutrition:
    sleep:
tags:
  - appsheet
  - backend
  - frontend
  - auth
  - bug
  - refactor
  - performance
  - idea
  - todo
  - insight
  - question
  - decision
  - mood
```

- [ ] **Step 2: Написать тесты на Load + AllCategoryPaths + CategoryAllowed**

`internal/domain/taxonomy_test.go`:
```go
package domain_test

import (
	"os"
	"slices"
	"testing"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func loadTaxonomyFile(t *testing.T) domain.Taxonomy {
	t.Helper()
	data, err := os.ReadFile("../../testdata/taxonomy.yml.example")
	require.NoError(t, err)
	tax, err := domain.LoadTaxonomy(data)
	require.NoError(t, err)
	return tax
}

func TestLoadTaxonomy_Malformed(t *testing.T) {
	_, err := domain.LoadTaxonomy([]byte(":\nthis is not yaml"))
	require.ErrorIs(t, err, domain.ErrTaxonomyMalformed)
}

func TestTaxonomy_AllCategoryPaths(t *testing.T) {
	tax := loadTaxonomyFile(t)
	paths := tax.AllCategoryPaths()

	expected := []string{
		"work",
		"work/projects",
		"work/projects/tms",
		"work/projects/ingestion",
		"work/meetings",
		"work/notes",
		"personal",
		"personal/learning",
		"personal/finance",
		"personal/relationships",
		"health",
		"health/workouts",
		"health/nutrition",
		"health/sleep",
	}
	for _, p := range expected {
		require.Truef(t, slices.Contains(paths, p), "missing path %q", p)
	}
}

func TestTaxonomy_CategoryAllowed(t *testing.T) {
	tax := loadTaxonomyFile(t)
	require.True(t, tax.CategoryAllowed("work"))
	require.True(t, tax.CategoryAllowed("work/projects"))
	require.True(t, tax.CategoryAllowed("work/projects/tms"))
	require.True(t, tax.CategoryAllowed("health/sleep"))
	require.False(t, tax.CategoryAllowed("crypto/defi"))
	require.False(t, tax.CategoryAllowed(""))
	require.False(t, tax.CategoryAllowed("WORK")) // case-sensitive by design
}
```

- [ ] **Step 3: Run, ожидать FAIL**

Run: `go test ./internal/domain/...`
Expected: FAIL — `undefined: domain.Taxonomy`.

- [ ] **Step 4: Добавить yaml.v3**

Run: `go get gopkg.in/yaml.v3`

- [ ] **Step 5: Реализовать `internal/domain/taxonomy.go`**

```go
package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Taxonomy holds the immutable whitelist of allowed categories and tags
// parsed from taxonomy.yml. Use LoadTaxonomy to construct.
type Taxonomy struct {
	Version  string
	paths    map[string]struct{}
	pathList []string
	tags     map[string]struct{}
	tagList  []string
}

type rawTaxonomy struct {
	Version    string    `yaml:"version"`
	Categories yaml.Node `yaml:"categories"`
	Tags       []string  `yaml:"tags"`
}

// LoadTaxonomy parses YAML bytes into a Taxonomy. Returns ErrTaxonomyMalformed
// (wrapped with parser detail) on any structural problem.
func LoadTaxonomy(data []byte) (Taxonomy, error) {
	var raw rawTaxonomy
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Taxonomy{}, fmt.Errorf("%w: %v", ErrTaxonomyMalformed, err)
	}
	if raw.Version == "" {
		return Taxonomy{}, fmt.Errorf("%w: missing version", ErrTaxonomyMalformed)
	}
	paths, err := collectPaths(&raw.Categories, "")
	if err != nil {
		return Taxonomy{}, err
	}
	pathSet := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		pathSet[p] = struct{}{}
	}
	tagSet := make(map[string]struct{}, len(raw.Tags))
	for _, tag := range raw.Tags {
		tagSet[tag] = struct{}{}
	}
	return Taxonomy{
		Version:  raw.Version,
		paths:    pathSet,
		pathList: paths,
		tags:     tagSet,
		tagList:  slices.Clone(raw.Tags),
	}, nil
}

// collectPaths recursively walks the YAML node and yields every prefix path
// reachable from the root. A node may be a mapping (sub-tree), a sequence
// of leaf names, or null (a leaf with no further children).
func collectPaths(node *yaml.Node, prefix string) ([]string, error) {
	if node == nil || node.IsZero() {
		return nil, nil
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil, nil
		}
		return collectPaths(node.Content[0], prefix)
	case yaml.MappingNode:
		var out []string
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valNode := node.Content[i+1]
			path := keyNode.Value
			if prefix != "" {
				path = prefix + "/" + keyNode.Value
			}
			out = append(out, path)
			children, err := collectPaths(valNode, path)
			if err != nil {
				return nil, err
			}
			out = append(out, children...)
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]string, 0, len(node.Content))
		for _, child := range node.Content {
			if child.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("%w: sequence entries must be strings", ErrTaxonomyMalformed)
			}
			path := child.Value
			if prefix != "" {
				path = prefix + "/" + child.Value
			}
			out = append(out, path)
		}
		return out, nil
	case yaml.ScalarNode:
		if node.Value == "" || node.Tag == "!!null" {
			return nil, nil
		}
		// rare: a categories: bareString — treat as single leaf
		path := node.Value
		if prefix != "" {
			path = prefix + "/" + node.Value
		}
		return []string{path}, nil
	default:
		return nil, errors.New("unexpected yaml node kind")
	}
}

// AllCategoryPaths returns every valid category path in deterministic order.
// Used by the atomizer prompt to constrain LLM output.
func (t Taxonomy) AllCategoryPaths() []string {
	return slices.Clone(t.pathList)
}

// AllTags returns every valid tag in deterministic order.
func (t Taxonomy) AllTags() []string {
	return slices.Clone(t.tagList)
}

// CategoryAllowed reports whether path is exactly a valid category path.
// Comparison is case-sensitive; leading/trailing slashes are not stripped.
func (t Taxonomy) CategoryAllowed(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, ok := t.paths[path]
	return ok
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/domain/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/taxonomy.go internal/domain/taxonomy_test.go testdata/taxonomy.yml.example go.mod go.sum
git commit -m "feat(domain): load taxonomy.yml, expose AllCategoryPaths and CategoryAllowed"
```

---

## Task 7: Taxonomy — FilterTags + Normalize

**Files:**
- Modify: `internal/domain/taxonomy.go` (добавить методы)
- Modify: `internal/domain/taxonomy_test.go`

- [ ] **Step 1: Дописать тесты**

Append to `internal/domain/taxonomy_test.go`:
```go
func TestTaxonomy_FilterTags(t *testing.T) {
	tax := loadTaxonomyFile(t)

	require.Equal(t, []string{"bug", "auth"}, tax.FilterTags([]string{"bug", "unknown", "auth"}))
	require.Empty(t, tax.FilterTags([]string{"none", "alsobad"}))
	require.Empty(t, tax.FilterTags(nil))
	require.Equal(t, []string{"idea"}, tax.FilterTags([]string{"idea", "idea"})) // dedup
}

func TestTaxonomy_Normalize_ValidCategory(t *testing.T) {
	tax := loadTaxonomyFile(t)
	in := domain.Note{
		Category: "work/projects/tms",
		Tags:     []string{"bug", "appsheet", "ghost"},
	}
	out := tax.Normalize(in)

	require.Equal(t, "work/projects/tms", out.Category)
	require.Empty(t, out.OriginalCategory)
	require.Equal(t, []string{"bug", "appsheet"}, out.Tags)
}

func TestTaxonomy_Normalize_InvalidCategoryMovesToUncategorized(t *testing.T) {
	tax := loadTaxonomyFile(t)
	in := domain.Note{
		Category: "crypto/defi",
		Tags:     []string{"idea"},
	}
	out := tax.Normalize(in)

	require.Equal(t, domain.CategoryUncategorized, out.Category)
	require.Equal(t, "crypto/defi", out.OriginalCategory)
	require.Equal(t, []string{"idea"}, out.Tags)
}
```

- [ ] **Step 2: Run, ожидать FAIL**

Run: `go test ./internal/domain/...`
Expected: FAIL.

- [ ] **Step 3: Дополнить `internal/domain/taxonomy.go`**

Добавить методы:
```go
// FilterTags returns only the tags present in the taxonomy whitelist,
// preserving input order and deduplicating.
func (t Taxonomy) FilterTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		if _, dup := seen[tag]; dup {
			continue
		}
		if _, ok := t.tags[tag]; !ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Normalize applies taxonomy rules to a note: invalid categories fall back
// to CategoryUncategorized (preserving the original in OriginalCategory),
// and tags are filtered against the whitelist.
func (t Taxonomy) Normalize(n Note) Note {
	if !t.CategoryAllowed(n.Category) {
		n.OriginalCategory = n.Category
		n.Category = CategoryUncategorized
	}
	n.Tags = t.FilterTags(n.Tags)
	return n
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/domain/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/taxonomy.go internal/domain/taxonomy_test.go
git commit -m "feat(domain): FilterTags and Normalize for taxonomy whitelist enforcement"
```

---

## Task 8: Note YAML marshal/unmarshal (frontmatter + body)

**Files:**
- Create: `internal/domain/yaml.go`
- Test: `internal/domain/yaml_test.go`

- [ ] **Step 1: Написать тест round-trip Note → bytes → Note**

`internal/domain/yaml_test.go`:
```go
package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func sampleNote() domain.Note {
	moscow, _ := time.LoadLocation("Europe/Moscow")
	return domain.Note{
		ID:            "20260527-tms-auth-bug",
		SchemaVersion: domain.SchemaVersion,
		Date:          time.Date(2026, 5, 27, 22, 40, 0, 0, moscow),
		Source:        domain.SourceTelegramVoice,
		Category:      "work/projects/tms",
		Tags:          []string{"bug", "auth"},
		Slug:          "tms-auth-bug",
		Body:          "Текст мысли.\nВторая строка.\n",
		Ingest: domain.IngestMeta{
			DumpID:          "00000000-0000-0000-0000-000000000001",
			ModelAtomize:    "anthropic/claude-3.5-haiku",
			ModelTranscribe: "openai/whisper-1",
		},
	}
}

func TestMarshalNote_Layout(t *testing.T) {
	out, err := domain.MarshalNote(sampleNote())
	require.NoError(t, err)
	s := string(out)

	require.True(t, strings.HasPrefix(s, "---\n"), "must start with --- delimiter")
	require.Contains(t, s, "\n---\n")
	require.Contains(t, s, "id: 20260527-tms-auth-bug")
	require.Contains(t, s, "schema_version: \"1.0\"")
	require.Contains(t, s, "category: work/projects/tms")
	require.Contains(t, s, "Текст мысли.")
	require.True(t, strings.HasSuffix(s, "Вторая строка.\n"))
}

func TestMarshalNote_OmitsEmptyOriginalAndTranscribe(t *testing.T) {
	n := sampleNote()
	n.Source = domain.SourceTelegramText
	n.Ingest.ModelTranscribe = ""

	out, err := domain.MarshalNote(n)
	require.NoError(t, err)
	s := string(out)

	require.NotContains(t, s, "original_category")
	require.NotContains(t, s, "model_transcribe")
}

func TestUnmarshalNote_RoundTrip(t *testing.T) {
	in := sampleNote()
	data, err := domain.MarshalNote(in)
	require.NoError(t, err)

	out, err := domain.UnmarshalNote(data)
	require.NoError(t, err)

	require.Equal(t, in.ID, out.ID)
	require.Equal(t, in.Category, out.Category)
	require.Equal(t, in.Tags, out.Tags)
	require.Equal(t, in.Body, out.Body)
	require.Equal(t, in.Ingest.DumpID, out.Ingest.DumpID)
	require.True(t, in.Date.Equal(out.Date))
}

func TestUnmarshalNote_RejectsMissingDelimiter(t *testing.T) {
	_, err := domain.UnmarshalNote([]byte("no frontmatter here"))
	require.Error(t, err)
}
```

- [ ] **Step 2: Run, ожидать FAIL**

Run: `go test ./internal/domain/...`
Expected: FAIL.

- [ ] **Step 3: Реализовать `internal/domain/yaml.go`**

```go
package domain

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

const frontmatterDelim = "---\n"

// MarshalNote serializes a Note as a Markdown file with YAML frontmatter
// followed by the Body. Output ends with a newline.
func MarshalNote(n Note) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(frontmatterDelim)

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return nil, fmt.Errorf("encode frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close encoder: %w", err)
	}

	buf.WriteString(frontmatterDelim)
	buf.WriteString(n.Body)
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// UnmarshalNote parses a Markdown file with YAML frontmatter back into a Note.
// Body is everything after the second --- delimiter.
func UnmarshalNote(data []byte) (Note, error) {
	if !bytes.HasPrefix(data, []byte(frontmatterDelim)) {
		return Note{}, errors.New("note: missing opening --- delimiter")
	}
	rest := data[len(frontmatterDelim):]
	end := bytes.Index(rest, []byte("\n"+frontmatterDelim))
	if end < 0 {
		return Note{}, errors.New("note: missing closing --- delimiter")
	}
	frontmatter := rest[:end+1] // include trailing newline
	body := rest[end+1+len(frontmatterDelim):]

	var n Note
	if err := yaml.Unmarshal(frontmatter, &n); err != nil {
		return Note{}, fmt.Errorf("decode frontmatter: %w", err)
	}
	n.Body = string(body)
	return n, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/domain/...`
Expected: PASS (включая все более ранние тесты домена).

- [ ] **Step 5: Commit**

```bash
git add internal/domain/yaml.go internal/domain/yaml_test.go
git commit -m "feat(domain): MarshalNote/UnmarshalNote for YAML frontmatter + body"
```

---

## Task 9: Ports (port.in and port.out interfaces)

**Files:**
- Create: `internal/port/in/ingest.go`
- Create: `internal/port/out/transcriber.go`
- Create: `internal/port/out/atomizer.go`
- Create: `internal/port/out/note_store.go`
- Create: `internal/port/out/taxonomy_loader.go`

- [ ] **Step 1: Создать `internal/port/in/ingest.go`**

```go
// Package in declares driving (inbound) ports that adapters can call to
// invoke domain use cases.
package in

import (
	"context"
	"io"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

// IngestRequest describes one dump entering the pipeline.
type IngestRequest struct {
	Source    domain.DumpSource
	Text      string        // populated for text dumps
	Audio     io.ReadCloser // populated for voice dumps; caller transfers ownership
	AudioMIME string
	UserID    int64
}

// IngestResult summarizes the outcome of processing a single dump.
type IngestResult struct {
	Notes         []domain.Note
	Paths         []string
	Uncategorized int
	Errors        []error
}

// IngestDumpUseCase is the entry point for converting a dump into stored notes.
type IngestDumpUseCase interface {
	Execute(ctx context.Context, req IngestRequest) (IngestResult, error)
}
```

- [ ] **Step 2: Создать `internal/port/out/transcriber.go`**

```go
// Package out declares driven (outbound) ports the use case requires.
package out

import (
	"context"
	"io"
)

// Transcriber converts an audio stream into raw text.
type Transcriber interface {
	Transcribe(ctx context.Context, audio io.Reader, mime string) (string, error)
}
```

- [ ] **Step 3: Создать `internal/port/out/atomizer.go`**

```go
package out

import (
	"context"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

// Atomizer asks an LLM to split a dump into atomic notes constrained by
// the supplied taxonomy.
type Atomizer interface {
	Atomize(ctx context.Context, dump string, tax domain.Taxonomy) ([]domain.Note, error)
}
```

- [ ] **Step 4: Создать `internal/port/out/note_store.go`**

```go
package out

import (
	"context"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

// NoteStore persists a note. Implementations may rename to resolve filename
// collisions; the final ID is reflected back into the returned Note via the
// in/out IngestResult by the caller.
type NoteStore interface {
	Write(ctx context.Context, n domain.Note) (path string, finalID string, err error)
}
```

- [ ] **Step 5: Создать `internal/port/out/taxonomy_loader.go`**

```go
package out

import (
	"context"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

// TaxonomyLoader returns the current Taxonomy snapshot. Implementations
// may cache and watch the underlying file.
type TaxonomyLoader interface {
	Load(ctx context.Context) (domain.Taxonomy, error)
}
```

- [ ] **Step 6: Verify build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 7: Commit**

```bash
git add internal/port/
git commit -m "feat(port): define driving and driven port interfaces"
```

---

## Task 10: Generate mocks via mockery

**Files:**
- Create: `internal/port/in/mocks/mock_IngestDumpUseCase.go` (generated)
- Create: `internal/port/out/mocks/mock_Transcriber.go` (generated)
- Create: `internal/port/out/mocks/mock_Atomizer.go` (generated)
- Create: `internal/port/out/mocks/mock_NoteStore.go` (generated)
- Create: `internal/port/out/mocks/mock_TaxonomyLoader.go` (generated)

- [ ] **Step 1: Run mockery**

Run: `make mocks`
Expected: создаются файлы в `internal/port/{in,out}/mocks/`. Если mockery ругается на конфиг — корректировать `.mockery.yml` минимально, не изобретая структуру.

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: success (моки компилируются).

- [ ] **Step 3: Verify quick mock usage**

Создать `internal/port/out/mocks/sanity_test.go`:
```go
package mocks_test

import (
	"context"
	"testing"

	outmocks "github.com/aleksejmetlusko/second-brain/internal/port/out/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestTranscriberMock_Smoke(t *testing.T) {
	m := outmocks.NewMockTranscriber(t)
	m.EXPECT().Transcribe(mock.Anything, mock.Anything, "audio/ogg").
		Return("hello", nil).Once()

	got, err := m.Transcribe(t.Context(), nil, "audio/ogg")
	require.NoError(t, err)
	require.Equal(t, "hello", got)
}
```

Run: `go test ./internal/port/out/mocks/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/port/in/mocks internal/port/out/mocks
git commit -m "chore: generate mockery mocks for all ports"
```

---

## Task 11: UseCase — happy path (text dump)

**Files:**
- Create: `internal/usecase/ingest.go`
- Test: `internal/usecase/ingest_test.go`
- Modify: `go.mod` (add google/uuid)

- [ ] **Step 1: Добавить uuid dependency**

Run: `go get github.com/google/uuid@latest`

- [ ] **Step 2: Написать тест на happy text path**

`internal/usecase/ingest_test.go`:
```go
package usecase_test

import (
	"strings"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
	outmocks "github.com/aleksejmetlusko/second-brain/internal/port/out/mocks"
	"github.com/aleksejmetlusko/second-brain/internal/usecase"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func buildTaxonomy(t *testing.T) domain.Taxonomy {
	t.Helper()
	tax, err := domain.LoadTaxonomy([]byte(`
version: "1.0"
categories:
  work:
    projects:
      - tms
tags:
  - bug
  - auth
`))
	require.NoError(t, err)
	return tax
}

func noteAt(category, slug string) domain.Note {
	return domain.Note{
		SchemaVersion: domain.SchemaVersion,
		Date:          time.Date(2026, 5, 27, 22, 40, 0, 0, time.UTC),
		Source:        domain.SourceTelegramText,
		Category:      category,
		Tags:          []string{"bug"},
		Slug:          slug,
		Body:          "body",
	}
}

func TestExecute_TextDump_Happy(t *testing.T) {
	transcriber := outmocks.NewMockTranscriber(t)
	atomizer := outmocks.NewMockAtomizer(t)
	store := outmocks.NewMockNoteStore(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	tax := buildTaxonomy(t)
	taxLdr.EXPECT().Load(mock.Anything).Return(tax, nil).Once()

	notes := []domain.Note{
		noteAt("work/projects/tms", "tms-bug-a"),
		noteAt("work/projects/tms", "tms-bug-b"),
	}
	atomizer.EXPECT().
		Atomize(mock.Anything, "dump", mock.Anything).
		Return(notes, nil).
		Once()

	store.EXPECT().
		Write(mock.Anything, mock.Anything).
		RunAndReturn(func(_ any, n domain.Note) (string, string, error) {
			require.True(t, strings.HasPrefix(n.ID, "20260527-tms-bug"))
			return "/notes/work/projects/tms/" + n.ID + ".md", n.ID, nil
		}).
		Times(2)

	uc := usecase.NewIngestUseCase(transcriber, atomizer, store, taxLdr, "anthropic/claude-3.5-haiku", "openai/whisper-1")

	result, err := uc.Execute(t.Context(), in.IngestRequest{
		Source: domain.SourceTelegramText,
		Text:   "dump",
		UserID: 42,
	})
	require.NoError(t, err)
	require.Len(t, result.Notes, 2)
	require.Len(t, result.Paths, 2)
	require.Zero(t, result.Uncategorized)
	require.Empty(t, result.Errors)

	// shared DumpID across all notes
	require.Equal(t, result.Notes[0].Ingest.DumpID, result.Notes[1].Ingest.DumpID)
	require.NotEmpty(t, result.Notes[0].Ingest.DumpID)
	require.Equal(t, "anthropic/claude-3.5-haiku", result.Notes[0].Ingest.ModelAtomize)
	require.Empty(t, result.Notes[0].Ingest.ModelTranscribe, "transcribe model should be empty for text")
}
```

- [ ] **Step 3: Run, ожидать FAIL**

Run: `go test ./internal/usecase/...`
Expected: FAIL — `undefined: usecase.NewIngestUseCase`.

- [ ] **Step 4: Реализовать `internal/usecase/ingest.go`**

```go
// Package usecase implements driving-port use cases by orchestrating
// driven-port adapters.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
	"github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// IngestUseCase implements in.IngestDumpUseCase. Use NewIngestUseCase to construct.
type IngestUseCase struct {
	transcriber out.Transcriber
	atomizer    out.Atomizer
	store       out.NoteStore
	taxLoader   out.TaxonomyLoader

	atomizeModel    string
	transcribeModel string
	now             func() time.Time
}

// NewIngestUseCase wires the use case with its driven ports.
// atomizeModel and transcribeModel are recorded in every produced note's
// IngestMeta for traceability.
func NewIngestUseCase(
	tr out.Transcriber,
	at out.Atomizer,
	ns out.NoteStore,
	tl out.TaxonomyLoader,
	atomizeModel string,
	transcribeModel string,
) *IngestUseCase {
	return &IngestUseCase{
		transcriber:     tr,
		atomizer:        at,
		store:           ns,
		taxLoader:       tl,
		atomizeModel:    atomizeModel,
		transcribeModel: transcribeModel,
		now:             time.Now,
	}
}

// Execute runs the pipeline: load taxonomy → transcribe (if voice) → atomize
// → normalize → store. Partial store failures are accumulated in result.Errors.
// A non-nil error is returned only when a whole-pipeline stage fails
// (taxonomy/transcribe/atomize/empty dump).
func (u *IngestUseCase) Execute(ctx context.Context, req in.IngestRequest) (in.IngestResult, error) {
	tax, err := u.taxLoader.Load(ctx)
	if err != nil {
		return in.IngestResult{}, fmt.Errorf("load taxonomy: %w", err)
	}

	text := req.Text
	transcribeUsed := false
	if req.Source == domain.SourceTelegramVoice {
		text, err = u.transcriber.Transcribe(ctx, req.Audio, req.AudioMIME)
		if err != nil {
			return in.IngestResult{}, fmt.Errorf("transcribe: %w", err)
		}
		if strings.TrimSpace(text) == "" {
			return in.IngestResult{}, domain.ErrEmptyDump
		}
		transcribeUsed = true
	}
	if strings.TrimSpace(text) == "" {
		return in.IngestResult{}, domain.ErrEmptyDump
	}

	notes, err := u.atomizer.Atomize(ctx, text, tax)
	if err != nil {
		return in.IngestResult{}, fmt.Errorf("atomize: %w", err)
	}
	if len(notes) == 0 {
		return in.IngestResult{}, domain.ErrAtomizerNoNotes
	}

	dumpID := uuid.NewString()
	now := u.now()
	result := in.IngestResult{}

	for _, n := range notes {
		n = tax.Normalize(n)
		if n.Category == domain.CategoryUncategorized {
			result.Uncategorized++
		}

		n.SchemaVersion = domain.SchemaVersion
		n.Source = req.Source
		n.Date = now
		n.Ingest = domain.IngestMeta{
			DumpID:       dumpID,
			ModelAtomize: u.atomizeModel,
		}
		if transcribeUsed {
			n.Ingest.ModelTranscribe = u.transcribeModel
		}
		n.Slug = domain.Slugify(n.Slug)
		n.ID = domain.BuildID(n.Date, n.Slug)

		path, finalID, writeErr := u.store.Write(ctx, n)
		if writeErr != nil {
			result.Errors = append(result.Errors, fmt.Errorf("write %s: %w", n.ID, writeErr))
			continue
		}
		n.ID = finalID
		result.Notes = append(result.Notes, n)
		result.Paths = append(result.Paths, path)
	}

	if len(result.Notes) == 0 && len(result.Errors) > 0 {
		return result, fmt.Errorf("all writes failed: %w", errors.Join(result.Errors...))
	}
	return result, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/usecase/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/usecase/ingest.go internal/usecase/ingest_test.go go.mod go.sum
git commit -m "feat(usecase): IngestUseCase happy text path with mocked ports"
```

---

## Task 12: UseCase — voice path + empty transcript

**Files:**
- Modify: `internal/usecase/ingest_test.go`

- [ ] **Step 1: Дописать тесты**

```go
func TestExecute_VoiceDump_Happy(t *testing.T) {
	transcriber := outmocks.NewMockTranscriber(t)
	atomizer := outmocks.NewMockAtomizer(t)
	store := outmocks.NewMockNoteStore(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	taxLdr.EXPECT().Load(mock.Anything).Return(buildTaxonomy(t), nil).Once()
	transcriber.EXPECT().
		Transcribe(mock.Anything, mock.Anything, "audio/ogg").
		Return("recognized dump", nil).Once()
	atomizer.EXPECT().
		Atomize(mock.Anything, "recognized dump", mock.Anything).
		Return([]domain.Note{noteAt("work/projects/tms", "x")}, nil).Once()
	store.EXPECT().
		Write(mock.Anything, mock.Anything).
		RunAndReturn(func(_ any, n domain.Note) (string, string, error) { return "/p.md", n.ID, nil }).
		Once()

	uc := usecase.NewIngestUseCase(transcriber, atomizer, store, taxLdr, "atomize-model", "whisper")
	result, err := uc.Execute(t.Context(), in.IngestRequest{
		Source:    domain.SourceTelegramVoice,
		Audio:     nil,
		AudioMIME: "audio/ogg",
	})
	require.NoError(t, err)
	require.Len(t, result.Notes, 1)
	require.Equal(t, "whisper", result.Notes[0].Ingest.ModelTranscribe)
}

func TestExecute_VoiceDump_EmptyTranscript(t *testing.T) {
	transcriber := outmocks.NewMockTranscriber(t)
	atomizer := outmocks.NewMockAtomizer(t)
	store := outmocks.NewMockNoteStore(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	taxLdr.EXPECT().Load(mock.Anything).Return(buildTaxonomy(t), nil).Once()
	transcriber.EXPECT().Transcribe(mock.Anything, mock.Anything, "audio/ogg").
		Return("   \n  ", nil).Once()

	uc := usecase.NewIngestUseCase(transcriber, atomizer, store, taxLdr, "a", "w")
	_, err := uc.Execute(t.Context(), in.IngestRequest{Source: domain.SourceTelegramVoice, AudioMIME: "audio/ogg"})
	require.ErrorIs(t, err, domain.ErrEmptyDump)
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/usecase/...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/usecase/ingest_test.go
git commit -m "test(usecase): voice path and empty-transcript handling"
```

---

## Task 13: UseCase — error propagation (atomize fail, taxonomy fail, no notes)

**Files:**
- Modify: `internal/usecase/ingest_test.go`

- [ ] **Step 1: Дописать тесты**

```go
func TestExecute_TaxonomyLoadFails(t *testing.T) {
	taxLdr := outmocks.NewMockTaxonomyLoader(t)
	taxLdr.EXPECT().Load(mock.Anything).Return(domain.Taxonomy{}, domain.ErrTaxonomyMissing).Once()

	uc := usecase.NewIngestUseCase(nil, nil, nil, taxLdr, "a", "w")
	_, err := uc.Execute(t.Context(), in.IngestRequest{Source: domain.SourceTelegramText, Text: "x"})
	require.ErrorIs(t, err, domain.ErrTaxonomyMissing)
}

func TestExecute_AtomizerFails(t *testing.T) {
	atomizer := outmocks.NewMockAtomizer(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	taxLdr.EXPECT().Load(mock.Anything).Return(buildTaxonomy(t), nil).Once()
	atomizer.EXPECT().Atomize(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, domain.ErrAtomizerBadResponse).Once()

	uc := usecase.NewIngestUseCase(nil, atomizer, nil, taxLdr, "a", "w")
	_, err := uc.Execute(t.Context(), in.IngestRequest{Source: domain.SourceTelegramText, Text: "x"})
	require.ErrorIs(t, err, domain.ErrAtomizerBadResponse)
}

func TestExecute_AtomizerReturnsZeroNotes(t *testing.T) {
	atomizer := outmocks.NewMockAtomizer(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	taxLdr.EXPECT().Load(mock.Anything).Return(buildTaxonomy(t), nil).Once()
	atomizer.EXPECT().Atomize(mock.Anything, mock.Anything, mock.Anything).
		Return([]domain.Note{}, nil).Once()

	uc := usecase.NewIngestUseCase(nil, atomizer, nil, taxLdr, "a", "w")
	_, err := uc.Execute(t.Context(), in.IngestRequest{Source: domain.SourceTelegramText, Text: "x"})
	require.ErrorIs(t, err, domain.ErrAtomizerNoNotes)
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/usecase/...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/usecase/ingest_test.go
git commit -m "test(usecase): error propagation for taxonomy, atomizer, no-notes"
```

---

## Task 14: UseCase — Uncategorized fallback + partial/total store failure

**Files:**
- Modify: `internal/usecase/ingest_test.go`

- [ ] **Step 1: Дописать тесты**

```go
func TestExecute_UncategorizedFallback(t *testing.T) {
	atomizer := outmocks.NewMockAtomizer(t)
	store := outmocks.NewMockNoteStore(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	taxLdr.EXPECT().Load(mock.Anything).Return(buildTaxonomy(t), nil).Once()
	notes := []domain.Note{
		noteAt("work/projects/tms", "valid"),
		noteAt("crypto/defi", "alien"),
	}
	atomizer.EXPECT().Atomize(mock.Anything, mock.Anything, mock.Anything).Return(notes, nil).Once()
	store.EXPECT().Write(mock.Anything, mock.Anything).
		RunAndReturn(func(_ any, n domain.Note) (string, string, error) {
			return "/p/" + n.ID + ".md", n.ID, nil
		}).Times(2)

	uc := usecase.NewIngestUseCase(nil, atomizer, store, taxLdr, "a", "w")
	result, err := uc.Execute(t.Context(), in.IngestRequest{Source: domain.SourceTelegramText, Text: "x"})
	require.NoError(t, err)
	require.Equal(t, 1, result.Uncategorized)
	require.Len(t, result.Notes, 2)

	uncatNotes := 0
	for _, n := range result.Notes {
		if n.Category == domain.CategoryUncategorized {
			require.Equal(t, "crypto/defi", n.OriginalCategory)
			uncatNotes++
		}
	}
	require.Equal(t, 1, uncatNotes)
}

func TestExecute_PartialStoreFailure(t *testing.T) {
	atomizer := outmocks.NewMockAtomizer(t)
	store := outmocks.NewMockNoteStore(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	taxLdr.EXPECT().Load(mock.Anything).Return(buildTaxonomy(t), nil).Once()
	atomizer.EXPECT().Atomize(mock.Anything, mock.Anything, mock.Anything).
		Return([]domain.Note{noteAt("work/projects/tms", "a"), noteAt("work/projects/tms", "b")}, nil).Once()

	var calls int
	store.EXPECT().Write(mock.Anything, mock.Anything).
		RunAndReturn(func(_ any, n domain.Note) (string, string, error) {
			calls++
			if calls == 1 {
				return "", "", errors.New("disk full")
			}
			return "/p/" + n.ID + ".md", n.ID, nil
		}).Times(2)

	uc := usecase.NewIngestUseCase(nil, atomizer, store, taxLdr, "a", "w")
	result, err := uc.Execute(t.Context(), in.IngestRequest{Source: domain.SourceTelegramText, Text: "x"})
	require.NoError(t, err)
	require.Len(t, result.Notes, 1)
	require.Len(t, result.Errors, 1)
}

func TestExecute_AllStoresFail(t *testing.T) {
	atomizer := outmocks.NewMockAtomizer(t)
	store := outmocks.NewMockNoteStore(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	taxLdr.EXPECT().Load(mock.Anything).Return(buildTaxonomy(t), nil).Once()
	atomizer.EXPECT().Atomize(mock.Anything, mock.Anything, mock.Anything).
		Return([]domain.Note{noteAt("work/projects/tms", "a")}, nil).Once()
	store.EXPECT().Write(mock.Anything, mock.Anything).
		Return("", "", errors.New("permission denied")).Once()

	uc := usecase.NewIngestUseCase(nil, atomizer, store, taxLdr, "a", "w")
	_, err := uc.Execute(t.Context(), in.IngestRequest{Source: domain.SourceTelegramText, Text: "x"})
	require.Error(t, err)
}
```

Не забудь импорт `errors` сверху файла (если ещё не).

- [ ] **Step 2: Run tests**

Run: `go test ./internal/usecase/...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/usecase/ingest_test.go
git commit -m "test(usecase): Uncategorized fallback and partial/total store failure"
```

---

## Task 15: Config (cleanenv)

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Create: `cmd/envexample/main.go`

- [ ] **Step 1: Добавить cleanenv**

Run: `go get github.com/ilyakaznacheev/cleanenv`

- [ ] **Step 2: Тесты на Load + Validate**

`internal/config/config_test.go`:
```go
package config_test

import (
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/config"
	"github.com/stretchr/testify/require"
)

func setRequired(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")
	t.Setenv("TELEGRAM_BOT_TOKEN", "tg-test")
	t.Setenv("ALLOWED_USER_IDS", "11,22")
}

func TestLoad_DefaultsApplied(t *testing.T) {
	setRequired(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, "anthropic/claude-3.5-haiku", cfg.AtomizeModel)
	require.Equal(t, "openai/whisper-1", cfg.TranscribeModel)
	require.Equal(t, "/data/notes", cfg.NotesDir)
	require.Equal(t, 120*time.Second, cfg.HTTPTimeout)
	require.Equal(t, "Europe/Moscow", cfg.TZ)
	require.Equal(t, []int64{11, 22}, cfg.AllowedUserIDs)
}

func TestLoad_RequiredMissing(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("TELEGRAM_BOT_TOKEN", "tg-test")
	t.Setenv("ALLOWED_USER_IDS", "11")
	_, err := config.Load()
	require.Error(t, err)
}

func TestValidate_RejectsBadLogLevel(t *testing.T) {
	cfg := config.Config{LogLevel: "loud"}
	require.Error(t, cfg.Validate())
}

func TestValidate_AcceptsKnownLogLevels(t *testing.T) {
	for _, lvl := range []string{"debug", "info", "warn", "error"} {
		cfg := config.Config{
			LogLevel:         lvl,
			OpenRouterAPIKey: "k",
			TelegramBotToken: "t",
			AllowedUserIDs:   []int64{1},
		}
		require.NoError(t, cfg.Validate())
	}
}
```

- [ ] **Step 3: Run, ожидать FAIL**

Run: `go test ./internal/config/...`
Expected: FAIL.

- [ ] **Step 4: Реализовать `internal/config/config.go`**

```go
// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"slices"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config is the fully-populated runtime configuration.
type Config struct {
	OpenRouterAPIKey  string `env:"OPENROUTER_API_KEY"          env-required:"true"   env-description:"OpenRouter API key (sk-or-...)"`
	OpenRouterBaseURL string `env:"OPENROUTER_BASE_URL"         env-default:"https://openrouter.ai/api/v1"`
	AtomizeModel      string `env:"OPENROUTER_ATOMIZE_MODEL"    env-default:"anthropic/claude-3.5-haiku"`
	TranscribeModel   string `env:"OPENROUTER_TRANSCRIBE_MODEL" env-default:"openai/whisper-1"`
	HTTPReferer       string `env:"OPENROUTER_HTTP_REFERER"     env-default:"https://github.com/aleksejmetlusko/second-brain"`
	XTitle            string `env:"OPENROUTER_X_TITLE"          env-default:"Second Brain"`

	TelegramBotToken string  `env:"TELEGRAM_BOT_TOKEN" env-required:"true"`
	AllowedUserIDs   []int64 `env:"ALLOWED_USER_IDS"   env-required:"true" env-separator:"," env-description:"CSV of allowlisted Telegram user IDs"`

	NotesDir  string `env:"NOTES_DIR"  env-default:"/data/notes"`
	ConfigDir string `env:"CONFIG_DIR" env-default:"/etc/second-brain"`

	HTTPTimeout       time.Duration `env:"HTTP_TIMEOUT"       env-default:"120s"`
	TranscribeTimeout time.Duration `env:"TRANSCRIBE_TIMEOUT" env-default:"180s"`
	AtomizeTimeout    time.Duration `env:"ATOMIZE_TIMEOUT"    env-default:"60s"`
	FSWriteTimeout    time.Duration `env:"FS_WRITE_TIMEOUT"   env-default:"5s"`

	TZ       string `env:"TZ"        env-default:"Europe/Moscow"`
	LogLevel string `env:"LOG_LEVEL" env-default:"info" env-description:"debug | info | warn | error"`
}

var allowedLogLevels = []string{"debug", "info", "warn", "error"}

// Load parses environment variables according to struct tags.
func Load() (Config, error) {
	var cfg Config
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return Config{}, fmt.Errorf("read env: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate enforces semantic rules that cleanenv tags cannot express.
func (c Config) Validate() error {
	if !slices.Contains(allowedLogLevels, c.LogLevel) {
		return fmt.Errorf("invalid LOG_LEVEL %q (expected one of %v)", c.LogLevel, allowedLogLevels)
	}
	if len(c.AllowedUserIDs) == 0 {
		return fmt.Errorf("ALLOWED_USER_IDS must contain at least one user")
	}
	return nil
}

// Description returns the auto-generated env description for tooling.
func Description() (string, error) {
	var cfg Config
	return cleanenv.GetDescription(&cfg, nil)
}
```

- [ ] **Step 5: Реализовать `cmd/envexample/main.go`** (генератор `.env.example` через `make env-example`)

```go
package main

import (
	"fmt"
	"os"

	"github.com/aleksejmetlusko/second-brain/internal/config"
)

func main() {
	desc, err := config.Description()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("# .env.example — autogenerated by `make env-example`. Do not edit by hand.")
	fmt.Println(desc)
}
```

- [ ] **Step 6: Run tests + сгенерировать .env.example**

Run:
```bash
go test ./internal/config/...
make env-example > .env.example
```

Expected: тесты PASS, `.env.example` создан с описаниями переменных.

- [ ] **Step 7: Commit**

```bash
git add internal/config cmd/envexample .env.example go.mod go.sum
git commit -m "feat(config): cleanenv-based Config with validation and .env.example generator"
```

---

## Task 16: Adapter fs — NoteStore (с резолвом коллизий)

**Files:**
- Create: `internal/adapter/out/fs/note_store.go`
- Test: `internal/adapter/out/fs/note_store_test.go`

- [ ] **Step 1: Тесты**

`internal/adapter/out/fs/note_store_test.go`:
```go
package fs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/fs"
	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func sample(t *testing.T, category, slug string) domain.Note {
	t.Helper()
	return domain.Note{
		ID:            domain.BuildID(time.Date(2026, 5, 27, 22, 40, 0, 0, time.UTC), slug),
		SchemaVersion: domain.SchemaVersion,
		Date:          time.Date(2026, 5, 27, 22, 40, 0, 0, time.UTC),
		Source:        domain.SourceTelegramText,
		Category:      category,
		Tags:          []string{"bug"},
		Slug:          slug,
		Body:          "body\n",
		Ingest:        domain.IngestMeta{DumpID: "uuid-1", ModelAtomize: "m"},
	}
}

func TestNoteStore_WriteCreatesDirectoriesAndFile(t *testing.T) {
	root := t.TempDir()
	store := fs.NewNoteStore(root)

	path, finalID, err := store.Write(t.Context(), sample(t, "work/projects/tms", "x"))
	require.NoError(t, err)
	require.Equal(t, "20260527-x", finalID)
	require.Equal(t, filepath.Join(root, "work", "projects", "tms", "20260527-x.md"), path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "id: 20260527-x")
}

func TestNoteStore_CollisionAppendsSuffix(t *testing.T) {
	root := t.TempDir()
	store := fs.NewNoteStore(root)

	_, id1, err := store.Write(t.Context(), sample(t, "work/projects/tms", "dup"))
	require.NoError(t, err)
	require.Equal(t, "20260527-dup", id1)

	_, id2, err := store.Write(t.Context(), sample(t, "work/projects/tms", "dup"))
	require.NoError(t, err)
	require.Equal(t, "20260527-dup-2", id2)

	_, id3, err := store.Write(t.Context(), sample(t, "work/projects/tms", "dup"))
	require.NoError(t, err)
	require.Equal(t, "20260527-dup-3", id3)

	// File for -2 must contain the matching id in YAML
	data, _ := os.ReadFile(filepath.Join(root, "work", "projects", "tms", "20260527-dup-2.md"))
	require.Contains(t, string(data), "id: 20260527-dup-2")
}

func TestNoteStore_UncategorizedLayout(t *testing.T) {
	root := t.TempDir()
	store := fs.NewNoteStore(root)
	n := sample(t, domain.CategoryUncategorized, "alien")
	n.OriginalCategory = "crypto/defi"

	path, _, err := store.Write(t.Context(), n)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(path, filepath.Join(root, "uncategorized")))

	data, _ := os.ReadFile(path)
	require.Contains(t, string(data), "original_category: crypto/defi")
}
```

- [ ] **Step 2: Run, FAIL**

Run: `go test ./internal/adapter/out/fs/...`
Expected: FAIL.

- [ ] **Step 3: Реализовать `internal/adapter/out/fs/note_store.go`**

```go
// Package fs contains filesystem-backed driven adapters.
package fs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

// NoteStore writes notes to disk under a configurable root directory.
type NoteStore struct {
	root string
}

// NewNoteStore returns a store that creates files under root.
func NewNoteStore(root string) *NoteStore {
	return &NoteStore{root: root}
}

// Write serializes a Note to the filesystem under root/<category>/<id>.md.
// If a file with that ID already exists, the suffix "-2", "-3", ... is
// appended (both to the filename and to the YAML id field) until a free
// name is found. The final ID is returned alongside the path.
func (s *NoteStore) Write(_ context.Context, n domain.Note) (string, string, error) {
	dir := filepath.Join(s.root, filepath.FromSlash(n.Category))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	finalID, path, err := s.resolveFreePath(dir, n.ID)
	if err != nil {
		return "", "", err
	}
	n.ID = finalID

	data, err := domain.MarshalNote(n)
	if err != nil {
		return "", "", fmt.Errorf("marshal note: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, finalID, nil
}

// resolveFreePath probes baseID, then baseID-2, baseID-3, ... until a name
// that does not exist is found.
func (s *NoteStore) resolveFreePath(dir, baseID string) (string, string, error) {
	for i := 1; i < 1000; i++ {
		id := baseID
		if i > 1 {
			id = fmt.Sprintf("%s-%d", baseID, i)
		}
		path := filepath.Join(dir, id+".md")
		_, err := os.Stat(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			return id, path, nil
		case err == nil:
			continue
		default:
			return "", "", fmt.Errorf("stat %s: %w", path, err)
		}
	}
	return "", "", fmt.Errorf("could not find free name for %s after 1000 attempts", baseID)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/adapter/out/fs/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/out/fs/note_store.go internal/adapter/out/fs/note_store_test.go
git commit -m "feat(adapter/fs): NoteStore with collision resolution and ID rewrite"
```

---

## Task 17: Adapter fs — TaxonomyLoader (mtime cache)

**Files:**
- Create: `internal/adapter/out/fs/taxonomy_loader.go`
- Test: `internal/adapter/out/fs/taxonomy_loader_test.go`

- [ ] **Step 1: Тесты**

`internal/adapter/out/fs/taxonomy_loader_test.go`:
```go
package fs_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/fs"
	"github.com/stretchr/testify/require"
)

const sampleTaxonomy = `version: "1.0"
categories:
  work:
    - alpha
    - beta
tags:
  - bug
`

func TestTaxonomyLoader_LoadsAndCaches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "taxonomy.yml")
	require.NoError(t, os.WriteFile(path, []byte(sampleTaxonomy), 0o644))

	loader := fs.NewTaxonomyLoader(path, 50*time.Millisecond)

	tax1, err := loader.Load(t.Context())
	require.NoError(t, err)
	require.True(t, tax1.CategoryAllowed("work/alpha"))

	// rewrite to a different content; within TTL, cache returns old result
	require.NoError(t, os.WriteFile(path, []byte(`version: "1.0"
categories:
  health:
    - sleep
tags: []
`), 0o644))

	tax2, err := loader.Load(t.Context())
	require.NoError(t, err)
	require.True(t, tax2.CategoryAllowed("work/alpha"), "cached snapshot expected within TTL")

	time.Sleep(80 * time.Millisecond)
	// after TTL expires + mtime changed: new content loaded
	tax3, err := loader.Load(t.Context())
	require.NoError(t, err)
	require.False(t, tax3.CategoryAllowed("work/alpha"))
	require.True(t, tax3.CategoryAllowed("health/sleep"))
}

func TestTaxonomyLoader_MissingFile(t *testing.T) {
	loader := fs.NewTaxonomyLoader("/no/such/path/taxonomy.yml", time.Second)
	_, err := loader.Load(t.Context())
	require.Error(t, err)
}
```

- [ ] **Step 2: Run, FAIL**

Run: `go test ./internal/adapter/out/fs/...`
Expected: FAIL.

- [ ] **Step 3: Реализовать `internal/adapter/out/fs/taxonomy_loader.go`**

```go
package fs

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

// TaxonomyLoader reads taxonomy.yml from disk with a TTL + mtime cache so
// edits are picked up without restart.
type TaxonomyLoader struct {
	path string
	ttl  time.Duration

	mu        sync.Mutex
	cached    domain.Taxonomy
	cachedAt  time.Time
	cachedMod time.Time
	hasCache  bool
}

// NewTaxonomyLoader returns a loader for the given taxonomy.yml path.
// A non-positive TTL disables caching.
func NewTaxonomyLoader(path string, ttl time.Duration) *TaxonomyLoader {
	return &TaxonomyLoader{path: path, ttl: ttl}
}

// Load returns the current Taxonomy snapshot, reloading the file when the
// TTL has elapsed and the file's modification time has changed.
func (l *TaxonomyLoader) Load(_ context.Context) (domain.Taxonomy, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	info, err := os.Stat(l.path)
	if err != nil {
		return domain.Taxonomy{}, fmt.Errorf("stat taxonomy %s: %w", l.path, err)
	}

	if l.hasCache && l.ttl > 0 && time.Since(l.cachedAt) < l.ttl {
		return l.cached, nil
	}
	if l.hasCache && info.ModTime().Equal(l.cachedMod) {
		l.cachedAt = time.Now()
		return l.cached, nil
	}

	data, err := os.ReadFile(l.path)
	if err != nil {
		return domain.Taxonomy{}, fmt.Errorf("read taxonomy %s: %w", l.path, err)
	}
	tax, err := domain.LoadTaxonomy(data)
	if err != nil {
		return domain.Taxonomy{}, err
	}
	l.cached = tax
	l.cachedAt = time.Now()
	l.cachedMod = info.ModTime()
	l.hasCache = true
	return tax, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/adapter/out/fs/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/out/fs/taxonomy_loader.go internal/adapter/out/fs/taxonomy_loader_test.go
git commit -m "feat(adapter/fs): TaxonomyLoader with TTL + mtime cache"
```

---

## Task 18: Adapter openrouter — Client + retry policy

**Files:**
- Create: `internal/adapter/out/openrouter/client.go`
- Create: `internal/adapter/out/openrouter/retry.go`
- Test: `internal/adapter/out/openrouter/retry_test.go`

- [ ] **Step 1: Добавить SDK**

Run: `go get github.com/OpenRouterTeam/go-sdk@latest`

- [ ] **Step 2: Тесты для retry**

`internal/adapter/out/openrouter/retry_test.go`:
```go
package openrouter_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/openrouter"
	"github.com/stretchr/testify/require"
)

func TestRetry_StopsOnNonRetryableHTTP(t *testing.T) {
	policy := openrouter.RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond}
	var calls int
	err := openrouter.WithRetry(t.Context(), policy, func(_ context.Context) error {
		calls++
		return openrouter.HTTPError{Status: 401, Msg: "unauthorized"}
	})
	require.Equal(t, 1, calls)
	require.Error(t, err)
}

func TestRetry_RetriesOn5xx(t *testing.T) {
	policy := openrouter.RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond}
	var calls int
	err := openrouter.WithRetry(t.Context(), policy, func(_ context.Context) error {
		calls++
		if calls < 3 {
			return openrouter.HTTPError{Status: 503, Msg: "transient"}
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, calls)
}

func TestRetry_GivesUpAfterMaxAttempts(t *testing.T) {
	policy := openrouter.RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond}
	var calls int
	err := openrouter.WithRetry(t.Context(), policy, func(_ context.Context) error {
		calls++
		return openrouter.HTTPError{Status: 502, Msg: "bad gw"}
	})
	require.Equal(t, 2, calls)
	require.Error(t, err)
}

func TestRetry_RetriesOnNetError(t *testing.T) {
	policy := openrouter.RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond}
	var calls int
	err := openrouter.WithRetry(t.Context(), policy, func(_ context.Context) error {
		calls++
		if calls == 1 {
			return &net.OpError{Op: "dial", Err: errors.New("conn refused")}
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestRetry_NoRetryOnContextCanceled(t *testing.T) {
	policy := openrouter.RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var calls int
	err := openrouter.WithRetry(ctx, policy, func(_ context.Context) error {
		calls++
		return openrouter.HTTPError{Status: 503, Msg: "down"}
	})
	require.Equal(t, 1, calls)
	require.ErrorIs(t, err, context.Canceled)
}
```

- [ ] **Step 3: Реализовать `internal/adapter/out/openrouter/retry.go`**

```go
package openrouter

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"slices"
	"time"
)

// HTTPError is returned by callers to communicate an HTTP-shaped failure
// without coupling them to a specific HTTP client.
type HTTPError struct {
	Status int
	Msg    string
}

func (e HTTPError) Error() string { return fmt.Sprintf("http %d: %s", e.Status, e.Msg) }

// RetryPolicy describes the back-off used by WithRetry.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Jitter      float64 // 0..1, fraction of computed delay added randomly
}

// DefaultRetryPolicy mirrors the values in the design spec (§5.3).
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 8 * time.Second, Jitter: 0.3}
}

var retryableStatuses = []int{429, 500, 502, 503, 504}

// WithRetry invokes op until it succeeds, the context is cancelled, or
// MaxAttempts is exhausted. Only retryable HTTP statuses and transient
// network errors are retried.
func WithRetry(ctx context.Context, p RetryPolicy, op func(context.Context) error) error {
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	var lastErr error
	for attempt := range p.MaxAttempts {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return errors.Join(lastErr, err)
			}
			return err
		}
		err := op(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryable(err) {
			return err
		}
		if attempt == p.MaxAttempts-1 {
			break
		}
		delay := backoffDelay(p, attempt)
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
		// don't retry past the caller's deadline; surface as-is via outer ctx.Err()
		return false
	}
	if httpErr, ok := errors.AsType[HTTPError](err); ok {
		return slices.Contains(retryableStatuses, httpErr.Status)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return false
}

func backoffDelay(p RetryPolicy, attempt int) time.Duration {
	d := p.BaseDelay * (1 << attempt) //nolint:gosec // small exponent bounded by MaxAttempts
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

- [ ] **Step 4: Реализовать `internal/adapter/out/openrouter/client.go`**

```go
package openrouter

import (
	"net/http"
	"time"

	openrouter "github.com/OpenRouterTeam/go-sdk"
)

// Client groups everything an OpenRouter-backed adapter needs: the official
// SDK for chat completions, plus the raw HTTP client and config for direct
// endpoints not yet covered by the SDK (audio transcription).
type Client struct {
	SDK     *openrouter.SDK
	HTTP    *http.Client
	APIKey  string
	BaseURL string
	Retry   RetryPolicy
}

// ClientConfig is the constructor input for New.
type ClientConfig struct {
	APIKey      string
	BaseURL     string
	HTTPReferer string
	XTitle      string
	HTTPTimeout time.Duration
	Retry       RetryPolicy
}

// New constructs a Client. The same *http.Client is shared between the SDK
// and direct HTTP calls so timeouts and middleware behave consistently.
func New(cfg ClientConfig) *Client {
	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}
	sdk := openrouter.New(
		openrouter.WithSecurity(cfg.APIKey),
		openrouter.WithClient(httpClient),
	)
	return &Client{
		SDK:     sdk,
		HTTP:    httpClient,
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Retry:   cfg.Retry,
	}
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/adapter/out/openrouter/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/adapter/out/openrouter/client.go internal/adapter/out/openrouter/retry.go internal/adapter/out/openrouter/retry_test.go go.mod go.sum
git commit -m "feat(adapter/openrouter): Client wrapper and unified retry policy"
```

---

## Task 19: Adapter openrouter — Atomizer (prompt + chat completion)

**Files:**
- Create: `internal/adapter/out/openrouter/prompt.go`
- Create: `internal/adapter/out/openrouter/atomizer.go`
- Create: `internal/adapter/out/openrouter/prompts/atomize_system.txt`
- Test: `internal/adapter/out/openrouter/atomizer_test.go`
- Test: `internal/adapter/out/openrouter/prompt_test.go`

- [ ] **Step 1: Embed-промпт**

`internal/adapter/out/openrouter/prompts/atomize_system.txt`:
```text
You are an atomizer for a personal knowledge base. The user sent a stream-of-thought dump.

Split the dump into ATOMIC notes. One distinct idea, observation, task, question, or feeling = one note. Do not group.

OUTPUT FORMAT (strict): a single JSON array, no prose, no markdown fences. Each element matches:
{
  "title_slug": "<short kebab-case ASCII slug, <=40 chars>",
  "category":   "<one of: {{categories}}>",
  "tags":       ["<zero or more of: {{tags}}>"],
  "body":       "<the atomic thought as plain markdown, <=1500 chars>"
}

RULES:
- title_slug: lowercase ASCII kebab-case, only [a-z0-9-], <=40 chars, no trailing hyphen
- category: MUST be exactly one of the allowed paths above; if nothing fits, return "uncategorized"
- tags: only from the allowed list; if nothing fits, return []
- body: short, self-contained markdown
- If the dump contains zero ideas, return []

Return only the JSON array.
```

- [ ] **Step 2: Тест prompt rendering**

`internal/adapter/out/openrouter/prompt_test.go`:
```go
package openrouter_test

import (
	"strings"
	"testing"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/openrouter"
	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestRenderAtomizePrompt_InlinesCategoriesAndTags(t *testing.T) {
	tax, err := domain.LoadTaxonomy([]byte(`
version: "1.0"
categories:
  work:
    - tms
tags:
  - bug
  - idea
`))
	require.NoError(t, err)

	out := openrouter.RenderAtomizeSystemPrompt(tax)
	require.True(t, strings.Contains(out, "work"))
	require.True(t, strings.Contains(out, "work/tms"))
	require.True(t, strings.Contains(out, "bug"))
	require.True(t, strings.Contains(out, "idea"))
	require.False(t, strings.Contains(out, "{{categories}}"), "placeholder must be replaced")
	require.False(t, strings.Contains(out, "{{tags}}"), "placeholder must be replaced")
}
```

- [ ] **Step 3: Реализовать `internal/adapter/out/openrouter/prompt.go`**

```go
package openrouter

import (
	_ "embed"
	"strings"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

//go:embed prompts/atomize_system.txt
var atomizeSystemPromptTpl string

// RenderAtomizeSystemPrompt substitutes taxonomy data into the prompt template.
// The output is suitable as a system message for the chat completion request.
func RenderAtomizeSystemPrompt(tax domain.Taxonomy) string {
	cats := strings.Join(tax.AllCategoryPaths(), ", ")
	tags := strings.Join(tax.AllTags(), ", ")
	out := strings.ReplaceAll(atomizeSystemPromptTpl, "{{categories}}", cats)
	out = strings.ReplaceAll(out, "{{tags}}", tags)
	return out
}
```

- [ ] **Step 4: Тест атомизатора через httptest**

`internal/adapter/out/openrouter/atomizer_test.go`:
```go
package openrouter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/openrouter"
	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func newAtomizerFakeServer(t *testing.T, handler http.HandlerFunc) (*openrouter.Atomizer, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cli := openrouter.New(openrouter.ClientConfig{
		APIKey:      "test",
		BaseURL:     srv.URL,
		HTTPTimeout: 5 * time.Second,
		Retry:       openrouter.RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond},
	})
	return openrouter.NewAtomizer(cli, "test-model"), srv
}

func writeChatResponse(w http.ResponseWriter, content string) {
	body := map[string]any{
		"id":      "x",
		"object":  "chat.completion",
		"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": content}}},
	}
	_ = json.NewEncoder(w).Encode(body)
}

func TestAtomizer_ParsesValidResponse(t *testing.T) {
	atomizer, _ := newAtomizerFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer test", r.Header.Get("Authorization"))
		writeChatResponse(w, `[
			{"title_slug":"tms-auth-bug","category":"work/projects/tms","tags":["bug"],"body":"text"},
			{"title_slug":"sleep-idea","category":"health/sleep","tags":[],"body":"hm"}
		]`)
	})

	tax := buildTaxonomyForAtomizer(t)
	notes, err := atomizer.Atomize(t.Context(), "dump", tax)
	require.NoError(t, err)
	require.Len(t, notes, 2)
	require.Equal(t, "work/projects/tms", notes[0].Category)
	require.Equal(t, "tms-auth-bug", notes[0].Slug)
	require.Equal(t, []string{"bug"}, notes[0].Tags)
}

func TestAtomizer_RejectsMalformedJSON(t *testing.T) {
	atomizer, _ := newAtomizerFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeChatResponse(w, "definitely not json")
	})

	_, err := atomizer.Atomize(t.Context(), "dump", buildTaxonomyForAtomizer(t))
	require.ErrorIs(t, err, domain.ErrAtomizerBadResponse)
}

func TestAtomizer_RetriesOn5xx(t *testing.T) {
	var calls int
	atomizer, _ := newAtomizerFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeChatResponse(w, `[{"title_slug":"x","category":"work/projects/tms","tags":[],"body":""}]`)
	})

	notes, err := atomizer.Atomize(t.Context(), "dump", buildTaxonomyForAtomizer(t))
	require.NoError(t, err)
	require.Len(t, notes, 1)
	require.Equal(t, 2, calls)
}

func TestAtomizer_NoRetryOn401(t *testing.T) {
	var calls int
	atomizer, _ := newAtomizerFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
	})

	_, err := atomizer.Atomize(t.Context(), "dump", buildTaxonomyForAtomizer(t))
	require.Error(t, err)
	require.Equal(t, 1, calls)
}

func buildTaxonomyForAtomizer(t *testing.T) domain.Taxonomy {
	t.Helper()
	tax, err := domain.LoadTaxonomy([]byte(`
version: "1.0"
categories:
  work:
    projects:
      - tms
  health:
    - sleep
tags:
  - bug
`))
	require.NoError(t, err)
	return tax
}

func TestAtomizer_StripsCodeFencesIfPresent(t *testing.T) {
	atomizer, _ := newAtomizerFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeChatResponse(w, "```json\n[]\n```")
	})

	notes, err := atomizer.Atomize(t.Context(), "dump", buildTaxonomyForAtomizer(t))
	require.NoError(t, err)
	require.Empty(t, notes)
}
```

- [ ] **Step 5: Реализовать `internal/adapter/out/openrouter/atomizer.go`**

```go
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

// Atomizer asks an LLM (via OpenRouter chat completions) to split a dump
// into atomic notes. It implements port.out.Atomizer.
type Atomizer struct {
	client *Client
	model  string
}

// NewAtomizer constructs an Atomizer bound to a specific OpenRouter model.
func NewAtomizer(c *Client, model string) *Atomizer {
	return &Atomizer{client: c, model: model}
}

type chatRequest struct {
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature"`
	Messages    []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type atomItem struct {
	TitleSlug string   `json:"title_slug"`
	Category  string   `json:"category"`
	Tags      []string `json:"tags"`
	Body      string   `json:"body"`
}

// Atomize sends one request to OpenRouter and returns the parsed notes.
// Returns domain.ErrAtomizerBadResponse if the response cannot be parsed.
func (a *Atomizer) Atomize(ctx context.Context, dump string, tax domain.Taxonomy) ([]domain.Note, error) {
	system := RenderAtomizeSystemPrompt(tax)
	reqBody := chatRequest{
		Model:       a.model,
		Temperature: 0.3,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: dump},
		},
	}

	var content string
	err := WithRetry(ctx, a.client.Retry, func(ctx context.Context) error {
		raw, err := a.postJSON(ctx, "/chat/completions", reqBody)
		if err != nil {
			return err
		}
		var resp chatResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return fmt.Errorf("decode chat response: %w", err)
		}
		if len(resp.Choices) == 0 {
			return fmt.Errorf("%w: no choices", domain.ErrAtomizerBadResponse)
		}
		content = resp.Choices[0].Message.Content
		return nil
	})
	if err != nil {
		return nil, err
	}

	items, err := parseAtomItems(content)
	if err != nil {
		return nil, err
	}
	notes := make([]domain.Note, 0, len(items))
	for _, it := range items {
		notes = append(notes, domain.Note{
			Category: it.Category,
			Tags:     it.Tags,
			Slug:     it.TitleSlug,
			Body:     it.Body,
		})
	}
	return notes, nil
}

func parseAtomItems(s string) ([]atomItem, error) {
	s = strings.TrimSpace(s)
	s = stripCodeFence(s)
	var items []atomItem
	if err := json.Unmarshal([]byte(s), &items); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrAtomizerBadResponse, err)
	}
	return items, nil
}

// stripCodeFence removes a leading ```...``` markdown fence if the LLM
// added one despite instructions.
func stripCodeFence(s string) string {
	if rest, ok := strings.CutPrefix(s, "```"); ok {
		// drop optional language tag on first line
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if cut, ok := strings.CutSuffix(rest, "```"); ok {
			return strings.TrimSpace(cut)
		}
	}
	return s
}

// postJSON sends a JSON body and returns the raw response, classifying
// non-2xx responses as HTTPError for the retry layer.
func (a *Atomizer) postJSON(ctx context.Context, path string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.client.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.client.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, HTTPError{Status: resp.StatusCode, Msg: string(data)}
	}
	return data, nil
}
```

> Note: this implementation uses raw HTTP for the chat call too (rather than the SDK's `s.Chat.Send`). Reason: it keeps retry/error classification logic uniform across atomizer and transcriber and avoids tying every test to the SDK's deeply nested types. The SDK is still used elsewhere if needed; if the team later wants to swap atomizer to `s.Chat.Send`, only this file changes.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/adapter/out/openrouter/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/adapter/out/openrouter/prompt.go internal/adapter/out/openrouter/prompts internal/adapter/out/openrouter/atomizer.go internal/adapter/out/openrouter/atomizer_test.go internal/adapter/out/openrouter/prompt_test.go
git commit -m "feat(adapter/openrouter): Atomizer with embedded prompt and retry"
```

---

## Task 20: Adapter openrouter — Transcriber (multipart audio)

**Files:**
- Create: `internal/adapter/out/openrouter/transcriber.go`
- Test: `internal/adapter/out/openrouter/transcriber_test.go`

- [ ] **Step 1: Тесты**

`internal/adapter/out/openrouter/transcriber_test.go`:
```go
package openrouter_test

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/openrouter"
	"github.com/stretchr/testify/require"
)

func newTranscriberFakeServer(t *testing.T, handler http.HandlerFunc) *openrouter.Transcriber {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cli := openrouter.New(openrouter.ClientConfig{
		APIKey:      "test",
		BaseURL:     srv.URL,
		HTTPTimeout: 5 * time.Second,
		Retry:       openrouter.RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond},
	})
	return openrouter.NewTranscriber(cli, "openai/whisper-1")
}

func TestTranscriber_SendsMultipart(t *testing.T) {
	transcriber := newTranscriberFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/audio/transcriptions", r.URL.Path)
		require.Equal(t, "Bearer test", r.Header.Get("Authorization"))

		mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		require.NoError(t, err)
		require.Equal(t, "multipart/form-data", mt)
		require.NotEmpty(t, params["boundary"])

		err = r.ParseMultipartForm(1 << 20)
		require.NoError(t, err)
		require.Equal(t, []string{"openai/whisper-1"}, r.MultipartForm.Value["model"])
		require.Len(t, r.MultipartForm.File["file"], 1)

		_ = json.NewEncoder(w).Encode(map[string]string{"text": "hello world"})
	})

	got, err := transcriber.Transcribe(t.Context(), strings.NewReader("AUDIO"), "audio/ogg")
	require.NoError(t, err)
	require.Equal(t, "hello world", got)
}

func TestTranscriber_ReturnsHTTPError(t *testing.T) {
	transcriber := newTranscriberFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "no")
	})

	_, err := transcriber.Transcribe(t.Context(), strings.NewReader("AUDIO"), "audio/ogg")
	require.Error(t, err)
}
```

- [ ] **Step 2: Реализовать `internal/adapter/out/openrouter/transcriber.go`**

```go
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// Transcriber calls OpenRouter's /audio/transcriptions endpoint. The endpoint
// is OpenAI-compatible (multipart with "file" + "model"), but is not yet
// covered by the SDK; we issue the request via the SDK's shared HTTP client.
type Transcriber struct {
	client *Client
	model  string
}

// NewTranscriber constructs a Transcriber bound to a specific model.
func NewTranscriber(c *Client, model string) *Transcriber {
	return &Transcriber{client: c, model: model}
}

type transcribeResponse struct {
	Text string `json:"text"`
}

// Transcribe uploads audio bytes to the configured model and returns the
// transcript. The mime parameter is informational; the multipart "filename"
// is derived from it but Whisper accepts a wide range of formats including ogg.
func (t *Transcriber) Transcribe(ctx context.Context, audio io.Reader, mimeType string) (string, error) {
	var body bytes.Buffer
	mp := multipart.NewWriter(&body)

	filename := filenameForMIME(mimeType)
	fw, err := mp.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("multipart file: %w", err)
	}
	if _, err := io.Copy(fw, audio); err != nil {
		return "", fmt.Errorf("copy audio: %w", err)
	}
	if err := mp.WriteField("model", t.model); err != nil {
		return "", fmt.Errorf("multipart model field: %w", err)
	}
	if err := mp.Close(); err != nil {
		return "", fmt.Errorf("multipart close: %w", err)
	}

	var transcript string
	err = WithRetry(ctx, t.client.Retry, func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.client.BaseURL+"/audio/transcriptions", bytes.NewReader(body.Bytes()))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+t.client.APIKey)
		req.Header.Set("Content-Type", mp.FormDataContentType())

		resp, err := t.client.HTTP.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 400 {
			return HTTPError{Status: resp.StatusCode, Msg: string(raw)}
		}
		var parsed transcribeResponse
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return fmt.Errorf("decode transcribe response: %w", err)
		}
		transcript = parsed.Text
		return nil
	})
	return transcript, err
}

func filenameForMIME(m string) string {
	switch m {
	case "audio/ogg", "audio/opus":
		return "voice.ogg"
	case "audio/mpeg", "audio/mp3":
		return "voice.mp3"
	case "audio/wav":
		return "voice.wav"
	default:
		return "voice.bin"
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/adapter/out/openrouter/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/adapter/out/openrouter/transcriber.go internal/adapter/out/openrouter/transcriber_test.go
git commit -m "feat(adapter/openrouter): Transcriber with multipart audio upload"
```

---

## Task 21: Adapter telegram — bot client + dedup LRU

**Files:**
- Create: `internal/adapter/in/telegram/dedup.go`
- Create: `internal/adapter/in/telegram/bot.go`
- Test: `internal/adapter/in/telegram/dedup_test.go`

- [ ] **Step 1: Тесты дедупа**

`internal/adapter/in/telegram/dedup_test.go`:
```go
package telegram_test

import (
	"testing"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/in/telegram"
	"github.com/stretchr/testify/require"
)

func TestDedup_FirstTimeFalse_SubsequentTrue(t *testing.T) {
	d := telegram.NewUpdateDedup(4)
	require.False(t, d.Seen(1))
	require.True(t, d.Seen(1))
	require.False(t, d.Seen(2))
	require.True(t, d.Seen(2))
}

func TestDedup_EvictsOldest(t *testing.T) {
	d := telegram.NewUpdateDedup(2)
	d.Seen(1)
	d.Seen(2)
	d.Seen(3) // evicts 1
	require.False(t, d.Seen(1), "1 should have been evicted")
	require.True(t, d.Seen(2))
	require.True(t, d.Seen(3))
}
```

- [ ] **Step 2: Реализовать `internal/adapter/in/telegram/dedup.go`**

```go
// Package telegram contains the driving adapter that bridges Telegram updates
// to the IngestDumpUseCase.
package telegram

import (
	"container/list"
	"sync"
)

// UpdateDedup remembers the most recent N update IDs to suppress duplicates
// produced by Telegram retries or operator-triggered redelivery.
type UpdateDedup struct {
	cap  int
	mu   sync.Mutex
	set  map[int64]*list.Element
	list *list.List
}

// NewUpdateDedup returns a dedup that remembers the last cap update IDs.
func NewUpdateDedup(cap int) *UpdateDedup {
	if cap < 1 {
		cap = 1
	}
	return &UpdateDedup{cap: cap, set: make(map[int64]*list.Element, cap), list: list.New()}
}

// Seen reports whether updateID was previously seen and records it otherwise.
func (d *UpdateDedup) Seen(updateID int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.set[updateID]; ok {
		return true
	}
	if d.list.Len() == d.cap {
		oldest := d.list.Back()
		if oldest != nil {
			delete(d.set, oldest.Value.(int64))
			d.list.Remove(oldest)
		}
	}
	d.set[updateID] = d.list.PushFront(updateID)
	return false
}
```

- [ ] **Step 3: Добавить `go-telegram/bot`**

Run: `go get github.com/go-telegram/bot@latest`

- [ ] **Step 4: Заглушка `internal/adapter/in/telegram/bot.go`**

```go
package telegram

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot"
)

// Bot wraps a *bot.Bot with allowlist and dedup state shared by handlers.
type Bot struct {
	BotAPI         *bot.Bot
	AllowedUserIDs map[int64]struct{}
	Dedup          *UpdateDedup
	Logger         *slog.Logger
}

// NewBot constructs a Bot wrapper. The wrapper does not start polling on its
// own; the composition root attaches handlers and calls Start.
func NewBot(api *bot.Bot, allowed []int64, logger *slog.Logger) *Bot {
	set := make(map[int64]struct{}, len(allowed))
	for _, id := range allowed {
		set[id] = struct{}{}
	}
	return &Bot{
		BotAPI:         api,
		AllowedUserIDs: set,
		Dedup:          NewUpdateDedup(1024),
		Logger:         logger,
	}
}

// Start begins long-polling. The context controls shutdown.
func (b *Bot) Start(ctx context.Context) {
	b.BotAPI.Start(ctx)
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/adapter/in/telegram/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/adapter/in/telegram/dedup.go internal/adapter/in/telegram/dedup_test.go internal/adapter/in/telegram/bot.go go.mod go.sum
git commit -m "feat(adapter/telegram): bot wrapper, allowlist set, update-id dedup LRU"
```

---

## Task 22: Adapter telegram — reply formatter

**Files:**
- Create: `internal/adapter/in/telegram/reply.go`
- Test: `internal/adapter/in/telegram/reply_test.go`

- [ ] **Step 1: Тесты**

`internal/adapter/in/telegram/reply_test.go`:
```go
package telegram_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/in/telegram"
	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
	"github.com/stretchr/testify/require"
)

func makeResult() in.IngestResult {
	return in.IngestResult{
		Notes: []domain.Note{
			{ID: "20260527-tms-auth-bug", Category: "work/projects/tms", Tags: []string{"appsheet", "bug"}, Slug: "tms-auth-bug"},
			{ID: "20260527-sleep", Category: "health/sleep", Tags: []string{"idea"}, Slug: "sleep"},
		},
		Paths:         []string{"/p1.md", "/p2.md"},
		Uncategorized: 0,
	}
}

func TestFormatReply_HappySummary(t *testing.T) {
	out := telegram.FormatReply(makeResult(), nil)
	require.True(t, strings.HasPrefix(out, "✅"))
	require.Contains(t, out, "Сохранено 2 заметки")
	require.Contains(t, out, "[tms-auth-bug] (work/projects/tms) #appsheet #bug")
	require.Contains(t, out, "[sleep] (health/sleep) #idea")
}

func TestFormatReply_WithUncategorized(t *testing.T) {
	r := makeResult()
	r.Uncategorized = 1
	out := telegram.FormatReply(r, nil)
	require.Contains(t, out, "📦 1 уехала в Uncategorized")
}

func TestFormatReply_PartialErrors(t *testing.T) {
	r := makeResult()
	r.Errors = []error{errors.New("disk full")}
	out := telegram.FormatReply(r, nil)
	require.Contains(t, out, "⚠️ 1 заметка не записалась")
}

func TestFormatReply_EmptyNotesNonErrorMessage(t *testing.T) {
	out := telegram.FormatReply(in.IngestResult{}, domain.ErrAtomizerNoNotes)
	require.Contains(t, out, "🤔")
}

func TestFormatReply_TranscribeError(t *testing.T) {
	out := telegram.FormatReply(in.IngestResult{}, errors.New("transcribe: blah"))
	require.True(t, strings.HasPrefix(out, "❌"))
}
```

- [ ] **Step 2: Реализация `internal/adapter/in/telegram/reply.go`**

```go
package telegram

import (
	"errors"
	"fmt"
	"strings"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
)

// FormatReply turns an IngestResult plus a top-level error into the Russian-
// language summary the user sees in Telegram.
func FormatReply(r in.IngestResult, topErr error) string {
	switch {
	case errors.Is(topErr, domain.ErrAtomizerNoNotes):
		return "🤔 не нашёл идей в дампе"
	case errors.Is(topErr, domain.ErrEmptyDump):
		return "❌ не распознал речь, повтори"
	case topErr != nil:
		return fmt.Sprintf("❌ %s", shortError(topErr))
	case len(r.Notes) == 0:
		return "🤔 ничего не сохранилось"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "✅ Сохранено %s:\n", pluralNotes(len(r.Notes)))
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "• [%s] (%s)", n.Slug, n.Category)
		for _, tag := range n.Tags {
			fmt.Fprintf(&b, " #%s", tag)
		}
		b.WriteByte('\n')
	}
	if r.Uncategorized > 0 {
		fmt.Fprintf(&b, "📦 %s в Uncategorized\n", pluralUncat(r.Uncategorized))
	}
	if len(r.Errors) > 0 {
		fmt.Fprintf(&b, "⚠️ %s не записалась\n", pluralNotErr(len(r.Errors)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func pluralNotes(n int) string {
	switch n % 10 {
	case 1:
		if n%100 != 11 {
			return fmt.Sprintf("%d заметка", n)
		}
	case 2, 3, 4:
		if n%100 < 12 || n%100 > 14 {
			return fmt.Sprintf("%d заметки", n)
		}
	}
	return fmt.Sprintf("%d заметок", n)
}

func pluralUncat(n int) string {
	if n == 1 {
		return "1 уехала"
	}
	return fmt.Sprintf("%d уехали", n)
}

func pluralNotErr(n int) string {
	if n == 1 {
		return "1 заметка"
	}
	return fmt.Sprintf("%d заметок", n)
}

func shortError(err error) string {
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	return msg
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/adapter/in/telegram/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/adapter/in/telegram/reply.go internal/adapter/in/telegram/reply_test.go
git commit -m "feat(adapter/telegram): reply formatter with Russian pluralization"
```

---

## Task 23: Adapter telegram — handler (allowlist, voice limit, routing)

**Files:**
- Create: `internal/adapter/in/telegram/handler.go`
- Test: `internal/adapter/in/telegram/handler_test.go`

- [ ] **Step 1: Тесты**

`internal/adapter/in/telegram/handler_test.go`:
```go
package telegram_test

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/in/telegram"
	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
	inmocks "github.com/aleksejmetlusko/second-brain/internal/port/in/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func nopLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRoute_AllowlistRejectsUnknown(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	r := telegram.NewRouter(uc, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())

	decision := r.Route(telegram.IncomingUpdate{UpdateID: 1, FromUserID: 999, Kind: telegram.UpdateText, Text: "x"})
	require.Equal(t, telegram.ActionDrop, decision.Action)
}

func TestRoute_DedupSkipsDuplicate(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	r := telegram.NewRouter(uc, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())

	d1 := r.Route(telegram.IncomingUpdate{UpdateID: 7, FromUserID: 42, Kind: telegram.UpdateText, Text: "x"})
	require.Equal(t, telegram.ActionProcessText, d1.Action)

	d2 := r.Route(telegram.IncomingUpdate{UpdateID: 7, FromUserID: 42, Kind: telegram.UpdateText, Text: "x"})
	require.Equal(t, telegram.ActionDrop, d2.Action)
}

func TestRoute_VoiceTooLongRejected(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	r := telegram.NewRouter(uc, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())

	d := r.Route(telegram.IncomingUpdate{UpdateID: 1, FromUserID: 42, Kind: telegram.UpdateVoice, VoiceDurationSec: telegram.MaxVoiceSeconds + 1})
	require.Equal(t, telegram.ActionReplyTooLong, d.Action)
}

func TestRoute_VoiceWithinLimitProcessed(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	r := telegram.NewRouter(uc, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())

	d := r.Route(telegram.IncomingUpdate{UpdateID: 1, FromUserID: 42, Kind: telegram.UpdateVoice, VoiceDurationSec: 60})
	require.Equal(t, telegram.ActionProcessVoice, d.Action)
}

func TestRoute_UnsupportedKindReplies(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	r := telegram.NewRouter(uc, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())

	d := r.Route(telegram.IncomingUpdate{UpdateID: 1, FromUserID: 42, Kind: telegram.UpdateOther})
	require.Equal(t, telegram.ActionReplyUnsupported, d.Action)
}

func TestHandle_ProcessTextCallsUseCase(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	uc.EXPECT().Execute(mock.Anything, mock.MatchedBy(func(req in.IngestRequest) bool {
		return req.Source == domain.SourceTelegramText && req.Text == "hello" && req.UserID == 42
	})).Return(in.IngestResult{Notes: []domain.Note{{Slug: "x", Category: "work"}}}, nil).Once()

	r := telegram.NewRouter(uc, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())
	reply, err := r.Handle(t.Context(), telegram.IncomingUpdate{UpdateID: 1, FromUserID: 42, Kind: telegram.UpdateText, Text: "hello"})
	require.NoError(t, err)
	require.NotEmpty(t, reply)
}

func TestHandle_TopErrorIsFormatted(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	uc.EXPECT().Execute(mock.Anything, mock.Anything).Return(in.IngestResult{}, errors.New("transcribe: down")).Once()

	r := telegram.NewRouter(uc, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())
	reply, err := r.Handle(t.Context(), telegram.IncomingUpdate{UpdateID: 1, FromUserID: 42, Kind: telegram.UpdateText, Text: "hi"})
	require.NoError(t, err)
	require.Contains(t, reply, "❌")
}
```

- [ ] **Step 2: Реализовать `internal/adapter/in/telegram/handler.go`**

```go
package telegram

import (
	"context"
	"io"
	"log/slog"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
)

// MaxVoiceSeconds caps the duration of a single voice message we accept;
// audios above this need to be split by the user.
const MaxVoiceSeconds = 1500

// UpdateKind enumerates the message types the handler distinguishes.
type UpdateKind int

const (
	UpdateText UpdateKind = iota + 1
	UpdateVoice
	UpdateOther
)

// IncomingUpdate is the adapter-internal representation of a Telegram update,
// stripped of library-specific types so Route is pure-Go testable.
type IncomingUpdate struct {
	UpdateID         int64
	FromUserID       int64
	Kind             UpdateKind
	Text             string
	VoiceDurationSec int
	AudioOpener      func(context.Context) (io.ReadCloser, error) // populated for voice
	AudioMIME        string
}

// Action is the routing decision: what to do with the update.
type Action int

const (
	ActionDrop Action = iota + 1
	ActionProcessText
	ActionProcessVoice
	ActionReplyUnsupported
	ActionReplyTooLong
)

// Decision returns Route's outcome with a pre-rendered user-visible reply
// for static-reply branches.
type Decision struct {
	Action Action
	Reply  string
}

// Router decides what to do with each incoming update and runs the use case
// for the messages that pass the filters.
type Router struct {
	uc      in.IngestDumpUseCase
	allowed map[int64]struct{}
	dedup   *UpdateDedup
	logger  *slog.Logger
}

// NewRouter constructs a Router. allowed must already be a set of user IDs;
// dedup is shared with other concurrent handlers.
func NewRouter(uc in.IngestDumpUseCase, allowed map[int64]struct{}, dedup *UpdateDedup, logger *slog.Logger) *Router {
	return &Router{uc: uc, allowed: allowed, dedup: dedup, logger: logger}
}

// Route classifies the update without running the use case. Useful for tests
// and for handlers that want to short-circuit before downloading the audio.
func (r *Router) Route(u IncomingUpdate) Decision {
	if _, ok := r.allowed[u.FromUserID]; !ok {
		r.logger.Info("rejected non-allowlisted user", "user_id", u.FromUserID, "update_id", u.UpdateID)
		return Decision{Action: ActionDrop}
	}
	if r.dedup.Seen(u.UpdateID) {
		r.logger.Info("dropping duplicate update", "update_id", u.UpdateID)
		return Decision{Action: ActionDrop}
	}
	switch u.Kind {
	case UpdateText:
		return Decision{Action: ActionProcessText}
	case UpdateVoice:
		if u.VoiceDurationSec > MaxVoiceSeconds {
			return Decision{Action: ActionReplyTooLong, Reply: "❌ Голосовое > 25 мин, разбей"}
		}
		return Decision{Action: ActionProcessVoice}
	default:
		return Decision{Action: ActionReplyUnsupported, Reply: "Поддерживаются текст и голос"}
	}
}

// Handle runs the full incoming → use case → reply pipeline and returns the
// user-visible reply text. Errors here are infrastructure errors (e.g. audio
// download failure); business errors come back as formatted reply strings.
func (r *Router) Handle(ctx context.Context, u IncomingUpdate) (string, error) {
	d := r.Route(u)
	switch d.Action {
	case ActionDrop:
		return "", nil
	case ActionReplyUnsupported, ActionReplyTooLong:
		return d.Reply, nil
	case ActionProcessText:
		res, err := r.uc.Execute(ctx, in.IngestRequest{
			Source: domain.SourceTelegramText,
			Text:   u.Text,
			UserID: u.FromUserID,
		})
		return FormatReply(res, err), nil
	case ActionProcessVoice:
		audio, err := u.AudioOpener(ctx)
		if err != nil {
			return "❌ не удалось скачать голосовое, повтори", nil
		}
		defer audio.Close()
		res, err := r.uc.Execute(ctx, in.IngestRequest{
			Source:    domain.SourceTelegramVoice,
			Audio:     audio,
			AudioMIME: u.AudioMIME,
			UserID:    u.FromUserID,
		})
		return FormatReply(res, err), nil
	default:
		return "", nil
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/adapter/in/telegram/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/adapter/in/telegram/handler.go internal/adapter/in/telegram/handler_test.go
git commit -m "feat(adapter/telegram): Router with allowlist, dedup, voice-limit routing"
```

---

## Task 24: Adapter telegram — wire library handler

**Files:**
- Modify: `internal/adapter/in/telegram/bot.go`
- Create: `internal/adapter/in/telegram/telegram_handler.go`

- [ ] **Step 1: Реализовать `internal/adapter/in/telegram/telegram_handler.go`**

```go
package telegram

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// TelegramHandler bridges go-telegram/bot models to our adapter-internal
// IncomingUpdate type and dispatches through Router.
type TelegramHandler struct {
	router *Router
	httpC  *http.Client
	token  string
}

// NewTelegramHandler returns a bot.HandlerFunc-compatible handler ready to
// pass to b.RegisterHandler / b.RegisterHandlerMatchFunc.
func NewTelegramHandler(router *Router, token string) *TelegramHandler {
	return &TelegramHandler{router: router, httpC: http.DefaultClient, token: token}
}

// Handle is the entry point passed to the go-telegram/bot library.
func (h *TelegramHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil {
		return
	}
	msg := update.Message
	in := IncomingUpdate{
		UpdateID:   update.ID,
		FromUserID: msg.From.ID,
	}
	switch {
	case msg.Voice != nil:
		in.Kind = UpdateVoice
		in.VoiceDurationSec = msg.Voice.Duration
		in.AudioMIME = msg.Voice.MimeType
		fileID := msg.Voice.FileID
		in.AudioOpener = func(ctx context.Context) (io.ReadCloser, error) {
			return h.downloadFile(ctx, b, fileID)
		}
	case msg.Text != "":
		in.Kind = UpdateText
		in.Text = msg.Text
	default:
		in.Kind = UpdateOther
	}

	reply, err := h.router.Handle(ctx, in)
	if err != nil {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: "❌ внутренняя ошибка"})
		return
	}
	if reply == "" {
		return
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: reply})
}

func (h *TelegramHandler) downloadFile(ctx context.Context, b *bot.Bot, fileID string) (io.ReadCloser, error) {
	file, err := b.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		return nil, fmt.Errorf("get file: %w", err)
	}
	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", h.token, file.FilePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.httpC.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download status %d", resp.StatusCode)
	}
	return resp.Body, nil
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/adapter/in/telegram/telegram_handler.go
git commit -m "feat(adapter/telegram): glue handler between go-telegram/bot and Router"
```

---

## Task 25: Composition root — cmd/ingest/main.go

**Files:**
- Create: `cmd/ingest/main.go`

- [ ] **Step 1: Реализовать main**

```go
// Command ingest is the entry point for the Ingestion MVP service.
// It wires adapters together and runs the Telegram long-poll loop.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-telegram/bot"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/in/telegram"
	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/fs"
	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/openrouter"
	"github.com/aleksejmetlusko/second-brain/internal/config"
	"github.com/aleksejmetlusko/second-brain/internal/usecase"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	if _, err := time.LoadLocation(cfg.TZ); err != nil {
		return fmt.Errorf("invalid TZ %s: %w", cfg.TZ, err)
	}

	taxLoader := fs.NewTaxonomyLoader(filepath.Join(cfg.ConfigDir, "taxonomy.yml"), time.Minute)
	if _, err := taxLoader.Load(context.Background()); err != nil {
		return fmt.Errorf("initial taxonomy load: %w", err)
	}

	noteStore := fs.NewNoteStore(cfg.NotesDir)

	openrtrClient := openrouter.New(openrouter.ClientConfig{
		APIKey:      cfg.OpenRouterAPIKey,
		BaseURL:     cfg.OpenRouterBaseURL,
		HTTPReferer: cfg.HTTPReferer,
		XTitle:      cfg.XTitle,
		HTTPTimeout: cfg.HTTPTimeout,
		Retry:       openrouter.DefaultRetryPolicy(),
	})
	atomizer := openrouter.NewAtomizer(openrtrClient, cfg.AtomizeModel)
	transcriber := openrouter.NewTranscriber(openrtrClient, cfg.TranscribeModel)

	uc := usecase.NewIngestUseCase(transcriber, atomizer, noteStore, taxLoader, cfg.AtomizeModel, cfg.TranscribeModel)

	allowed := make(map[int64]struct{}, len(cfg.AllowedUserIDs))
	for _, id := range cfg.AllowedUserIDs {
		allowed[id] = struct{}{}
	}
	dedup := telegram.NewUpdateDedup(1024)
	router := telegram.NewRouter(uc, allowed, dedup, logger)
	tgHandler := telegram.NewTelegramHandler(router, cfg.TelegramBotToken)

	b, err := bot.New(cfg.TelegramBotToken, bot.WithDefaultHandler(tgHandler.Handle))
	if err != nil {
		return fmt.Errorf("init telegram bot: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("starting bot", "atomize_model", cfg.AtomizeModel, "transcribe_model", cfg.TranscribeModel, "notes_dir", cfg.NotesDir)
	b.Start(ctx)

	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("shutdown complete")
	return nil
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add cmd/ingest/main.go
git commit -m "feat(cmd/ingest): composition root wiring all adapters into the use case"
```

---

## Task 26: Panic recovery + correlation logging middleware

**Files:**
- Modify: `internal/adapter/in/telegram/telegram_handler.go`
- Modify: `internal/adapter/in/telegram/handler.go`
- Create: `internal/adapter/in/telegram/middleware.go`
- Test: `internal/adapter/in/telegram/middleware_test.go`

- [ ] **Step 1: Тест на recover**

`internal/adapter/in/telegram/middleware_test.go`:
```go
package telegram_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/in/telegram"
	"github.com/stretchr/testify/require"
)

func TestRecoverMiddleware_ConvertsPanicToReply(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := func(_ context.Context, _ telegram.IncomingUpdate) (string, error) {
		panic("kaboom")
	}
	wrapped := telegram.RecoverMiddleware(logger, handler)
	reply, err := wrapped(context.Background(), telegram.IncomingUpdate{UpdateID: 1, FromUserID: 1})
	require.NoError(t, err)
	require.Contains(t, reply, "❌ внутренняя ошибка")
}

func TestRecoverMiddleware_PassThroughError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := func(_ context.Context, _ telegram.IncomingUpdate) (string, error) {
		return "", errors.New("boom")
	}
	wrapped := telegram.RecoverMiddleware(logger, handler)
	_, err := wrapped(context.Background(), telegram.IncomingUpdate{UpdateID: 1, FromUserID: 1})
	require.Error(t, err)
}
```

- [ ] **Step 2: Реализовать `internal/adapter/in/telegram/middleware.go`**

```go
package telegram

import (
	"context"
	"log/slog"
	"runtime/debug"
)

// HandleFunc is the signature exposed by Router.Handle, also used by middleware.
type HandleFunc func(ctx context.Context, u IncomingUpdate) (string, error)

// RecoverMiddleware turns panics into a user-visible error reply and an
// ERROR log entry. The process keeps running.
func RecoverMiddleware(logger *slog.Logger, next HandleFunc) HandleFunc {
	return func(ctx context.Context, u IncomingUpdate) (reply string, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic in handler",
					"recover", r,
					"update_id", u.UpdateID,
					"user_id", u.FromUserID,
					"stack", string(debug.Stack()),
				)
				reply = "❌ внутренняя ошибка, см. логи"
				err = nil
			}
		}()
		return next(ctx, u)
	}
}
```

- [ ] **Step 3: Подключить middleware в `telegram_handler.go`**

Изменить `TelegramHandler.Handle`:
```go
func (h *TelegramHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil {
		return
	}
	msg := update.Message
	in := h.buildIncoming(update, msg, b)

	logger := slog.Default().With("update_id", update.ID, "user_id", msg.From.ID)
	handler := RecoverMiddleware(logger, h.router.Handle)

	reply, err := handler(ctx, in)
	if err != nil {
		logger.Error("handler error", "err", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: "❌ внутренняя ошибка"})
		return
	}
	if reply == "" {
		return
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: reply})
}

func (h *TelegramHandler) buildIncoming(update *models.Update, msg *models.Message, b *bot.Bot) IncomingUpdate {
	in := IncomingUpdate{UpdateID: update.ID, FromUserID: msg.From.ID}
	switch {
	case msg.Voice != nil:
		in.Kind = UpdateVoice
		in.VoiceDurationSec = msg.Voice.Duration
		in.AudioMIME = msg.Voice.MimeType
		fileID := msg.Voice.FileID
		in.AudioOpener = func(ctx context.Context) (io.ReadCloser, error) {
			return h.downloadFile(ctx, b, fileID)
		}
	case msg.Text != "":
		in.Kind = UpdateText
		in.Text = msg.Text
	default:
		in.Kind = UpdateOther
	}
	return in
}
```

Импорт `log/slog` добавить.

- [ ] **Step 4: Run tests + build**

Run: `go test ./internal/adapter/in/telegram/...` and `go build ./...`
Expected: PASS / success.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/in/telegram/middleware.go internal/adapter/in/telegram/middleware_test.go internal/adapter/in/telegram/telegram_handler.go
git commit -m "feat(adapter/telegram): panic recovery middleware around router"
```

---

## Task 27: Integration tests — golden files

**Files:**
- Create: `internal/integration/integration_test.go`
- Create: `testdata/golden/case_01_text_happy/atomizer_response.json`
- Create: `testdata/golden/case_01_text_happy/taxonomy.yml`
- Create: `testdata/golden/case_01_text_happy/input.txt`
- Create: `testdata/golden/case_01_text_happy/expected/reply.txt`
- Create: `testdata/golden/case_01_text_happy/expected/work/projects/tms/20260527-tms-auth-bug.md`
- Create: `testdata/golden/case_02_uncategorized/` (см. Step 6)
- Create: `testdata/golden/case_03_empty/` (см. Step 7)

- [ ] **Step 1: Создать кейс 01 — happy text**

`testdata/golden/case_01_text_happy/input.txt`:
```
В TMS опять упал auth, AppSheet не пропускает. Идея: вынести проверку токена в middleware.
```

`testdata/golden/case_01_text_happy/taxonomy.yml`:
```yaml
version: "1.0"
categories:
  work:
    projects:
      - tms
tags:
  - auth
  - bug
  - idea
```

`testdata/golden/case_01_text_happy/atomizer_response.json`:
```json
[
  {
    "title_slug": "tms-auth-bug",
    "category": "work/projects/tms",
    "tags": ["auth", "bug"],
    "body": "В TMS снова упал auth, AppSheet не пропускает запрос."
  },
  {
    "title_slug": "tms-auth-middleware",
    "category": "work/projects/tms",
    "tags": ["idea"],
    "body": "Идея: вынести проверку токена в middleware."
  }
]
```

`testdata/golden/case_01_text_happy/expected/reply.txt`:
```
✅ Сохранено 2 заметки:
• [tms-auth-bug] (work/projects/tms) #auth #bug
• [tms-auth-middleware] (work/projects/tms) #idea
```

`testdata/golden/case_01_text_happy/expected/work/projects/tms/20260527-tms-auth-bug.md`:
```markdown
---
id: 20260527-tms-auth-bug
schema_version: "1.0"
date: 2026-05-27T22:40:00+03:00
source: telegram-text
category: work/projects/tms
tags:
    - auth
    - bug
ingest:
    dump_id: 00000000-0000-0000-0000-000000000001
    model_atomize: test-atomize
---
В TMS снова упал auth, AppSheet не пропускает запрос.
```

Аналогично для `20260527-tms-auth-middleware.md`.

> Note: точный формат YAML (отступы, кавычки) определяется реализацией `MarshalNote` — при первом запуске тестов используй флаг `-update` (см. ниже), чтобы записать каноничные ожидаемые файлы из реального вывода.

- [ ] **Step 2: Реализовать `internal/integration/integration_test.go`**

```go
//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/in/telegram"
	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/fs"
	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
	"github.com/aleksejmetlusko/second-brain/internal/port/out"
	"github.com/aleksejmetlusko/second-brain/internal/usecase"
	"github.com/stretchr/testify/require"
	"log/slog"
)

var update = flag.Bool("update", false, "rewrite golden files from current output")

// replayAtomizer returns a canned list of notes from a JSON file.
type replayAtomizer struct{ path string }

func (r *replayAtomizer) Atomize(_ context.Context, _ string, _ domain.Taxonomy) ([]domain.Note, error) {
	data, err := os.ReadFile(r.path)
	if err != nil {
		return nil, err
	}
	var items []struct {
		TitleSlug string   `json:"title_slug"`
		Category  string   `json:"category"`
		Tags      []string `json:"tags"`
		Body      string   `json:"body"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	notes := make([]domain.Note, 0, len(items))
	for _, it := range items {
		notes = append(notes, domain.Note{Category: it.Category, Tags: it.Tags, Slug: it.TitleSlug, Body: it.Body})
	}
	return notes, nil
}

type stubTranscriber struct{}

func (stubTranscriber) Transcribe(_ context.Context, _ io.Reader, _ string) (string, error) {
	return "", nil
}

func runCase(t *testing.T, caseDir string) {
	t.Helper()
	notesDir := t.TempDir()

	moscow, _ := time.LoadLocation("Europe/Moscow")
	fixedTime := time.Date(2026, 5, 27, 22, 40, 0, 0, moscow)

	store := fs.NewNoteStore(notesDir)
	taxLoader := fs.NewTaxonomyLoader(filepath.Join(caseDir, "taxonomy.yml"), 0)

	atomizer := &replayAtomizer{path: filepath.Join(caseDir, "atomizer_response.json")}

	uc := usecase.NewIngestUseCase(stubTranscriber{}, atomizer, store, taxLoader, "test-atomize", "test-transcribe")
	uc.WithFixedNow(func() time.Time { return fixedTime })
	uc.WithFixedDumpID("00000000-0000-0000-0000-000000000001")

	input, err := os.ReadFile(filepath.Join(caseDir, "input.txt"))
	require.NoError(t, err)

	result, topErr := uc.Execute(context.Background(), in.IngestRequest{
		Source: domain.SourceTelegramText,
		Text:   string(input),
	})

	reply := telegram.FormatReply(result, topErr)

	expectedDir := filepath.Join(caseDir, "expected")
	if *update {
		writeGolden(t, expectedDir, notesDir, reply)
		t.Logf("updated golden for %s", caseDir)
		return
	}

	compareDirs(t, filepath.Join(expectedDir), notesDir)
	expectedReply, err := os.ReadFile(filepath.Join(expectedDir, "reply.txt"))
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(string(expectedReply)), strings.TrimSpace(reply))
}

func compareDirs(t *testing.T, want, got string) {
	t.Helper()
	err := filepath.Walk(want, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(want, p)
		if rel == "reply.txt" {
			return nil
		}
		expected, _ := os.ReadFile(p)
		actual, _ := os.ReadFile(filepath.Join(got, rel))
		require.Equal(t, string(expected), string(actual), "mismatch in %s", rel)
		return nil
	})
	require.NoError(t, err)
}

func writeGolden(t *testing.T, expectedDir, notesDir, reply string) {
	t.Helper()
	_ = os.RemoveAll(expectedDir)
	require.NoError(t, os.MkdirAll(expectedDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(expectedDir, "reply.txt"), []byte(reply+"\n"), 0o644))
	err := filepath.Walk(notesDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(notesDir, p)
		dst := filepath.Join(expectedDir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
		data, _ := os.ReadFile(p)
		require.NoError(t, os.WriteFile(dst, data, 0o644))
		return nil
	})
	require.NoError(t, err)
}

func TestGoldenCases(t *testing.T) {
	root := "../../testdata/golden"
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			runCase(t, filepath.Join(root, e.Name()))
		})
	}
}

var _ out.Atomizer = (*replayAtomizer)(nil)
var _ out.Transcriber = stubTranscriber{}
var _ = slog.Default
```

- [ ] **Step 3: Добавить тестовые помощники в use case**

Modify `internal/usecase/ingest.go` — добавить два метода:
```go
// WithFixedNow overrides the now() function. Test-only.
func (u *IngestUseCase) WithFixedNow(now func() time.Time) {
	u.now = now
}

// WithFixedDumpID forces a deterministic dump ID for golden tests.
func (u *IngestUseCase) WithFixedDumpID(id string) {
	u.fixedDumpID = id
}
```

И добавить поле `fixedDumpID string` в struct, использовать его вместо `uuid.NewString()`:
```go
dumpID := u.fixedDumpID
if dumpID == "" {
    dumpID = uuid.NewString()
}
```

- [ ] **Step 4: Сгенерировать golden-файлы первым запуском**

Run:
```bash
go test -tags=integration -run TestGoldenCases -update ./internal/integration/...
```

Проверь содержимое `testdata/golden/case_01_text_happy/expected/` — должно быть похоже на эталон из Step 1, но в каноничном формате YAML, который реально пишет код. Скоммить как есть.

- [ ] **Step 5: Запустить тест без -update**

Run: `make test-int`
Expected: PASS.

- [ ] **Step 6: Добавить кейс 02 — Uncategorized**

`testdata/golden/case_02_uncategorized/`:
- `input.txt`: `Мысли о DeFi и комиссиях.`
- `taxonomy.yml`: тот же что в case_01 (без crypto)
- `atomizer_response.json`:
```json
[
  {"title_slug": "defi-fees", "category": "crypto/defi", "tags": [], "body": "Комиссии растут."}
]
```
- Сгенерировать expected через `-update`

- [ ] **Step 7: Добавить кейс 03 — empty result**

`testdata/golden/case_03_empty/`:
- `input.txt`: `мусор`
- `atomizer_response.json`: `[]`
- Сгенерировать expected через `-update`

- [ ] **Step 8: Run all integration tests**

Run: `make test-int`
Expected: PASS на всех трёх кейсах.

- [ ] **Step 9: Commit**

```bash
git add internal/integration internal/usecase/ingest.go testdata/golden
git commit -m "test(integration): golden-file tests for happy/uncategorized/empty cases"
```

---

## Task 28: Docker (Dockerfile + docker-compose + .env.example)

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`
- Modify: `.env.example` (re-gen после задач конфига)

- [ ] **Step 1: Создать `Dockerfile`**

```dockerfile
# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ingest ./cmd/ingest

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app app
COPY --from=builder /out/ingest /usr/local/bin/ingest
USER app
ENTRYPOINT ["/usr/local/bin/ingest"]
```

- [ ] **Step 2: Создать `docker-compose.yml`**

```yaml
services:
  ingest:
    build: .
    image: second-brain/ingest:dev
    restart: unless-stopped
    env_file: .env
    environment:
      TZ: Europe/Moscow
      NOTES_DIR: /data/notes
      CONFIG_DIR: /etc/second-brain
    volumes:
      - ./data/notes:/data/notes
      - ./config:/etc/second-brain
```

- [ ] **Step 3: Проверить сборку**

Run:
```bash
make docker-build
```

Expected: образ собирается, размер < 30MB (`docker images second-brain/ingest:dev`).

- [ ] **Step 4: Сгенерировать актуальный `.env.example`**

Run: `make env-example`
Expected: файл обновлён.

- [ ] **Step 5: Commit**

```bash
git add Dockerfile docker-compose.yml .env.example
git commit -m "build: multi-stage Dockerfile, docker-compose, refreshed .env.example"
```

---

## Task 29: README + финальная полировка

**Files:**
- Modify: `README.md`
- Verify: `make ci`

- [ ] **Step 1: Развернуть README**

```markdown
# Second Brain — Ingestion MVP

Telegram-бот превращает голосовые и текстовые сообщения в атомарные Markdown-заметки с YAML-фронтматтером. Хранилище — обычная файловая система, индексирование/RAG/телеметрия — отдельные следующие подсистемы (см. `docs/superpowers/specs/2026-05-27-ingestion-mvp-design.md`).

## Архитектура

Гексагональная: чистый `internal/domain`, входной порт `port/in` для use case `usecase/ingest`, выходные порты `port/out` для адаптеров `adapter/out/openrouter` (chat + whisper) и `adapter/out/fs`. Telegram-бот — driving adapter `adapter/in/telegram`. Композиция в `cmd/ingest/main.go`.

## Стек

Go 1.26 · OpenRouterTeam/go-sdk · go-telegram/bot · ilyakaznacheev/cleanenv · mockery v3 · gopkg.in/yaml.v3 · gosimple/slug · google/uuid · log/slog · Docker.

## Setup

```bash
# 1. Скачать репозиторий, установить toolchain
go mod download

# 2. Сгенерировать моки и .env.example
make mocks
make env-example

# 3. Подготовить конфиг
cp .env.example .env
cp testdata/taxonomy.yml.example config/taxonomy.yml
# отредактировать .env (OPENROUTER_API_KEY, TELEGRAM_BOT_TOKEN, ALLOWED_USER_IDS)
# отредактировать config/taxonomy.yml — список разрешённых категорий и тегов
```

## Run

```bash
# локально
make build && ./bin/ingest

# Docker
make run     # docker compose up --build
```

## Test

```bash
make test         # unit
make test-int     # integration (golden files)
make race         # race detector
make ci           # lint + test-int + race
make test-prompts # ручной smoke c реальной LLM (требует OPENROUTER_API_KEY)
```

## Структура репозитория

```
cmd/ingest               — entrypoint
internal/
  domain/                — чистое ядро (Note, Taxonomy, slug, ID, YAML)
  port/in, port/out      — интерфейсы (driving / driven ports)
  usecase/               — IngestUseCase
  adapter/in/telegram/   — Telegram driving adapter
  adapter/out/openrouter — Atomizer + Transcriber via OpenRouter
  adapter/out/fs         — NoteStore + TaxonomyLoader (файловая система)
  config/                — cleanenv-based env config
testdata/                — фикстуры для тестов
docs/superpowers/        — spec + plan
```

## Конвенции

См. §12 спеки. Ключевое: перед любой Go-правкой агенты вызывают skill `modern-go-guidelines:use-modern-go`. TDD для domain и use case.

## Лицензия

Private personal project.
```

- [ ] **Step 2: Прогон полного CI**

Run: `make ci`
Expected: `lint + test-int + race` всё зелёное.

- [ ] **Step 3: Smoke test composition (без реального токена, ожидаем graceful exit на отсутствии envs)**

Run:
```bash
unset TELEGRAM_BOT_TOKEN OPENROUTER_API_KEY ALLOWED_USER_IDS
./bin/ingest && echo "should not reach"; echo "exit=$?"
```

Expected: ингест падает с понятным сообщением про отсутствующие env, exit code 1.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: full README with setup, run, test, repo layout"
```

- [ ] **Step 5: Финальная проверка definition of done из спеки §11**

Чек-лист:
- [ ] `make ci` зелёное
- [ ] `docker build .` собирается, образ < 30MB
- [ ] `docker compose up` от чистой клон-копии поднимает бота за < 1 мин (требует валидные envs)
- [ ] README актуальный
- [ ] Ручные smoke зелёные: text dump, voice dump, voice > 25 мин (последнее — отказ с сообщением)
- [ ] Open questions §9 спеки либо закрыты, либо записаны как GitHub issues

---

## Notes for the executor

- **Order matters.** Tasks 1–10 строят фундамент (домен + порты + моки). С Task 11 начинаются use-case тесты на моках — без моков они не запустятся. Task 24–25 (telegram_handler + main.go) нужны раньше Docker-задач, чтобы было что собирать.
- **Если упрёшься в неуказанный тип/функцию** — сверься со спекой `docs/superpowers/specs/2026-05-27-ingestion-mvp-design.md` (она канон). Если расхождение реальное — пометь в коммите и в `docs/superpowers/specs/` (отдельный коммит с фиксом спеки).
- **Каждая задача — отдельный commit**, в идеале — отдельный PR. Сообщение коммита в формате `<type>(scope): <subject>`.
- **Перед написанием Go-кода** — invoke `modern-go-guidelines:use-modern-go` skill (см. §12.1 спеки).
- **Mocks regen:** при изменении интерфейсов `port.in`/`port.out` — `make mocks` и коммит сгенерированных файлов вместе с интерфейсом.
- **Golden update:** при намеренных изменениях в `FormatReply` или `MarshalNote` — `go test -tags=integration -update ./...` и коммит обновлённых `testdata/golden/`.

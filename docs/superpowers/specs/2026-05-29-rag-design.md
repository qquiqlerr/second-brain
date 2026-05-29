---
title: Semantic Index + RAG — Design
date: 2026-05-29
status: draft
sub-project: 2 (per ingestion-mvp §10 roadmap)
depends-on: 2026-05-27-ingestion-mvp-design.md
---

# Semantic Index + RAG — Design

Второй под-проект из roadmap-а ingestion-MVP. Добавляет над уже работающим
конвейером заметок:

- Эмбеддинги через Voyage (`voyage-4-large` по умолчанию)
- Векторный индекс в `sqlite-vec` (vec0 virtual table, in-process)
- Авто-перелинковку соседей в YAML frontmatter (`linked_notes`)
- Telegram-команду `/find` (semantic retrieval)

**RAG-синтез (`/ask`) и агентные сценарии — НЕ в v1.** План: накопить
2–4 недели реального использования `/find` + ручного обхода по
`linked_notes` в Obsidian, чтобы понять, какие запросы plain retrieval
не вытягивает. После этого — sub-project #6 «Agentic /ask» с tool-using
loop, построенный сразу под multi-hop reasoning.

Под-проект 1 (ingestion) — единственный prerequisite. Все остальные
под-проекты (3 telemetry, 4 watchdog) либо ортогональны, либо зависят от (2).

---

## 1. Цель и границы

### Цель

Дать сервису второй слой поверх корпуса MD-заметок: семантический поиск
и RAG-ответы через Telegram-бот. Источник правды остаётся файловая
система — индекс производный, перестраивается из заметок.

### Что в scope

- Порт `Embedder` + адаптер Voyage (`voyage-4-large`, модель через env)
- Порт `VectorIndex` + адаптер `sqlite-vec` (файл `data/index/index.db`)
- Use case `Indexer`: периодический FS-scan → embed → upsert → recompute `linked_notes`
- Schema bump: 1.0 → 1.1, добавляется `linked_notes: [id1, id2, ...]`
- Use case `Searcher.Find`: semantic retrieval top-K
- Telegram-команда `/find <query>` (top-N с заголовком, сниппетом, путём к файлу)

### Что вне scope (другие под-проекты или после v1)

- **`/ask` RAG-синтез и любые формы LLM-генерации поверх retrieval** — переносится в sub-project #6 (agentic /ask), будет строиться сразу с tool-using loop
- Re-ranking (cross-encoder)
- Hybrid search (BM25 + dense)
- Чанкование заметок на параграфы — атомы уже маленькие, эмбедим целиком
- Backlinks внутри MD — Obsidian считает их сам, мы пишем только forward `linked_notes`
- Telemetry (sub-project 3), Watchdog (sub-project 4)

### Success criteria

- Бот отвечает на `/find` за < 1.5с при 5K заметок в индексе
- При деплое на чистый VPS индексатор поднимает индекс с нуля без потерь
- При ручной правке заметки в Obsidian индекс обновляется в течение `RAG_SCAN_INTERVAL` (дефолт 5 мин)
- Два ручных smoke-сценария зелёные: новая заметка → `linked_notes` появились; `/find` возвращает релевантное

---

## 2. Архитектура

### 2.1 Hexagonal продолжается

Добавляем порты и адаптеры, существующий ingestion-pipeline не трогаем
кроме одной мелочи в §4.2 (поднимаем `SchemaVersion` на `"1.1"`).

### 2.2 Новые порты (`internal/port/out`)

```go
type Embedder interface {
    Embed(ctx context.Context, texts []string, kind EmbedKind) ([][]float32, error)
    Dim() int
}

type EmbedKind string
const (
    EmbedDocument EmbedKind = "document" // для индексации
    EmbedQuery    EmbedKind = "query"    // для поиска
)

type VectorIndex interface {
    Upsert(ctx context.Context, items []IndexItem) error
    SearchByVector(ctx context.Context, vec []float32, q SearchQuery) ([]SearchHit, error)
    GetMeta(ctx context.Context, id string) (IndexMeta, bool, error)
    GetEmbedding(ctx context.Context, id string) ([]float32, bool, error) // для linker
    UpdateLinkedNotes(ctx context.Context, id string, links []string) error
    DeleteByIDs(ctx context.Context, ids []string) error
    ListAllMeta(ctx context.Context) ([]IndexMeta, error) // для diff в indexer
}

type IndexItem struct {
    ID        string
    FilePath  string
    BodyHash  string
    Category  string
    Tags      []string
    Date      time.Time
    Kind      string  // atom|summary
    Embedding []float32
}
// NB: IndexItem не содержит LinkedNotes. Upsert обновляет всё кроме linked_notes
// (для INSERT — оставляет пустой массив; для UPDATE — не трогает существующее).
// LinkedNotes обновляется отдельным методом UpdateLinkedNotes из линкера.

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

type SearchQuery struct {
    TopK           int
    KindFilter     []string  // например ["atom"]
    CategoryPrefix string    // например "work/projects"
    DateFrom       time.Time // zero — без ограничения
}

type SearchHit struct {
    ID    string
    Score float32
    Meta  IndexMeta
}
```

Synthesizer-порт и связанные с `/ask` типы добавятся в sub-project #6.

### 2.3 Новые адаптеры (`internal/adapter/out`)

- `voyage/embedder.go` — HTTP-клиент к `https://api.voyageai.com/v1/embeddings`. Batching до 1000 элементов. При `EmbedKind` ставит соответствующий `input_type`. Retry — через generic `WithRetry`, который в этом PR извлекается из `openrouter/retry.go` в общий пакет `internal/adapter/httpretry/` (туда же переезжает `HTTPError`), чтобы оба адаптера переиспользовали его без cross-import-а между out-адаптерами.
- `sqlitevec/index.go` — обёртка над `mattn/go-sqlite3` + `asg017/sqlite-vec-go-bindings`. На `Open` грузит расширение vec0, держит две таблицы (см. 2.5). CGO обязателен.

### 2.4 Новые use cases (`internal/usecase`)

- `indexer/indexer.go` — оркестратор: scan FS → diff с `notes_meta` → embed batch → upsert → recompute `linked_notes` → atomic rewrite YAML. Запускается в горутине из `main`.
- `searcher/searcher.go` — `Find(ctx, query, opts) []Hit`. Только retrieval, без LLM-синтеза.

### 2.5 Структура индекса (sqlite-vec)

```sql
CREATE TABLE notes_meta (
  id            TEXT PRIMARY KEY,            -- из YAML
  file_path     TEXT NOT NULL,               -- относительно NOTES_DIR
  body_hash     TEXT NOT NULL,               -- sha256(body без frontmatter)
  category      TEXT NOT NULL,
  tags_json     TEXT NOT NULL,
  date          TEXT NOT NULL,               -- ISO 8601
  kind          TEXT NOT NULL,               -- atom|summary
  indexed_at    TEXT NOT NULL,
  linked_notes  TEXT NOT NULL DEFAULT '[]'   -- JSON array; source of truth для YAML
);

CREATE VIRTUAL TABLE notes_vec USING vec0(
  id        TEXT PRIMARY KEY,
  embedding FLOAT[1024]                      -- размерность из EMBEDDING_DIM
);

CREATE TABLE index_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
-- хранит embedding_model, embedding_dim, schema_version_of_indexer
```

Все три таблицы живут в одном файле `data/index/index.db`.

### 2.6 Идемпотентность scan

Indexer сравнивает текущий `sha256(body)` с `notes_meta.body_hash`. Изменения
**только во frontmatter** (включая нашу же запись `linked_notes`) не триггерят
re-embed. Это разрывает потенциальный цикл «индексер пишет linked_notes →
mtime меняется → индексер видит изменение → re-embed → пишет linked_notes...».

### 2.7 Структура файлов

```
internal/
  domain/
    note.go                 # дополняется LinkedNotes; SchemaVersion → "1.1"
    errors.go               # +ErrEmbedder, +ErrVectorIndex
  port/
    out/
      embedder.go           # NEW
      vector_index.go       # NEW
  adapter/
    httpretry/              # NEW — извлечено из openrouter/retry.go
      retry.go
      http_error.go
      retry_test.go
    out/
      voyage/               # NEW
        embedder.go
        embedder_test.go
        client.go
      sqlitevec/            # NEW
        index.go
        index_integration_test.go
        schema.go
      openrouter/
        retry.go            # удаляется (переехало в adapter/httpretry)
    in/
      telegram/
        cmd_find.go         # NEW
        dispatcher.go       # модифицируется: роутинг /find отдельно от dump
  usecase/
    indexer/
      indexer.go            # NEW
      diff.go               # NEW (чистая функция, юнит-тесты)
      linker.go             # NEW
      indexer_test.go
    searcher/
      searcher.go           # NEW (только Find)
      searcher_test.go
cmd/
  ingest/
    main.go                 # модифицируется: DI новых компонентов, запуск indexer-goroutine
```

---

## 3. Data Flow

### 3.1 Indexer (фоновая горутина)

Запускается в `main` после wiring всех зависимостей. Owned by app.

```
loop:
    select {
    case <-ctx.Done():  return
    case <-time.After(RAG_SCAN_INTERVAL):
        if err := runOnce(ctx); err != nil {
            slog.Error("indexer.run", "err", err)
        }
    }
```

`runOnce` (с одним sync.Mutex — overlapping cycles невозможны):

```
1. fsList := walk(NOTES_DIR) → []{path, mtime, id (из имени файла)}
   Файлы без валидного YAML — пропустить с WARN
2. dbList := vec.ListAllMeta(ctx)
3. toEmbed, toDelete := diff(fsList, dbList):
     - file in fs && id ∉ db                    → toEmbed (new)
     - file in fs && sha256(body) ≠ db.body_hash → toEmbed (changed)
     - id in db && id ∉ fs                       → toDelete (removed manually)
4. for batch of INDEXER_BATCH_SIZE from toEmbed:
     a. парсим каждый файл → domain.Note + body
     b. vectors := embedder.Embed(ctx, bodies, EmbedDocument)
     c. items := zip(notes, vectors)
     d. vec.Upsert(ctx, items)   // транзакция в адаптере
5. vec.DeleteByIDs(ctx, toDelete)
6. if len(toEmbed) > 0: linker.Recompute(ctx, touchedIDs)
```

### 3.2 Линкер (`linker.Recompute(seedIDs)`)

Линкер обрабатывает **только заметки с `kind == "atom"`**. Summaries в v1
не получают `linked_notes` — они метазаметки по дням, линковать их не имеет
смысла (тематика разрозненная).

```
// 1. собираем set затронутых: сами changed + соседи changed
affected := set()
for id in seedIDs where meta(id).Kind == "atom":
    affected.add(id)
    seedVec := vec.GetEmbedding(id)
    neighbors := vec.SearchByVector(seedVec, SearchQuery{
        TopK: LINK_TOP_K + 1,
        KindFilter: []string{"atom"},
    })
    for n in neighbors:
        if n.ID != id: affected.add(n.ID)

// 2. для каждой затронутой пересчитываем её собственные linked_notes
for id in affected:
    selfMeta := vec.GetMeta(id)
    selfVec := vec.GetEmbedding(id)
    hits := vec.SearchByVector(selfVec, SearchQuery{
        TopK: LINK_TOP_K + 1,
        KindFilter: []string{"atom"},
    })
    new_links := []
    for h in hits:
        if h.ID == id: continue
        if h.Score < LINK_MIN_SIMILARITY: break  // hits sorted desc
        new_links.append(h.ID)
        if len(new_links) >= LINK_TOP_K: break

    cur := json.unmarshal(selfMeta.LinkedNotes)
    if equal(new_links, cur):
        continue                                  // no-op, не трогаем диск
    vec.UpdateLinkedNotes(id, new_links)
    rewriteYAMLFrontmatter(selfMeta.FilePath, linked_notes=new_links, schema_version="1.1")
```

Каскад не делаем: когда линкер обновил B (из-за изменений A), теоретически
C мог бы тоже захотеть переподтянуть линки. Этим пренебрегаем — линки
сойдутся за несколько циклов scanner-а. Это закладывается осознанно ради
ограниченности одного `runOnce`.

`rewriteYAMLFrontmatter` — пишет через `*.tmp` + `os.Rename`. Body заметки
не трогается → `body_hash` стабилен → следующий scan не считает заметку
изменённой.

Параметры (env с дефолтами):

- `RAG_SCAN_INTERVAL` = `5m`
- `LINK_TOP_K` = `5`
- `LINK_MIN_SIMILARITY` = `0.70`
- `INDEXER_BATCH_SIZE` = `1000`

### 3.3 Backfill

В первом `runOnce` после деплоя `dbList` пустой → ВСЕ заметки попадают в
`toEmbed`. Voyage держит rate-limit, но batch 1000 — это один HTTP-запрос.
На 5K заметок: ~5 запросов, ~3–5с. На 50K заметок: ~50 запросов, ~30–60с.

### 3.4 `/find <query>`

```
1. bot validates: query non-empty, len < 500
2. вытащить опциональные модификаторы: `#category=work/projects`, `since:2026-05-01`
3. qvec := embedder.Embed(ctx, [query], EmbedQuery)[0]
4. hits := vec.SearchByVector(qvec, SearchQuery{
     TopK: FIND_TOP_K,
     KindFilter: []string{"atom"},
     CategoryPrefix: ...,
     DateFrom: ...,
   })
5. reply:
     1. {category}/{slug}  ({score:.2f})
        {first 200 chars of body}
        `{relative file path}`

     2. ...
```

Ожидаемо < 1с: HTTP к Voyage ~200мс + sqlite-vec на 5K векторов <10мс + Telegram send.

---

## 4. Структура данных

### 4.1 Schema 1.0 → 1.1

Добавляется одно опциональное поле в frontmatter:

```yaml
---
id: 20260527-tms-auth-bug
schema_version: "1.1"          # bumped
date: ...
source: ...
category: ...
tags: [...]
linked_notes:                   # NEW, omitempty
  - 20260524-jwt-refresh-flow
  - 20260520-tms-auth-prelim
  - 20260515-appsheet-session-leak
ingest:
  dump_id: ...
  model_atomize: ...
---
Body...
```

Правила:

- `linked_notes` — `omitempty`. Пока заметка не пройдёт через индексер, поля нет вообще
- Порядок — по убыванию similarity
- Длина — ровно `LINK_TOP_K`, или меньше, если соседей с `≥ LINK_MIN_SIMILARITY` не нашлось
- Парсер для `schema_version: "1.0"` остаётся рабочим (поле просто отсутствует)

### 4.2 `domain.Note` (расширение)

```go
type Note struct {
    // existing fields...
    LinkedNotes []string `yaml:"linked_notes,omitempty"`
}

const SchemaVersion = "1.1"  // bump
```

Атомайзер из под-проекта 1 после этого PR пишет `SchemaVersion = "1.1"`,
но без `LinkedNotes`. Через 1 цикл индексера поле появится.

### 4.3 Миграция старых 1.0-заметок

Никакой отдельной миграции. Индексер на первом старте видит все старые
1.0-файлы как `toEmbed` (их `id` нет в `notes_meta`), эмбедит, считает
соседей, дописывает `linked_notes` + поднимает `schema_version` тем же
atomic-rewrite в `rewriteYAMLFrontmatter`.

### 4.4 Конфликты с ручными правками

Сценарий: пользователь в Obsidian отредактировал тело заметки, не успел
синкнуть → indexer мог уже считать `linked_notes` для соседей. После
следующего scan:

- `body_hash` изменился → re-embed
- Линкер пересчитает соседей этой заметки
- Тех, кого выкинули из новых соседей, индексер сам подцепит на следующем витке через `affected := neighbors of changed`

Никаких «грязных» состояний на диске больше пары минут не висит.

### 4.5 Параллельный доступ к файлам

Индексер пишет → Obsidian читает. Atomic `rename(2)` на тот же inode
безопасен: Obsidian увидит либо старую, либо новую версию, не половинку.

Индексер ↔ ingest (атомайзер). Оба пишут в `data/notes/`. Атомайзер создаёт
новый файл (новый id), индексер перезаписывает существующие. Коллизия
возможна только если индексер начал переписывать `X.md` за миллисекунду до
того, как атомайзер пишет `X.md`. Имя файла включает `id` (timestamp+slug)
→ дубликаты исключены. Безопасно без локов.

---

## 5. Обработка ошибок

### 5.1 Доменные типы

В `internal/domain/errors.go` добавляются:

```go
var (
    ErrEmbedder    = errors.New("embedder")
    ErrVectorIndex = errors.New("vector index")
    ErrSearchEmpty = errors.New("no results")
)
```

Use case-слой работает только с доменными типами. Адаптеры заворачивают:
`fmt.Errorf("%w: %w", ErrEmbedder, err)`.

### 5.2 Стратегия по стадиям

| Стадия | Сбой | Реакция |
|---|---|---|
| Indexer: walk FS | I/O error на одной заметке | log WARN, пропускаем файл, продолжаем |
| Indexer: parse YAML | malformed frontmatter | log WARN с file_path, пропускаем |
| Indexer: embed batch | Voyage 429 / 5xx / timeout | retry policy (5.3); если все ретраи провалились — log ERROR, аборт batch, не commit. Следующий цикл попробует |
| Indexer: sqlite-vec upsert | I/O / corruption | log ERROR, аборт `runOnce`. На следующем цикле повтор |
| Indexer: YAML rewrite | rename failed | log WARN; `notes_meta.linked_notes` НЕ обновляем (источник правды — диск). На следующем цикле пересчитаем |
| `/find`: embed query | Voyage недоступен | Telegram-ответ: «Поиск временно недоступен, попробуй позже» + log ERROR |
| `/find`: search | sqlite locked / corrupted | то же сообщение, log ERROR |
| `/find`: пустой retrieval | 0 hits | ответ «Ничего не нашёл» |

### 5.3 Retry policy

Переиспользуем `openrouter.WithRetry` (он generic над `func(ctx) error`).
Voyage-адаптер использует те же дефолты:

- `MaxAttempts: 3`
- `BaseDelay: 1s`
- `MaxDelay: 8s`
- `Jitter: 0.3`

Retryable: 429, 5xx, `net.Error` с `Timeout()` или `Temporary()`.

### 5.4 Частичный успех в индексере

Если batch упал — rollback всей tx. Не «полузаписи». Следующий цикл увидит
те же файлы как `toEmbed` (их `body_hash` в БД не сменился).

Линкер работает поверх уже-успешно-эмбеженного. Если линкер падает на одной
заметке — log WARN и продолжаем со следующей в `affected` set. `linked_notes`
для упавшей останутся старыми, что лучше чем nil.

### 5.5 Логирование

Все логи структурные через `log/slog`. Ключевые поля:

- `op` — `indexer.run`, `indexer.embed`, `indexer.link`, `searcher.find`
- `note_id`, `dump_id`, `query_hash` (sha1 первых 32 байт)
- `duration_ms`, `n_embedded`, `n_linked`, `n_deleted`
- `error` на ERROR уровнях

Пример:

```json
{"level":"INFO","op":"indexer.run","duration_ms":3120,"n_embedded":1247,"n_linked":1389,"n_deleted":0}
```

### 5.6 Panic recovery

Indexer-горутина обёрнута в `defer recover()` с логом + рестартом через 30с.
Паника в обработке Telegram-команды — обрабатывается на уровне bot-фреймворка
(так же как в под-проекте 1).

### 5.7 Graceful shutdown

`main` подписан на SIGTERM/SIGINT. При shutdown:

- Останавливает приём новых Telegram-команд
- Ждёт завершения текущих `/find` (или ctx.timeout 10с)
- Indexer `ctx.cancel` — текущий `runOnce` доходит до ближайшей точки проверки (между batch-ами), затем выходит
- sqlite-vec `db.Close`

---

## 6. Тестирование

### 6.1 Пирамида

| Слой | Что тестируем | Чем |
|---|---|---|
| Domain | parse YAML 1.0/1.1, error wrapping | unit, plain `go test` |
| Indexer | diff algorithm, корректность affected-set, retry на embedder-сбое | unit + mockery v3 для портов |
| Linker | top-K, threshold, дедуп, исключение self | unit, fake-embedder |
| Searcher | формат hit, фильтры по category/date, fallback при пустом retrieval | unit + mockery |
| Adapter `sqlitevec` | upsert/search/delete на real sqlite-vec | integration (`-tags=integration`) |
| Adapter `voyage` | сериализация запроса/ответа, classify HTTP errors | unit (`httptest.Server`) |
| End-to-end | seed N MD-файлов → `indexer.runOnce` → `/find` → проверить порядок | integration |

### 6.2 Тестовые данные

- `testdata/notes/` — 20 заранее подготовленных MD-файлов с фиксированными body, разными категориями
- Fake `Embedder` возвращает векторы по `sha256(text) → 1024 float32`. Стабильно между прогонами, ортогонально по разумным текстам

### 6.3 Конкретные сценарии

```
domain:
  - parse_v10_no_linked_notes
  - parse_v11_with_linked_notes
  - parse_v11_empty_linked_notes_omits_field

usecase/indexer:
  - empty_db_embeds_all_files
  - body_hash_unchanged_skips_embedding
  - body_hash_changed_triggers_reembed
  - deleted_file_removes_from_index
  - embedder_failure_aborts_batch
  - rewrite_yaml_failure_logs_keeps_old_links_in_db

usecase/searcher:
  - find_returns_top_n_sorted_by_score
  - find_with_category_prefix_filter
  - find_with_date_from_filter
  - find_returns_empty_on_no_hits

linker:
  - picks_top_k_above_threshold
  - skips_self_in_neighbors
  - no_op_when_links_unchanged

adapter/sqlitevec (integration):
  - upsert_and_search_roundtrip
  - delete_removes_from_both_tables
  - search_handles_dim_mismatch_error
  - vec0_extension_loaded_on_open

adapter/voyage:
  - happy_path_returns_vectors
  - 429_classified_retryable
  - 400_classified_terminal
  - batch_split_respects_max_inputs
  - sends_correct_input_type_for_document_vs_query
```

### 6.4 Manual smoke

После деплоя:

1. Подождать `RAG_SCAN_INTERVAL` от первого старта → проверить, что у старых заметок появились `linked_notes` в YAML
2. `/find мысли про auth` → top-5 релевантных
3. Удалить одну заметку через `rm` → дождаться следующего цикла → `/find` не возвращает удалённую

### 6.5 CI

Юнит-тесты — в текущем `verify` job. Integration с `-tags=integration` уже
в pipeline. Образ теперь CGO — `setup-go` обязан включить gcc; в alpine
билдере это `gcc musl-dev sqlite-dev`. См. §7.7.

---

## 7. Конфигурация и развёртывание

### 7.1 Новые env-переменные

| Имя | Default | Назначение |
|---|---|---|
| `VOYAGE_API_KEY` | — (required) | Ключ Voyage |
| `EMBEDDING_MODEL` | `voyage-4-large` | Модель Voyage |
| `EMBEDDING_DIM` | `1024` | Размерность; должен совпадать с моделью |
| `INDEX_DB_PATH` | `/data/index/index.db` | Файл sqlite-vec |
| `RAG_SCAN_INTERVAL` | `5m` | Период FS-scan |
| `INDEXER_BATCH_SIZE` | `1000` | Размер embed-batch |
| `LINK_TOP_K` | `5` | Сколько соседей в `linked_notes` |
| `LINK_MIN_SIMILARITY` | `0.70` | Порог cosine similarity для линка |
| `FIND_TOP_K` | `5` | Сколько hit-ов возвращает `/find` |

Все читаются через тот же `cleanenv` config. `VOYAGE_API_KEY` —
обязательный, fatal при отсутствии.

### 7.2 GitHub Secrets

Никаких новых секретов в Actions. `VOYAGE_API_KEY` живёт **только** в
локальном `.env`, который синкается через `sync-prod.sh --env`. CI/CD
ключ не трогает.

### 7.3 `docker-compose.prod.yml`

Добавляется отдельный volume и env:

```yaml
volumes:
  - ./data/notes:/data/notes
  - ./data/index:/data/index       # NEW
  - ./config:/etc/second-brain
environment:
  TZ: Europe/Moscow
  NOTES_DIR: /data/notes
  CONFIG_DIR: /etc/second-brain
  INDEX_DB_PATH: /data/index/index.db   # NEW
```

Отдельная директория от `data/notes/`, чтобы FS-scanner не путал
`index.db` с заметкой. Init-flow в `sync-prod.sh --init` дополнить
созданием `~/second-brain/data/index/`.

### 7.4 Бэкап

`data/notes/` уже бэкапится (план под-проекта 1). `data/index/` **не
бэкапим** — индекс восстановим из заметок ребилдом. Это часть design:
индекс — derivative state, источник правды — MD-файлы.

### 7.5 Размер образа

`mattn/go-sqlite3` тянет CGO + libsqlite3. Образ вырастет с текущих
~25MB до ~45–50MB. Приемлемо.

### 7.6 Откат

Если в `/find` или линкере найдётся регрессия:

- `git revert` коммит → CI → автодеплой → рестарт без RAG
- Файлы `linked_notes` в YAML останутся — это валидный optional field в schema 1.1, Obsidian их не покажет, не помешает
- `data/index/index.db` остаётся на диске; следующий деплой с фиксом начнёт с него же (или удалить вручную для clean-state)

### 7.7 Dockerfile под CGO

Текущий Dockerfile собирает с `CGO_ENABLED=0`. Это меняется:

- **Builder stage** (alpine): `apk add --no-cache git gcc musl-dev sqlite-dev`
- **Build command**: убрать `CGO_ENABLED=0`, оставить `go build -trimpath -ldflags="-s -w" -o /out/ingest ./cmd/ingest`
- **Runtime stage**: `apk add --no-cache ca-certificates tzdata sqlite-libs`

`sqlite-vec-go-bindings` поставляется как Go-asset с native libs, компилировать
сам vec не понадобится — но `libsqlite3` нужен для базового sqlite. Если на
alpine/musl pre-built нативка vec не сработает — см. §8.1, фоллбек на
`debian-slim` в runtime-stage.

---

## 8. Open questions

Не блокеры для имплементации, фиксируем для финальных решений по ходу:

1. **Бандлинг `sqlite-vec`-нативки в alpine/musl.** Pre-built бинари у `asg017/sqlite-vec` — для glibc. Возможно понадобится либо переключиться на debian-slim в runtime-stage, либо собирать vec из C. Сначала пробуем как есть, фоллбек — debian-slim (+10MB).
2. **`LINK_MIN_SIMILARITY = 0.70` — догадка.** После первого реального прогона на корпусе пользователя смотрим распределение similarities; если линки получаются пустыми или шумными — двигаем.
3. **Re-embed при смене модели.** Используем таблицу `index_meta` с `embedding_model`, `embedding_dim`. При старте сравниваем с env: если поменялась только модель (та же размерность) — `DELETE FROM notes_vec; DELETE FROM notes_meta;` + полный backfill. Если поменялась размерность — `DROP TABLE notes_vec; CREATE VIRTUAL TABLE notes_vec USING vec0(... FLOAT[NEW_DIM])` + полный backfill. Оба случая логируются на WARN с явной причиной.
4. **Slash-command vs свободный текст.** Бот сейчас принимает любой текст как dump. `/find` парсим как command-only, минимум магии.

---

## 9. Зависимости в roadmap

- **Под-проект 1 (ingestion)** — этот RAG требует MD-файлов с валидным YAML. Готов
- **Под-проект 3 (telemetry)** — добавит блок `telemetry:` во frontmatter. **Ортогонально нам**: RAG не парсит этот блок, индексер просто проигнорирует поле. Никаких конфликтов
- **Под-проект 4 (watchdog & self-healing)** — будет читать sqlite-vec индекс для трекинга density/drift. **Зависит от (2)**: эта спека закладывает таблицу `index_meta` для конфиг-versioning, чем watchdog потом воспользуется
- **Под-проект 6 (agentic `/ask`)** — следующий шаг после (2). Использует те же `Embedder`, `VectorIndex.SearchByVector`, `notes.Reader` как **tools** в LLM tool-use loop (Anthropic tool-use API). Multi-hop reasoning, follow `linked_notes`, refine queries. Спека пишется после 2–4 недель реального использования `/find`, когда станет понятно, какие классы запросов plain retrieval не закрывает

При переходе схемы:

- `schema_version: "1.0" → "1.1"` (этот спек добавляет `linked_notes`)
- Будущий `"1.2"` придёт с (3) добавкой `telemetry`

---

## 10. Конвенции для агентов и контрибьюторов

Перечисляются дельты к §12 ingestion-спеки. Всё остальное остаётся в силе.

### 10.1 Modern Go

Без изменений. Перед любой Go-правкой — обязательно вызвать skill
`modern-go-guidelines:use-modern-go`, в т.ч. в субагентах.

### 10.2 Adapter discipline для sqlite-vec

- Импорт `github.com/mattn/go-sqlite3` — **только** в адаптере `internal/adapter/out/sqlitevec/`
- Загрузка vec0-расширения — в `NewIndex(...)` constructor
- Все SQL-запросы — параметризованные. Никакого `fmt.Sprintf` для user-input

### 10.3 Embedder discipline

- `voyage`-адаптер не знает про `domain.Note`. Принимает `[]string`, возвращает `[][]float32`. Маппинг «note → text для эмбединга» живёт в use case `indexer`
- Текст для эмбединга — `body` без YAML frontmatter
- `EmbedKind` (document vs query) обязателен — Voyage даёт разное качество для двух режимов

### 10.4 TDD для нового слоя

- `indexer.diff` — чистая функция, юнит-тестируется первой
- Линкер top-K — юнит-тест с детерминированным fake-embedder
- sqlite-vec адаптер — integration tests (real DB в temp dir), не mock

### 10.5 PR-структура

Под-проект режется на несколько PR (детали — в плане):

1. Domain (errors, Note bump, SchemaVersion → 1.1) + порты + extract `adapter/httpretry` из `adapter/out/openrouter/retry.go`
2. Adapter voyage + тесты
3. Adapter sqlite-vec + integration тесты + Dockerfile под CGO
4. Use case indexer (diff + linker) + тесты
5. Use case searcher (`Find`) + Telegram-команда `/find`
6. compose.prod.yml + sync-prod.sh --init обновления (env + volume для index.db)

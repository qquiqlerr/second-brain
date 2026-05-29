# Ingestion MVP — Design Spec

**Date:** 2026-05-27
**Status:** Draft, awaiting review
**Scope:** Sub-project 1 of 4 (см. roadmap в конце документа)

---

## 1. Цель и границы

### Цель
Минимальный сквозной слой системы «Второй Мозг»: пользователь шлёт в Telegram голосовое или текстовое сообщение → сервис превращает дамп в набор атомарных Markdown-заметок с YAML-фронтматтером в файловой системе.

### Что в scope
- Telegram-бот: приём текста и voice от allowlist-пользователей
- Транскрибация голосовых через OpenRouter (Whisper)
- Атомизация дампа через OpenRouter (Claude 3.5 Haiku / GPT-4o-mini / Gemini Flash Lite — конфигурируемо)
- Валидация против `taxonomy.yml` (strict whitelist + fallback в `uncategorized`)
- Запись MD-файлов в иерархию `notes/<category>/<id>.md`
- Сводный ответ пользователю в Telegram

### Что вне scope (другие под-проекты)
- **Sub-project 2 — Semantic Index + RAG:** эмбеддинги, векторная БД, авто-перелинковка `linked_notes`, бот-запросы к базе
- **Sub-project 3 — Telemetry Sync:** Strava + Google Fit → блок `telemetry` в YAML
- **Sub-project 4 — Watchdog & Self-Healing:** density / clustering / fallback overflow / RAG degradation триггеры, reindex, LLM-миграции таксономии

Поля `telemetry` и `linked_notes` в YAML **не появляются в файлах MVP**, добавятся в schema_version 1.1+.

### Success criteria
1. Допустимый дамп (текст ≤ 8KB или voice ≤ 25 мин) превращается в ≥1 MD-файл с валидным YAML за < 30с (p95)
2. Все файлы соответствуют схеме 1.0; валидируются программно
3. Невалидные категории не теряются — уходят в `uncategorized/` с сохранением `original_category`
4. Сервис переживает рестарт без потери записанных заметок
5. Smoke-тест пайплайна (Telegram-сообщение → MD-файл) можно запустить локально через docker-compose

---

## 2. Архитектура

### 2.1 Стиль: Hexagonal (Ports & Adapters)

```
┌─────────────────────────────────────────────────────────────┐
│  Driving adapters (inbound)         Driven adapters (out)   │
│  ┌────────────────┐                ┌──────────────────────┐ │
│  │ adapter/in/    │                │ adapter/out/         │ │
│  │   telegram/    │                │   openrouter/        │ │
│  └───────┬────────┘                │   fs/                │ │
│          │                          └──────────┬───────────┘ │
│          ▼                                     ▲             │
│       ┌──────────────────────────────────────────┐           │
│       │ port/in/  ←   usecase/   →  port/out/    │           │
│       └─────────────────┬────────────────────────┘           │
│                         ▼                                    │
│                     ┌─────────┐                              │
│                     │ domain/ │  ← pure, zero external deps  │
│                     └─────────┘                              │
└─────────────────────────────────────────────────────────────┘
```

**Правила зависимостей:**
- `domain` импортирует только стандартную библиотеку
- `port` импортирует только `domain`
- `usecase` импортирует `domain` + `port`
- `adapter` импортирует `domain` + `port` (никогда другой adapter и никогда `usecase` напрямую — только через `port.in`)
- `cmd/ingest/main.go` — composition root, единственное место, где конкретные адаптеры встречаются с use case

### 2.2 Layout проекта

```
cmd/
  ingest/main.go                  # composition root

internal/
  domain/                         # pure core
    note.go                       # Note entity, ID-правила, slug
    dump.go                       # Dump value object
    taxonomy.go                   # Taxonomy, Normalize(Note), ValidateNote
    errors.go                     # типизированные доменные ошибки

  port/
    in/
      ingest.go                   # IngestDumpUseCase (driving port)
    out/
      transcriber.go              # Transcriber (driven port)
      atomizer.go                 # Atomizer (driven port)
      note_store.go               # NoteStore (driven port)
      taxonomy_loader.go          # TaxonomyLoader (driven port)
    mocks/                        # сгенерированные mockery моки

  usecase/
    ingest.go                     # IngestUseCase: orchestrates pipeline
    ingest_test.go                # tests с mockery mocks port.out.*

  adapter/
    in/
      telegram/                   # driving adapter
        bot.go                    # long polling, allowlist
        handler.go                # update → IngestDumpUseCase
        reply.go                  # форматирование сводки
        dedup.go                  # LRU дедуп по update_id
    out/
      openrouter/                 # driven adapter
        client.go                 # обвязка над OpenRouterTeam/go-sdk + raw HTTP
        atomizer.go               # → port.out.Atomizer (через s.Chat.Send)
        transcriber.go            # → port.out.Transcriber (raw HTTP к /audio/transcriptions)
        prompt.go                 # шаблон + рендеринг таксономии
        retry.go                  # унифицированный retry policy
      fs/
        note_store.go             # → port.out.NoteStore
        taxonomy_loader.go        # → port.out.TaxonomyLoader (mtime cache)

  config/config.go                # env-парсинг, валидация на старте

testdata/
  taxonomy.yml.example            # стартовый шаблон
  dumps/                          # фикстуры дампов
  golden/                         # ожидаемые MD-файлы для integration

.mockery.yml                      # конфигурация mockery v3
Makefile
Dockerfile
docker-compose.yml
```

### 2.3 Ключевые контракты

```go
// port/in/ingest.go
type IngestRequest struct {
    Source    domain.DumpSource    // TelegramText | TelegramVoice
    Text      string               // для text-дампов
    Audio     io.ReadCloser        // для voice-дампов
    AudioMIME string
    UserID    int64                // для трассировки
}

type IngestResult struct {
    Notes         []domain.Note    // успешно записанные
    Paths         []string
    Uncategorized int              // сколько из Notes ушло в fallback
    Errors        []error          // ошибки записи отдельных заметок
}

type IngestDumpUseCase interface {
    Execute(ctx context.Context, req IngestRequest) (IngestResult, error)
}

// port/out/*.go
type Transcriber interface {
    Transcribe(ctx context.Context, audio io.Reader, mime string) (string, error)
}

type Atomizer interface {
    Atomize(ctx context.Context, dump string, tax domain.Taxonomy) ([]domain.Note, error)
}

type NoteStore interface {
    Write(ctx context.Context, n domain.Note) (path string, err error)
}

type TaxonomyLoader interface {
    Load(ctx context.Context) (domain.Taxonomy, error)
}
```

### 2.4 Архитектурный паттерн обработки

Синхронный pipeline в горутине на каждое Telegram-сообщение. Без очереди, без БД состояний. Эволюция в очередь (`asynq`/SQLite) — если потребуется на будущих этапах; интерфейсы готовы к этому.

---

## 3. Data Flow

### 3.1 Полный путь сообщения

```
1. Telegram Update → bot.go (long polling)
2. allowlist check: From.ID ∈ ALLOWED_USER_IDS?
     нет → silent drop (лог "rejected")
3. dedup by update_id (LRU 1000) → если уже видели, skip
4. type detect:
     Text       → req.Source = TelegramText
     Voice      → проверка Duration ≤ 1500с (25 мин)
                  превышено → reply «Голосовое > 25 мин, разбей»
                  иначе: getFile + download → req.Audio
     прочее     → reply «поддерживаются текст и голос»
5. correlationID = uuid → в ctx
6. bot шлёт «⏳ обрабатываю...»
7. result, err := usecase.Execute(ctx, req)
8. reply.Format(result, err) → bot.SendMessage
```

### 3.2 Use case orchestration

```go
func (u *IngestUC) Execute(ctx, req) (IngestResult, error) {
    tax, err := u.tax.Load(ctx)
    if err != nil { return IngestResult{}, fmt.Errorf("load taxonomy: %w", err) }

    text := req.Text
    if req.Source == domain.TelegramVoice {
        text, err = u.transcriber.Transcribe(ctx, req.Audio, req.AudioMIME)
        if err != nil { return IngestResult{}, fmt.Errorf("transcribe: %w", err) }
        if strings.TrimSpace(text) == "" {
            return IngestResult{}, domain.ErrEmptyDump
        }
    }

    notes, err := u.atomizer.Atomize(ctx, text, tax)
    if err != nil { return IngestResult{}, fmt.Errorf("atomize: %w", err) }

    dumpID := uuid.New().String()
    result := IngestResult{}
    for _, n := range notes {
        n = tax.Normalize(n)
        if n.Category == domain.CategoryUncategorized {
            result.Uncategorized++
        }
        n.Ingest.DumpID = dumpID
        n.ID = domain.BuildID(n.Date, n.Slug)
        path, err := u.store.Write(ctx, n)
        if err != nil {
            result.Errors = append(result.Errors, err)
            continue
        }
        result.Notes = append(result.Notes, n)
        result.Paths = append(result.Paths, path)
    }
    return result, nil
}
```

### 3.3 Atomize: контракт с LLM

Адаптер `openrouter.Atomizer` шлёт `s.Chat.Send(ctx, components.ChatRequest{...})`:

- **System prompt:** правила атомизации + сериализованный `taxonomy.AllCategoryPaths()` + `taxonomy.Tags` + инструкция «ответ — JSON-массив по схеме»
- **User prompt:** сырой `dump`
- **`ResponseFormat`:** JSON schema (если модель поддерживает; иначе валидируем + 1 ретрай с фидбэком)
- **Temperature:** 0.3 (детерминизм важнее креативности)

Ожидаемая JSON-схема ответа:
```json
[
  {
    "title_slug": "tms-auth-bug",
    "category": "work/projects/tms",
    "tags": ["appsheet", "backend", "auth", "bug"],
    "body": "Здесь идёт кратко отформатированный текст мысли..."
  }
]
```

Адаптер парсит JSON → `[]domain.Note` (без `ID`/`DumpID` — заполняются в use case).

### 3.4 Transcribe: контракт с Whisper

OpenRouterTeam SDK не покрывает `/audio/transcriptions`. Реализация — raw HTTP через `*http.Client`, переиспользуемый из SDK:

```go
// adapter/out/openrouter/transcriber.go
type transcriber struct {
    httpClient *http.Client            // тот же, что в openrouter.WithClient(...)
    baseURL    string                  // https://openrouter.ai/api/v1
    apiKey     string
    model      string                  // openai/whisper-1
}

func (t *transcriber) Transcribe(ctx, audio io.Reader, mime string) (string, error) {
    // multipart POST {baseURL}/audio/transcriptions
    //   file=<audio>, model=<model>
    // Headers: Authorization: Bearer {apiKey}
    // parse {"text": "..."}
}
```

**Формат аудио.** Telegram voice — OGG/Opus (MIME `audio/ogg`). Whisper API принимает форматы `flac, mp3, mp4, mpeg, mpga, m4a, oga, ogg, wav, webm` — OGG поддерживается нативно, **никаких ffmpeg-конверсий в MVP не нужно**. Файл проксируется в multipart-запросе как есть, имя `voice.ogg`.

Если SDK позже добавит нативный метод — меняем только тело адаптера, порт `Transcriber` остаётся.

### 3.5 Validate / Normalize в domain

```go
// domain/taxonomy.go
func (t Taxonomy) Normalize(n Note) Note {
    if !t.CategoryAllowed(n.Category) {
        n.OriginalCategory = n.Category
        n.Category = CategoryUncategorized
    }
    n.Tags = t.FilterTags(n.Tags)
    return n
}
```

Категория вне whitelist → `original_category` сохраняет исходник, `category` = `"uncategorized"`. Невалидные теги просто отфильтровываются (в MVP не сохраняем — sub-project 4 решит, как с ними работать).

### 3.6 ID, slug, путь

```go
// domain/note.go
func BuildID(date time.Time, slug string) string {
    return fmt.Sprintf("%s-%s", date.Format("20060102"), slug)
}
```

- **Date:** момент получения дампа ботом, в TZ `Europe/Moscow`
- **Slug:** приходит от LLM, нормализуется в domain: ASCII kebab-case, ≤ 40 символов, кириллица → транслит
- **Имя файла:** `{ID}.md`
- **Путь:** `{NOTES_DIR}/{category}/{ID}.md` (категория `work/projects/tms` → `{NOTES_DIR}/work/projects/tms/...`)
- **Коллизии:** `{ID}.md` существует → `{ID}-2.md`, `{ID}-3.md`. **Резолвит коллизию `fs_store.Write`** — он подбирает свободное имя, обновляет `n.ID` финальным значением (`-2`/`-3`) и только потом сериализует YAML. Инвариант: `id` поле в YAML всегда совпадает с именем файла без `.md`.

### 3.7 Telegram reply

`reply.Format` рендерит сводку:

```
✅ Сохранено 5 заметок:
• [tms-auth-bug] (work/projects/tms) #appsheet #bug
• [sleep-experiment] (health/sleep) #idea #experiment
• ...
📦 2 уехали в Uncategorized
⚠️ 1 заметка не записалась (см. логи)
```

Кейсы:
- Полный успех — только список + опц. строка про Uncategorized
- Частичный — список + строка про ошибки записи
- Полная ошибка — `❌ {краткое сообщение}` (transcribe/atomize/taxonomy fail)
- 0 заметок — `🤔 не нашёл идей в дампе`

---

## 4. Структура данных

### 4.1 YAML frontmatter (schema_version 1.0)

**Пример валидной заметки (voice → work/projects/tms):**

```yaml
---
id: 20260527-tms-auth-bug
schema_version: "1.0"
date: 2026-05-27T22:40:00+03:00
source: telegram-voice
category: work/projects/tms
tags: [appsheet, backend, auth, bug]
ingest:
  dump_id: 8c4a1f3e-...
  model_atomize: anthropic/claude-3.5-haiku
  model_transcribe: openai/whisper-1
---
Здесь идёт кратко отформатированный текст мысли...
```

**Пример заметки, ушедшей в Uncategorized (LLM предложил неизвестную категорию `crypto/defi`):**

```yaml
---
id: 20260527-defi-fee-spike
schema_version: "1.0"
date: 2026-05-27T22:40:00+03:00
source: telegram-text
category: uncategorized
original_category: crypto/defi
tags: [finance]
ingest:
  dump_id: 8c4a1f3e-...
  model_atomize: anthropic/claude-3.5-haiku
---
Думаю, что комиссии на L2 будут расти...
```

**Правила сериализации:**
- `model_transcribe` — `omitempty`, отсутствует для `source: telegram-text`
- `original_category` — `omitempty`, отсутствует для всего, кроме `category: uncategorized`
- Поля `telemetry`, `linked_notes` — **не существуют в schema 1.0** (добавятся в schema 1.1+ другими под-проектами)

### 4.2 `domain.Note`

```go
type Note struct {
    ID               string
    SchemaVersion    string          // "1.0"
    Date             time.Time
    Source           DumpSource
    Category         string
    OriginalCategory string          // omitempty
    Tags             []string
    Slug             string
    Body             string
    Ingest           IngestMeta
}

type IngestMeta struct {
    DumpID          string
    ModelAtomize    string
    ModelTranscribe string          // omitempty
}

type DumpSource string
const (
    TelegramText  DumpSource = "telegram-text"
    TelegramVoice DumpSource = "telegram-voice"
)

const CategoryUncategorized = "uncategorized"
```

### 4.3 `taxonomy.yml`

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

**Парсинг категорий:**
- Каждый ключ в `categories` — валидная категория (включая внутренние узлы)
- Значение ключа: либо `null` (`meetings:`) — leaf без потомков, либо map (`projects: ...`) — узел с потомками, либо список строк (`- tms`) — серия leaf-категорий на текущем уровне
- Валидное значение `category` в Note — любой полный путь от корня: `work`, `work/projects`, `work/projects/tms`, `work/meetings`
- LLM в промпте получает плоский список всех путей через `taxonomy.AllCategoryPaths()`

**Теги** — плоский whitelist строк.

**Загрузка:** `taxonomy_loader` читает файл при старте + кеширует с TTL=60с и mtime-check; правки `taxonomy.yml` подхватываются без рестарта.

**Расположение:** `${CONFIG_DIR}/taxonomy.yml` (env, по умолчанию `/etc/second-brain/taxonomy.yml` в контейнере).

**Бутстрап:** `testdata/taxonomy.yml.example` копируется пользователем. Отсутствие файла на старте → fatal.

### 4.4 Uncategorized

- Категория `uncategorized` — единственное допустимое значение вне whitelist
- Файл пишется в `{NOTES_DIR}/uncategorized/{id}.md`
- `original_category` хранит ровно то, что LLM выдал
- Это сигнал для будущего watchdog (sub-project 4)

### 4.5 Конфигурация (cleanenv)

Парсинг и валидация env — через [`github.com/ilyakaznacheev/cleanenv`](https://github.com/ilyakaznacheev/cleanenv). Никакой ручной валидации: required-флаги, дефолты и парсинг типов (`time.Duration`, `[]int64`) описываются тегами. На старте делается ровно один вызов `cleanenv.ReadEnv(&cfg)`; любая ошибка → `log.Fatal`.

```go
// internal/config/config.go
package config

import "time"

type Config struct {
    // OpenRouter
    OpenRouterAPIKey     string `env:"OPENROUTER_API_KEY"        env-required:"true"      env-description:"OpenRouter API key (sk-or-...)"`
    OpenRouterBaseURL    string `env:"OPENROUTER_BASE_URL"       env-default:"https://openrouter.ai/api/v1"`
    AtomizeModel         string `env:"OPENROUTER_ATOMIZE_MODEL"  env-default:"anthropic/claude-3.5-haiku"`
    TranscribeModel      string `env:"OPENROUTER_TRANSCRIBE_MODEL" env-default:"openai/whisper-1"`
    HTTPReferer          string `env:"OPENROUTER_HTTP_REFERER"   env-default:"https://github.com/aleksejmetlusko/second-brain"`
    XTitle               string `env:"OPENROUTER_X_TITLE"        env-default:"Second Brain"`

    // Telegram
    TelegramBotToken     string  `env:"TELEGRAM_BOT_TOKEN"  env-required:"true"`
    AllowedUserIDs       []int64 `env:"ALLOWED_USER_IDS"    env-required:"true"      env-separator:","  env-description:"CSV of allowlisted Telegram user IDs"`

    // Storage
    NotesDir             string `env:"NOTES_DIR"           env-default:"/data/notes"`
    ConfigDir            string `env:"CONFIG_DIR"          env-default:"/etc/second-brain"`

    // Timeouts
    HTTPTimeout          time.Duration `env:"HTTP_TIMEOUT"       env-default:"120s"`
    TranscribeTimeout    time.Duration `env:"TRANSCRIBE_TIMEOUT" env-default:"180s"`
    AtomizeTimeout       time.Duration `env:"ATOMIZE_TIMEOUT"    env-default:"60s"`
    FSWriteTimeout       time.Duration `env:"FS_WRITE_TIMEOUT"   env-default:"5s"`

    // Misc
    TZ                   string `env:"TZ"                  env-default:"Europe/Moscow"`
    LogLevel             string `env:"LOG_LEVEL"           env-default:"info"        env-description:"debug | info | warn | error"`
}

func Load() (Config, error) {
    var cfg Config
    if err := cleanenv.ReadEnv(&cfg); err != nil {
        return Config{}, fmt.Errorf("load config: %w", err)
    }
    return cfg, nil
}
```

**Что даёт cleanenv:**
- `env-required:"true"` — отсутствие переменной → понятная ошибка (`field "OpenRouterAPIKey" is required but the value is not provided`), без ручных `if cfg.X == ""` проверок
- `env-default:"..."` — типобезопасные дефолты прямо в теге
- `[]int64` с `env-separator:","` — авто-парсинг `ALLOWED_USER_IDS=12345,67890`
- `time.Duration` парсится из строки (`"120s"`, `"5m30s"`) автоматически
- `env-description` — используется при генерации `.env.example` через `cleanenv.GetDescription(&cfg, nil)` (доменно-специфичный кастомный helper в `make env-example`)

**Доменно-специфическая валидация** (например, проверка что `LogLevel ∈ {debug, info, warn, error}`) — отдельные методы `Config.Validate()` после `Load()`. cleanenv покрывает синтаксис; семантику валидируем явно.

**Файл `.env.example`** генерируется автоматически из тегов через `make env-example` — чтобы документация и код не расходились.

---

## 5. Обработка ошибок

### 5.1 Доменные типы

```go
// domain/errors.go
var (
    ErrEmptyDump            = errors.New("dump is empty after transcription")
    ErrTaxonomyMissing      = errors.New("taxonomy.yml not found")
    ErrTaxonomyMalformed    = errors.New("taxonomy.yml is malformed")
    ErrAtomizerBadResponse  = errors.New("atomizer returned invalid response")
    ErrAtomizerNoNotes      = errors.New("atomizer returned zero notes")
)

type TranscriberError struct{ Provider, Reason string; Retryable bool }
type AtomizerError    struct{ Provider, Reason string; Retryable bool }
type StoreError       struct{ Path, Reason string }
```

Use case оперирует только этими типами + стандартными `context.Canceled`/`DeadlineExceeded`. Адаптерная специфика (HTTP-коды, sdkerrors.\*) остаётся внутри адаптеров.

### 5.2 Стратегия по стадиям

| Стадия | Failure | Поведение |
|---|---|---|
| Bootstrap | Невалидный config, нет `taxonomy.yml`, недоступность OpenRouter | `log.Fatal`, контейнер падает, оркестратор рестартит |
| Telegram poll | Network blip, 5xx | Бесконечный ретрай с экспоненциальным backoff (1s → 30s капа) на уровне SDK |
| Audio download | Telegram getFile fail | 1 ретрай 2s → reply `❌ не удалось скачать голосовое` |
| Transcribe | OpenRouter 429/5xx | 2 ретрая с jitter (1s, 4s) → reply `❌ транскрибация недоступна` |
| Transcribe | Пустой результат | reply `❌ не распознал речь, повтори` |
| Atomize | OpenRouter 429/5xx | 2 ретрая с jitter → reply `❌ LLM недоступна` |
| Atomize | Невалидный JSON | 1 ретрай с уточняющей инструкцией → reply `❌ некорректный ответ модели` + лог |
| Atomize | 0 заметок | reply `🤔 не нашёл идей в дампе` (не ошибка) |
| Validate | Категория/тег вне whitelist | Не ошибка — fallback в `uncategorized` |
| Store.Write | Disk full / permission | Лог ERROR + накопление в `result.Errors`; продолжаем; в сводке `⚠️ M не записались` |
| Store.Write | Коллизия имени | Не ошибка — auto-rename (`{id}-2.md`) |
| Любая | `ctx.Done()` | Прерываем, не пишем reply; уже записанные файлы остаются |

### 5.3 Retry policy (унифицирована)

```go
type RetryPolicy struct {
    MaxAttempts int           // 3
    BaseDelay   time.Duration // 1s
    MaxDelay    time.Duration // 8s
    Jitter      float64       // 0.3
}
```

Ретраим только: `context.DeadlineExceeded`, HTTP 429, 500, 502, 503, 504, network errors.
Не ретраим: 400, 401, 403, 422 — повтор не поможет.
Каждая попытка логируется с `attempt=N`.

### 5.4 Частичный успех

First-class case. Use case возвращает `(result, nil)` если хотя бы одна заметка записана. `nil` error = «процесс дошёл до конца», ошибки видны в `result.Errors`. Возвращает `(zero, err)` только если упала общая стадия (transcribe/atomize).

### 5.5 Логирование

- `log/slog` с JSON handler
- Обязательные поля каждого лога: `correlation_id` (= `dump_id`), `user_id`, `source`, `stage`
- Уровни: `DEBUG` (тела запросов/ответов LLM), `INFO` (стадии), `WARN` (ретраи, fallback в uncategorized), `ERROR` (фейлы)
- Маска `Authorization` header
- Полный текст промпта + ответ при ERROR атомизатора — для дебага

### 5.6 Panic recovery

`defer recover()` в `bot/handler.go` вокруг каждого update. Лог ERROR + reply `❌ внутренняя ошибка`. Процесс не падает.

### 5.7 Graceful shutdown

`main.go` слушает `SIGTERM`/`SIGINT`:
1. Останавливает приём updates
2. Ждёт завершения in-flight pipelines (`sync.WaitGroup`), таймаут 30с
3. По истечении — `os.Exit(1)` с логом

---

## 6. Тестирование

### 6.1 Пирамида

```
                Manual E2E (Telegram + real OpenRouter) — чек-лист на релиз
              Integration (golden files: dump → MD)
            Use case tests (mockery моки port.out)
          Adapter tests (httptest, t.TempDir)
        Domain unit tests
```

Цель: 80% покрытия — domain + use case (быстро, детерминированно). Адаптеры — фокусно, на парсинге внешних форматов.

### 6.2 Mockery v3

`.mockery.yml`:
```yaml
template:testify
packages:
  github.com/aleksejmetlusko/second-brain/internal/port/in:
    config:
      dir: "internal/port/in/mocks"
      filename: "mock_{{.InterfaceName}}.go"
      pkgname: "mocks"
      structname: "Mock{{.InterfaceName}}"
    interfaces:
      IngestDumpUseCase:
  github.com/aleksejmetlusko/second-brain/internal/port/out:
    config:
      dir: "internal/port/out/mocks"
      filename: "mock_{{.InterfaceName}}.go"
      pkgname: "mocks"
      structname: "Mock{{.InterfaceName}}"
    interfaces:
      Transcriber:
      Atomizer:
      NoteStore:
      TaxonomyLoader:
```

Генерация: `make mocks` → `mockery`. Сгенерированные файлы коммитятся.

Шаблон теста use case:
```go
func TestExecute_TextDump_Happy(t *testing.T) {
    atomizer := outmocks.NewMockAtomizer(t)
    store    := outmocks.NewMockNoteStore(t)
    taxLdr   := outmocks.NewMockTaxonomyLoader(t)

    taxLdr.EXPECT().Load(mock.Anything).Return(testTaxonomy, nil).Once()
    atomizer.EXPECT().Atomize(mock.Anything, "dump text", mock.Anything).
        Return([]domain.Note{n1, n2, n3}, nil).Once()
    store.EXPECT().Write(mock.Anything, mock.Anything).Return("/path.md", nil).Times(3)

    uc := usecase.NewIngestUseCase(nil, atomizer, store, taxLdr)
    result, err := uc.Execute(ctx, req)

    require.NoError(t, err)
    require.Len(t, result.Notes, 3)
    // EXPECT() auto-asserted через t.Cleanup
}
```

### 6.3 Domain unit-тесты

`internal/domain/*_test.go` — pure Go, без внешних зависимостей.

Покрытие:
- `Taxonomy.CategoryAllowed` — корень, лист, промежуточный путь, отсутствующий, регистр, мусор
- `Taxonomy.FilterTags` — все валидные / невалидные / смесь / пустой массив
- `Taxonomy.Normalize` — категория ок / в fallback / `original_category` заполнен
- `BuildID` — формат `YYYYMMDD-slug`, граница TZ Moscow
- `slug.Normalize` — кириллица → транслит, пробелы → `-`, спецсимволы, длина > 40
- YAML round-trip — `Marshal`→`Unmarshal` для всех полей, `omitempty` для опциональных

### 6.4 Use case тесты (с mockery)

`internal/usecase/ingest_test.go`. Сценарии:

| Тест | Что проверяет |
|---|---|
| `TestExecute_TextDump_Happy` | Текстовый дамп, 3 заметки, всё сохранено |
| `TestExecute_VoiceDump_Happy` | Transcriber вызван, результат проброшен в Atomizer |
| `TestExecute_VoiceDump_EmptyTranscript` | Возвращает `ErrEmptyDump` |
| `TestExecute_AtomizerFails` | Обёрнутая ошибка, ничего не пишется |
| `TestExecute_OneCategoryInvalid` | Невалидная категория → `uncategorized`, `result.Uncategorized=1` |
| `TestExecute_PartialStoreFailure` | Store падает на 2-й → 2 в `Notes`, 1 в `Errors`, err=nil |
| `TestExecute_AllStoreFails` | Store падает на всех → err≠nil |
| `TestExecute_ContextCanceled` | ctx.cancel в полёте → `context.Canceled` |
| `TestExecute_TaxonomyLoadFails` | Ранний возврат, Transcriber не вызван |
| `TestExecute_DumpIDPropagated` | `IngestMeta.DumpID` одинаков у всех заметок дампа |

### 6.5 Adapter-тесты

**`adapter/out/openrouter/`** — `httptest.NewServer` фейкового OpenRouter:
- Формирование запроса: метод, путь, headers, тело
- Парсинг успешного ответа `/chat/completions` → `[]domain.Note`
- Парсинг успешного ответа `/audio/transcriptions` → string
- Retry on 429/503, no-retry on 400/401
- Невалидный JSON → `AtomizerError`
- Timeout → `context.DeadlineExceeded`

Фикстуры: `testdata/openrouter/{chat_3notes.json, chat_invalid.json, chat_429.json, transcribe_ok.json}`.

**`adapter/out/fs/`** в `t.TempDir()`:
- Запись в новую иерархию (`MkdirAll`)
- Коллизия → `-2`, `-3`
- Идемпотентность YAML-сериализации (один Note → байт-в-байт одинаковый файл)
- Запись в `uncategorized/`
- `taxonomy_loader`: валидный / malformed / mtime-кеш

**`adapter/in/telegram/`** с mockery-моком `IngestDumpUseCase`:
- Allowlist (silent drop)
- text vs voice роутится
- voice > 25 мин → reply, UC не вызван
- dedup по `update_id`
- `reply.Format` для каждого вида результата

### 6.6 Integration: golden files

`integration_test.go` (build tag `integration`):

```
testdata/golden/
  case_01_simple_text/
    input.txt              # сырой дамп
    atomizer_response.json # подменённый ответ OpenRouter
    taxonomy.yml
    expected/
      work/projects/tms/20260527-tms-auth-bug.md
      reply.txt
```

Тест собирает реальный use case + реальный `fs_store` + реальный `taxonomy_loader`, заменяет `Atomizer` на `replayAtomizer{file: ...}`. Сравнивает `t.TempDir()` против `expected/` байт-в-байт. Регенерация: `go test -update`.

3-5 кейсов на старте: happy, voice→text, Uncategorized, partial store-fail, empty.

### 6.7 Manual smoke

`make test-prompts` — реальный OpenRouter с 3 моделями на 5 эталонных дампах, печать diff. Не в CI, только для тюнинга промпта.

### 6.8 CI

Makefile цели:
```
make mocks       # генерация моков
make lint        # golangci-lint
make test        # unit + use case + adapter
make test-int    # + integration с golden
make race        # go test -race ./...
make ci          # = lint + test-int + race; цель < 30с
```

---

## 7. Зависимости

| Назначение | Пакет | Обоснование |
|---|---|---|
| OpenRouter SDK | `github.com/OpenRouterTeam/go-sdk` | Официальный, type-safe, встроенный retry/streaming/errors |
| Telegram bot | `github.com/go-telegram/bot` | Активный maintainership, чистый API, без `tgbotapi` legacy |
| Env config | `github.com/ilyakaznacheev/cleanenv` | Декларативный парсинг env через теги (`env-required`, `env-default`, `env-separator`), убирает ручную валидацию |
| YAML | `gopkg.in/yaml.v3` | Стандарт de-facto |
| Mock generation | `github.com/vektra/mockery/v3` | Go-стандарт, EXPECT() API, auto-assert |
| UUID | `github.com/google/uuid` | DumpID correlation |
| Slug normalize | `github.com/gosimple/slug` | Кириллица → транслит из коробки |
| Logging | `log/slog` (stdlib) | Структурное JSON-логирование |
| Tests | `testify` (`require`, `assert`, `mock`) | Совместимо с mockery |

Никаких других зависимостей в MVP. Без ORM, без DI-фреймворков, без gRPC.

---

## 8. Развёртывание

### 8.1 Dockerfile

Multi-stage build:
1. `golang:1.23-alpine` → сборка статического бинаря
2. `alpine:3.20` → копия бинаря + ca-certificates + tzdata

Размер итогового образа — цель < 30MB.

### 8.2 docker-compose.yml

```yaml
services:
  ingest:
    build: .
    restart: unless-stopped
    env_file: .env
    volumes:
      - ./data/notes:/data/notes
      - ./config:/etc/second-brain
    environment:
      TZ: Europe/Moscow
```

### 8.3 Запуск

```
cp testdata/taxonomy.yml.example config/taxonomy.yml
# отредактировать config/taxonomy.yml
cp .env.example .env
# заполнить OPENROUTER_API_KEY, TELEGRAM_BOT_TOKEN, ALLOWED_USER_IDS
docker compose up -d
docker compose logs -f
```

### 8.4 Бэкап

MD-файлы — единственная правда. Тома `./data/notes` и `./config` бэкапятся пользовательскими средствами (Time Machine / Syncthing / git push). В MVP сервис не отвечает за бэкап.

---

## 9. Open questions

Не блокеры для имплементации, но требуют решения по ходу:

1. **Слаги на кириллице:** транслит может быть некрасивым (`моя-идея` → `moya-ideya`). Альтернатива — позволить кириллицу в имени файла (Obsidian держит). Решим после первых реальных дампов.
2. **Длина body:** ограничивать ли в схеме? Сейчас — нет, но если LLM в одном «атоме» вернёт 2KB текста, это не атомарно. Добавить лог WARN при `len(body) > 1500`.
3. **Telegram bot framework выбор:** между `go-telegram/bot` и `mymmrac/telego`. Выбор делается на этапе реализации.
4. **JSON-схема response_format:** не все модели OpenRouter одинаково реализуют strict JSON output. Стратегия — пытаться использовать; при отказе модели или невалидном ответе делать 1 ретрай с явной инструкцией.

---

## 10. Roadmap (другие под-проекты)

Каждый — отдельный спек после завершения MVP.

| # | Под-проект | Зависит от |
|---|---|---|
| 1 | **Ingestion MVP** (этот документ) | — |
| 2 | **Semantic Index + RAG:** эмбеддинги (Voyage/local bge-small), векторная БД (SQLite+VSS), авто-перелинковка `linked_notes`, RAG-запросы через бота | (1) — устоявшийся формат MD |
| 3 | **Telemetry Sync:** Strava + Google Fit → блок `telemetry` в YAML, ночной cron | (1) — формат YAML |
| 4 | **Watchdog & Self-Healing:** density / clustering drift / fallback overflow / RAG degradation триггеры, reindex, LLM-миграции таксономии | (1), (2) — индекс и накопленные данные |
| 5 | **Daily Notes:** Telegram-команда `/daily-note <текст>` для коротких мыслей в течение дня в обход atomize-пайплайна (append-only в `data/notes/daily/YYYY-MM-DD.md` строкой `- HH:MM — <текст>`), + сборка `/daily-digest` или cron (≈23:55 локального TZ) в единый файл с таймштампами, опционально через LLM-саммари. Детали — отдельный спек. | (1) |
| 6 | **Agentic `/ask`:** LLM tool-use loop поверх `Embedder` + `VectorIndex` + `notes.Reader` из под-проекта 2. Multi-hop reasoning, follow `linked_notes`, refine queries. Спека пишется после 2–4 недель реального использования `/find`, когда накопится сигнал, какие запросы plain retrieval не закрывает. | (2) |

При переходе с одной фазы на следующую:
- `schema_version` инкрементируется (`1.0` → `1.1` для (3), `1.2` для (2))
- LLM-миграция (sub-project 4) умеет переписывать старые файлы под новую схему
- Адаптеры добавляются, порты могут расширяться

---

## 11. Что считается «готово»

- Все тесты (`make ci`) проходят локально и в CI
- Docker-образ собирается, < 30MB
- `docker compose up` от чистой клон-копии репозитория поднимает рабочий бот за < 1 мин
- README с инструкциями `setup → run → test`
- 3 ручных smoke-сценария зелёные: text dump, voice dump, voice > 25 мин (отказ)
- Все open questions из §9 либо решены, либо явно записаны в issue tracker

---

## 12. Конвенции для агентов и контрибьюторов

### 12.1 Modern Go (обязательно)

Перед написанием или редактированием любого Go-кода в этом репозитории — и для главного агента, и для каждого диспатченного субагента — **обязательно вызывать skill `modern-go-guidelines:use-modern-go`** через `Skill` tool.

Скилл подгружает рекомендации по модерн-синтаксису Go, выровненные под версию из `go.mod`. Это:
- `range over int` вместо `for i := 0; i < n; i++`
- `slices`/`maps`/`cmp` из stdlib вместо рукописных хелперов
- Дженерики там, где они уместны
- Структурное логирование через `log/slog`
- Современные паттерны контекстов и таймаутов

**Когда:** в начале сессии, как только становится понятно, что будет правка Go.

**Как обеспечить в субагентах:** при дисптаче через `Agent` tool в текст промпта вписать строку:
> "Invoke `modern-go-guidelines:use-modern-go` skill before writing any Go code."

### 12.2 Структура работы

- **Спека → план → имплементация.** Спека (этот документ) уже есть. Имплементационный план составляется через skill `superpowers:writing-plans` после её принятия. Только после плана начинается код.
- **TDD для domain и use case.** Эти слои чистые, тесты пишутся первыми. Адаптеры — допустимо тесты вместе с реализацией, потому что контракт фиксирован портом.
- **Один PR — один логический шаг.** Не смешивать введение порта и адаптера к нему с реализацией use case в одном PR; ревью должно умещаться в голову.
- **Никаких косметических рефакторов** в чужих PR. Если видишь беспорядок — отдельный PR.

### 12.3 Стиль кода

- `golangci-lint` обязателен в CI; конфиг в репозитории, не подавляем правила локальными `//nolint` без объяснения
- Зависимости — `go mod tidy` после каждого `go get`, лишних транзитивов в `go.sum` не оставляем
- Комментарии: только там, где «зачем» неочевиден из кода; что код делает — видно из имён
- Доменные пакеты (`internal/domain/*`) — **никаких импортов** кроме stdlib

### 12.4 Промпты в LLM

- Системный промпт атомизатора живёт в `internal/adapter/out/openrouter/prompt.go` как `embed`-ресурс из `prompts/atomize_system.txt`
- При правке промпта — обязательно прогнать `make test-prompts` на 3 моделях и приложить diff в PR
- Версия промпта в логах атомизатора (`prompt_version=N`) — инкрементить при изменениях

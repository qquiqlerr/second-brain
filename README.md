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

### Docker (recommended)

`docker compose` подхватывает `.env` автоматически и монтирует `./config` и `./data/notes`:

```bash
make run
# или: docker compose up --build
```

### Локально (без Docker)

`.env` shell не загружает — нужно явно source'нуть и переопределить пути:

```bash
make build
mkdir -p data/notes
set -a; source .env; set +a
CONFIG_DIR=$PWD/config NOTES_DIR=$PWD/data/notes ./bin/ingest
```

`CONFIG_DIR` по умолчанию `/etc/second-brain` (это путь внутри Docker-контейнера). При локальном запуске обязательно переопредели на `$PWD/config`, иначе бот не найдёт `taxonomy.yml`.

## Test

```bash
make test         # unit
make test-int     # integration (golden files)
make race         # race detector
make ci           # lint + test-int + race
make test-prompts # ручной smoke c реальной LLM (требует OPENROUTER_API_KEY)
```

## Deployment

Production-инстанс крутится на VPS, образ собирается GitHub Actions'ом и пушится в GHCR. Полный дизайн: `docs/superpowers/specs/2026-05-28-cicd-design.md`.

### Workflow
- Push в feature-ветку или PR → CI: `lint + unit + integration + docker build` (без push в registry)
- Merge в `main` → Deploy: те же проверки + сборка образа + push `ghcr.io/qquiqlerr/second-brain:latest` и `:sha-<7>` + SSH на VPS → `docker compose pull && up -d`

### Локальный синк конфигов
`.env` и `taxonomy.yml` живут только на dev-машине и на VPS, в git их нет. Заливка через `scripts/sync-prod.sh`:

```bash
make sync-env         # .env → recreate контейнера (~3с downtime)
make sync-taxonomy    # taxonomy.yml → hot-reload без рестарта
make sync-compose     # docker-compose.prod.yml → recreate контейнера
make sync-all         # всё сразу + один docker compose pull && up -d
```

Первичное развёртывание: `./scripts/sync-prod.sh --init` (создаёт каталоги на VPS, заливает все три файла, поднимает контейнер).

Скрипт подключается через SSH-алиас (по умолчанию `second-brain-vps`), который должен быть прописан в `~/.ssh/config`.

### Откат
SSH на VPS → `cd ~/second-brain` → заменить тег `:latest` на нужный `:sha-XXXXXXX` в `docker-compose.yml` → `docker compose up -d`. Все `:sha-*` теги живут в GHCR навсегда.

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

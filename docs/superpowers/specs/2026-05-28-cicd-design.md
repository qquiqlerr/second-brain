# CI/CD Design — Second Brain

**Дата:** 2026-05-28
**Автор:** Alexey Metlushko
**Статус:** Draft → User review
**Scope:** Инфраструктурный sub-project (между Ingestion MVP и Semantic Index + RAG)

---

## 1. Цель и границы

### Цель
Автоматизировать доставку изменений из локального репозитория на production-VPS: PR → проверка → merge в main → сборка образа → деплой на сервер. Плюс удобный локальный скрипт для синхронизации конфигов (`.env`, taxonomy, compose) с правильной семантикой rolling-restart.

### Что в scope
- Публичный GitHub-репозиторий `qquiqlerr/second-brain`
- GitHub Actions: два workflow (CI на PR, deploy на main)
- Сборка Docker-образа в GHCR (`ghcr.io/qquiqlerr/second-brain`), теги `:latest` и `:sha-<short>`
- SSH-pull deploy: GitHub Actions ходит по SSH на VPS и делает `docker compose pull && up -d`
- Локальный скрипт `scripts/sync-prod.sh` для заливки конфигов на VPS
- Документация в README по развёртыванию

### Что вне scope
- Staging-окружение (только prod)
- Semver-релизы и changelog
- Уведомления о деплоях (Slack/Telegram)
- Multi-arch образы (только amd64)
- Health-чек endpoint и graceful shutdown beyond Docker SIGTERM
- Бэкап `data/notes` (это отдельная задача, не CI/CD)
- Хранение `.env` в GitHub Secrets и его рендер в deploy-workflow

### Success criteria
1. Push в feature-ветку запускает CI (lint + unit + integration + проверка docker-build); красный CI не даёт смержить
2. Merge в main → автоматический деплой нового образа на VPS за ≤ 5 минут
3. `./scripts/sync-prod.sh --env` обновляет токены на VPS с downtime ≤ 5с
4. `./scripts/sync-prod.sh --taxonomy` обновляет таксономию **без рестарта** контейнера
5. Откат на любой предыдущий коммит — SSH → правка тега в compose → `up -d` (≤ 1 минута)
6. Секреты (`.env`, `taxonomy.yml`) живут **только** на VPS и на dev-машине, никогда не попадают в git и в GitHub Secrets

---

## 2. Архитектура

### 2.1 Общая схема

```
        ┌──────────────────────────────────────────────┐
        │              GitHub                          │
        │                                              │
   PR ─► │  .github/workflows/ci.yml                   │
        │    lint → unit → integration → build (no push)│
        │                                              │
   merge│  .github/workflows/deploy.yml                │
   main │    lint → tests → build → push GHCR          │
        │                       │                      │
        │                       ▼                      │
        │             ghcr.io/qquiqlerr/second-brain   │
        │                  :latest, :sha-<7>           │
        │                       │                      │
        │       ssh ───┐        │                      │
        └──────────────┼────────┼──────────────────────┘
                       │        │
                       ▼        ▼ docker pull (public, no auth)
                   ┌────────────────────────────────────┐
                   │             VPS (amd64)            │
                   │                                    │
                   │  ~/second-brain/                   │
                   │    docker-compose.yml (prod copy)  │
                   │    .env  ◄── редактируется руками  │
                   │    config/taxonomy.yml             │
                   │    data/notes/  ◄── volume         │
                   │                                    │
                   │  restart: unless-stopped           │
                   └────────────────────────────────────┘
```

### 2.2 Ключевые принципы

- **Образы immutable.** Каждый коммит в main производит уникальный `:sha-<short>` тег; `:latest` — alias на голову main. Откат — это смена тега в `docker-compose.yml` на VPS, без пересборки.
- **Pull-модель.** Сервер не подключается к GitHub. GitHub Actions через SSH запускает `docker compose pull && up -d`. Образ публичный → нет docker login на VPS.
- **Двойной гейт перед prod.** Один и тот же lint+test набор запускается и в CI (на PR), и в deploy workflow (на main). Дешёво, но защищает от ситуации, когда main как-то обошёл CI.
- **Секреты не в git и не в Actions.** `.env` и `taxonomy.yml` живут только на dev-машине и на VPS. GitHub Secrets хранит ровно то, что нужно для SSH-доставки: координаты сервера и deploy-ключ.
- **Hot-reload по типу файла.** `.env`/compose → recreate контейнера (`docker compose up -d`). `taxonomy.yml` → без рестарта, `TaxonomyLoader` детектит mtime.

### 2.3 Структура файлов в репозитории (новые/изменённые)

```
.github/
  workflows/
    ci.yml             # NEW — PR + push в non-main
    deploy.yml         # NEW — push в main
docker-compose.yml     # без изменений (dev, build: .)
docker-compose.prod.yml # NEW — image: ghcr.io/..., pull_policy: always
scripts/
  sync-prod.sh         # NEW — синк конфигов на VPS
Makefile               # UPDATED — цели sync-env / sync-taxonomy / sync-compose / sync-all
README.md              # UPDATED — секция Deployment
```

### 2.4 Структура на VPS

```
~/second-brain/
  docker-compose.yml      # копия docker-compose.prod.yml из репо
  .env                    # секреты, редактируется через sync-prod.sh --env
  config/
    taxonomy.yml          # таксономия, hot-reload через sync-prod.sh --taxonomy
  data/
    notes/                # volume для атомов и summaries
```

Владелец каталога — deploy-пользователь, входящий в группу `docker`.

---

## 3. CI workflow (`.github/workflows/ci.yml`)

### Триггеры
- `pull_request` на любую ветку
- `push` на любую ветку **кроме** `main`

### Джобы
Один job `verify` со следующими шагами:
1. `actions/checkout@v4`
2. `actions/setup-go@v5` с `go-version-file: go.mod` и `cache: true`
3. `golangci/golangci-lint-action@v6` (конфиг — существующий `.golangci.yml`)
4. `go test -count=1 ./...` — unit-тесты
5. `go test -count=1 -tags=integration ./...` — golden-тесты (replayAtomizer, не бьют по API)
6. `docker/build-push-action@v6` с `push: false`, `platforms: linux/amd64`, `cache-from`/`cache-to: type=gha` — sanity-проверка Dockerfile

### Что НЕ запускается в CI
- `make test-prompts` (тег `prompts`) — реально дёргает OpenRouter, стоит денег. Остаётся ручной командой.
- `-race` — флакает в Docker-окружении на тяжёлых OS-fixtures; пока оставим локальной целью `make race`. (Может быть добавлен позже, если nightly job окажется полезным.)

### Время выполнения (ожидаемое)
~2–3 минуты на холодный кэш, ~1 минута с кэшем модулей и билд-кэшем GHA.

---

## 4. Deploy workflow (`.github/workflows/deploy.yml`)

### Триггеры
- `push` на `main`
- `workflow_dispatch` (ручной запуск для повторного деплоя того же коммита)

### Permissions
```yaml
permissions:
  contents: read
  packages: write   # для push в ghcr.io
```

### Джоб `deploy`
1. `actions/checkout@v4`
2. `actions/setup-go@v5` — для повторного запуска тестов как гейта
3. lint + `go test ./...` + `go test -tags=integration ./...` (повтор того же гейта)
4. `docker/setup-buildx-action@v3`
5. `docker/login-action@v3` к `ghcr.io` через `${{ secrets.GITHUB_TOKEN }}` (PAT не требуется)
6. `docker/metadata-action@v5` — генерирует теги `:latest`, `:sha-<7chars>`
7. `docker/build-push-action@v6`:
   - `push: true`
   - `platforms: linux/amd64`
   - `cache-from`/`cache-to: type=gha`
   - tags: из metadata-action
8. `appleboy/ssh-action@v1`:
   - `host: ${{ secrets.VPS_HOST }}`
   - `username: ${{ secrets.VPS_USER }}`
   - `key: ${{ secrets.VPS_SSH_KEY }}`
   - `port: ${{ secrets.VPS_PORT }}`
   - `script:` блок:
     ```bash
     set -euo pipefail
     cd ~/second-brain
     docker compose pull
     docker compose up -d
     docker image prune -f
     ```

### Что происходит на VPS при `docker compose up -d`
1. Compose видит, что image digest для `ingest` сервиса изменился
2. Создаёт новый контейнер, останавливает старый, переключает volumes
3. Стандартный rolling-recreate, downtime ~1–3с
4. `prune -f` чистит висящие старые слои

### Видимость GHCR-пакета
По умолчанию первый push в `ghcr.io/<user>/<repo>` создаёт пакет как **private**, даже если репозиторий public. Это значит, что `docker pull` на VPS требовал бы аутентификации.

После первого успешного деплоя нужно один раз вручную:
1. Открыть https://github.com/users/qquiqlerr/packages/container/second-brain/settings
2. Раздел "Danger Zone" → "Change package visibility" → выбрать "Public"

После этого `docker pull` с VPS работает без `docker login`.

### Время выполнения (ожидаемое)
~3–4 минуты (тесты ~1 мин, build+push ~1–2 мин с кэшем, SSH ~30с).

---

## 5. Файл `docker-compose.prod.yml`

```yaml
services:
  ingest:
    image: ghcr.io/qquiqlerr/second-brain:latest
    pull_policy: always
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

### Отличия от dev `docker-compose.yml`
- Нет `build: .` — образ только pullится
- Явный `image:` с тегом `:latest`
- `pull_policy: always` — гарантирует, что `up -d` подтянет новый digest, даже если тег `:latest` не сменился (защита от случая, когда `latest` перезаписан той же ссылкой)
- В остальном идентично — те же volumes, env, TZ, restart-policy

### Почему отдельный файл, а не один `docker-compose.yml`
- На dev-машине `docker compose up` должен собирать локально (быстрая итерация), а не пуллить GHCR
- Имена `docker-compose.yml` vs `docker-compose.prod.yml` явно показывают, какой файл какого окружения
- При заливке на VPS скрипт копирует prod-файл под именем `docker-compose.yml`, чтобы команды `docker compose pull/up` на сервере не требовали `-f` флага

---

## 6. Скрипт `scripts/sync-prod.sh`

### Назначение
Локальный bash-скрипт для односторонней доставки конфигов с dev-машины на VPS. Не часть автодеплоя — запускается руками тогда, когда меняется что-то, чего нет в git: токены, личная таксономия, или нужно первичное развёртывание.

### Интерфейс
```
./scripts/sync-prod.sh [--env] [--taxonomy] [--compose] [--all] [--init]
```

| Флаг | Локальный источник | Назначение на VPS | После заливки |
|---|---|---|---|
| `--env` | `./.env` | `~/second-brain/.env` | `docker compose up -d` |
| `--taxonomy` | `./config/taxonomy.yml` | `~/second-brain/config/taxonomy.yml` | ничего (mtime-кэш `TaxonomyLoader`) |
| `--compose` | `./docker-compose.prod.yml` | `~/second-brain/docker-compose.yml` | `docker compose up -d` |
| `--all` | все три | соответственно | один `docker compose pull && up -d` |
| `--init` | все три | + `mkdir -p ~/second-brain/{config,data/notes}` + первый `up -d` | для первого развёртывания |
| `--force` | модификатор | пропускает подтверждение в `--init` если каталог уже существует | для повторной инициализации |

Без флагов — печатает usage и выходит с кодом 2.

Порядок шагов для `--init`:
1. `ssh ... 'mkdir -p ~/second-brain/{config,data/notes}'`
2. scp всех трёх файлов (атомарно, через `.tmp`)
3. `ssh ... 'cd ~/second-brain && docker compose config -q && docker compose pull && docker compose up -d'`

### Подключение к VPS
Скрипт читает env var `VPS_HOST_ALIAS` (по умолчанию `second-brain-vps`) и использует SSH-алиас. Пользователь один раз прописывает в `~/.ssh/config`:
```
Host second-brain-vps
  HostName <ip-or-domain>
  User <deploy-user>
  IdentityFile ~/.ssh/<personal-key>
  Port 22
```

Это держит IP, порт, путь к ключу вне репозитория и вне переменных окружения скрипта.

### Атомарность
Каждый файл копируется в `*.tmp` на сервере, валидируется (если применимо), потом `mv` — никаких полуобновлённых конфигов.

### Pre-flight проверки
- Существуют ли локальные файлы (для выбранных флагов) — если нет, выход с ошибкой
- Для `--compose`: `docker compose -f docker-compose.prod.yml config -q` локально (синтаксическая валидация перед заливкой)
- Для `--all`/`--compose`/`--env` после заливки и **до** `up -d`: `ssh ... 'cd ~/second-brain && docker compose config -q'` — если ломается, скрипт прерывается с ненулевым кодом; контейнер остаётся работать на старой конфигурации

### Поведение при ошибках
- `set -euo pipefail` + `trap` для понятных сообщений
- Если SSH не подключился — сразу exit с инструкцией проверить алиас
- Если `--init` запущен на уже инициализированном сервере (каталог `~/second-brain` существует) — скрипт спрашивает подтверждение (или принимает `--force`)

### Что скрипт НЕ делает
- Не качает файлы с VPS (диод в одну сторону)
- Не редактирует `.env` (для этого редактируешь локальный файл и снова `--env`)
- Не делает `down` (никогда — только `up -d`, который ребилдит контейнер аккуратно)
- Не работает с `data/notes` (это volume, инициализируется автоматически)

---

## 7. Makefile-обёртки

Добавить в `Makefile`:
```makefile
.PHONY: sync-env sync-taxonomy sync-compose sync-all

sync-env:
	./scripts/sync-prod.sh --env

sync-taxonomy:
	./scripts/sync-prod.sh --taxonomy

sync-compose:
	./scripts/sync-prod.sh --compose

sync-all:
	./scripts/sync-prod.sh --all
```

Это чистая эргономика — реальная логика в скрипте.

---

## 8. Секреты

### Где какой секрет живёт

| Секрет | Локально (dev) | VPS | GitHub Secrets |
|---|---|---|---|
| `OPENROUTER_API_KEY` | `.env` | `~/second-brain/.env` | — |
| `TELEGRAM_BOT_TOKEN` | `.env` | `~/second-brain/.env` | — |
| `ALLOWED_USER_IDS` | `.env` | `~/second-brain/.env` | — |
| `taxonomy.yml` | `config/taxonomy.yml` | `~/second-brain/config/taxonomy.yml` | — |
| VPS host/user/port | `~/.ssh/config` алиас | — | `VPS_HOST`, `VPS_USER`, `VPS_PORT` |
| Deploy SSH private key | — | публичная половина в `authorized_keys` | `VPS_SSH_KEY` (приватная) |

### Deploy SSH-ключ
Отдельная ключ-пара, **не** личный ключ пользователя. Генерируется один раз:
```bash
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519_secondbrain_deploy -N "" -C "github-actions-deploy"
```
- Публичная часть → `~/.ssh/authorized_keys` на VPS (под deploy-юзером)
- Приватная часть → значение секрета `VPS_SSH_KEY` (целиком, включая BEGIN/END строки)

В будущем при компрометации — удаляешь строку из `authorized_keys` и перегенерируешь.

---

## 9. Тегирование образов и откат

### Теги при каждом push в main
- `:latest` — alias на голову main (перезаписывается каждый деплой)
- `:sha-<7chars>` — иммутабельный тег конкретного коммита, остаётся в GHCR навсегда

### Сценарий отката
1. SSH на VPS: `ssh second-brain-vps`
2. `cd ~/second-brain`
3. Открыть `docker-compose.yml`, заменить `:latest` на `:sha-abc1234` (хеш предыдущего рабочего коммита — посмотреть `git log` локально или `docker images ghcr.io/qquiqlerr/second-brain --format '{{.Tag}}'` на VPS)
4. `docker compose up -d`
5. Когда основной баг починен и новый деплой прошёл — вернуть `:latest`

### Очистка старых образов
`docker image prune -f` после `up -d` чистит висящие слои (без тегов). Сами `:sha-*` теги в GHCR живут вечно — это плюс для отката. Если потребуется чистка, делается через `gh api -X DELETE ...` руками, не автоматизируется.

---

## 10. Пошаговый план развёртывания

Пять фаз. Между фазами можно прерваться.

### Фаза A — локальная подготовка (≈5 мин)
1. Подтвердить `gh auth status` (уже сделано — `qquiqlerr`, scopes `repo,workflow`)
2. `gh repo create qquiqlerr/second-brain --public --source=. --remote=origin --push`
3. Открыть https://github.com/qquiqlerr/second-brain и убедиться, что код виден

### Фаза B — VPS-подготовка (≈10–15 мин)
1. Локально: сгенерировать deploy-ключ
   ```bash
   ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519_secondbrain_deploy -N "" -C "github-actions-deploy"
   ```
2. Локально (по уже работающему SSH к VPS) добавить публичный ключ в `authorized_keys` deploy-пользователя
3. На VPS: проверить, что deploy-юзер в группе `docker` (`groups | grep docker`)
4. Локально: прописать в `~/.ssh/config` алиас `second-brain-vps` с личным ключом
5. На VPS вручную создать корень: `mkdir -p ~/second-brain/{config,data/notes}` (или сделать это автоматически на E.1 через `--init`)

### Фаза C — изменения в репозитории (один PR)
Создать feature-ветку `feat/cicd-bootstrap`, добавить:
- `docker-compose.prod.yml`
- `scripts/sync-prod.sh` + `chmod +x`
- `.github/workflows/ci.yml`
- `.github/workflows/deploy.yml`
- Обновлённый `Makefile`
- Обновлённый `README.md` (секция Deployment)

Открыть PR — он сам запустит свой же CI workflow (smoke-проверка, что workflow корректный). После зелёного — мердж в main.

### Фаза D — GitHub Secrets
Через `gh secret set` или UI:
```bash
gh secret set VPS_HOST    --body '<ip-or-domain>'
gh secret set VPS_USER    --body '<deploy-user>'
gh secret set VPS_PORT    --body '22'
gh secret set VPS_SSH_KEY < ~/.ssh/id_ed25519_secondbrain_deploy
```

### Фаза E — первый деплой и smoke-тест
1. Смерджить PR из фазы C → дождаться deploy workflow → первый push в GHCR создаст пакет `second-brain` (изначально private)
2. Сделать GHCR-пакет public (см. §4 «Видимость GHCR-пакета»)
3. Локально: `./scripts/sync-prod.sh --init` (заливает `.env`, `taxonomy.yml`, `docker-compose.yml`, делает первый `pull && up -d`)
4. Отправить тестовое сообщение боту в Telegram → проверить, что:
   - бот ответил
   - на VPS появились `data/notes/.../<id>.md` и `data/notes/summaries/<id>.md`
5. Контрольный тест: мелкое изменение в коде (например, текст приветственного reply) → feature-ветка → PR → CI зелёный → merge → дождаться deploy → отправить сообщение боту → убедиться, что новое поведение применилось (на VPS: `docker compose ps`, `docker compose logs --tail 50`)

---

## 11. Edge cases и трейд-оффы

### Race между двумя merge в main
Если два PR смержены подряд за 30 секунд, два deploy workflow запустятся параллельно и могут пушить образы в обратном порядке. На GitHub Actions у workflow есть `concurrency`-блок:
```yaml
concurrency:
  group: deploy-prod
  cancel-in-progress: false
```
Это гарантирует, что второй деплой подождёт первого, в правильном порядке.

### Деплой проходит, но контейнер не стартует
`appleboy/ssh-action` вернёт ненулевой код, если `docker compose up -d` упадёт (compose сам репортит ошибку). GitHub Action job будет failed; уведомление придёт через email (стандартная нотификация GitHub). Контейнер на VPS остаётся в последнем рабочем состоянии (если предыдущий был запущен) или в `Exited` (если первый деплой).

В будущем можно добавить health-check endpoint и шаг проверки после `up -d` — но это не MVP.

### `:latest` race / cache
`pull_policy: always` гарантирует, что `docker compose up -d` всегда сначала делает `docker pull`. Без этого compose мог бы решить, что image не изменился (по имени тега), и не пересоздать контейнер.

### Зашёл руками на VPS, поправил .env, потом sync-prod.sh --env затёр
Это by design — sync-prod.sh однонаправленный, с dev-машины. Хочешь "правильно" — правишь локально, потом sync. Если случилось — потеря небольшая, токены восстанавливаются из менеджера паролей.

### Что если GHCR упадёт
Очень редко. При недоступности GHCR `docker compose pull` упадёт; SSH-action вернёт ненулевой код; деплой прервётся. Контейнер на VPS продолжит работать на текущем образе. После восстановления GHCR — `workflow_dispatch` пересоздаёт деплой.

### Кэш GHA build-cache
`cache-from/to: type=gha` использует встроенный кэш GitHub Actions (10 GB на репо). Это даёт быстрые повторные сборки (~30с). Кэш автоматически evictится при достижении лимита.

---

## 12. Что НЕ делаем сейчас (но можно потом)

- **Health-check endpoint** `/healthz` в боте + Docker `HEALTHCHECK` директива — сделаем, когда заведём фоновые задачи (sub-project 4)
- **Уведомления в Telegram** о деплоях — лёгкая фича, добавим если станет нужно
- **Backup `data/notes`** — отдельная задача (cron на VPS → encrypted tarball в облако)
- **Bytecode signing / image signing** (cosign) — overkill для personal project
- **Multi-stage deploy (staging → prod)** — overkill при одном пользователе
- **Авто-rollback при упавшем health-check** — overkill в MVP

---

## 13. Конвенции коммитов и PR-ов

- PR с CI/CD изменениями — заголовок `ci: bootstrap GitHub Actions + sync script`
- Коммиты по теме — `ci:`, `chore(deploy):`, `docs(readme):` префиксы
- README обязательно содержит секцию Deployment с командой `./scripts/sync-prod.sh --init` для первичного развёртывания и кратким описанием git-flow

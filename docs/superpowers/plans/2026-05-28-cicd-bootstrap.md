# CI/CD Bootstrap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Поставить полный CI/CD-конвейер для `second-brain`: PR → CI; merge в main → сборка образа в GHCR → автодеплой на VPS по SSH. Плюс локальный `sync-prod.sh` для синка `.env`/`taxonomy.yml`/compose с правильной семантикой hot-reload.

**Architecture:** Pull-модель деплоя: GitHub Actions пушит образ в публичный GHCR (`ghcr.io/qquiqlerr/second-brain:latest` + `:sha-<7>`), затем по SSH дёргает на VPS `docker compose pull && up -d`. Конфиги (`.env`, `taxonomy.yml`) живут только на VPS и dev-машине, не в git и не в GitHub Secrets. Дизайн: `docs/superpowers/specs/2026-05-28-cicd-design.md`.

**Tech Stack:** GitHub Actions · GHCR · Docker Buildx · `appleboy/ssh-action` · `docker/build-push-action@v6` · bash · shellcheck · actionlint.

**Pre-requisites (нужны для выполнения):**
- gh CLI авторизован (`gh auth status` → ✓)
- доступ к VPS по SSH под существующим личным аккаунтом
- на VPS: Docker + compose plugin установлены
- локально установлены `shellcheck` и `actionlint`:
  ```bash
  brew install shellcheck actionlint
  ```

---

## Phase A — GitHub repository setup

### Task 1: Создать GitHub-репозиторий и запушить main + feat-ветку

**Files:** нет — только git remote + push

- [ ] **Step 1: Убедиться, что мы на feat-ветке со спекой**

```bash
git status
```
Expected: `On branch feat/cicd-bootstrap`, working tree clean.

- [ ] **Step 2: Переключиться на main для создания репо с правильным default-бранчем**

```bash
git checkout main
```
Expected: `Switched to branch 'main'`.

- [ ] **Step 3: Создать публичный репозиторий и запушить main**

```bash
gh repo create qquiqlerr/second-brain \
  --public \
  --description "Personal Knowledge & Telemetry System — Telegram-driven note ingestion" \
  --source=. \
  --remote=origin \
  --push
```
Expected:
```
✓ Created repository qquiqlerr/second-brain on GitHub
✓ Added remote https://github.com/qquiqlerr/second-brain.git
✓ Pushed commits to https://github.com/qquiqlerr/second-brain.git
```

- [ ] **Step 4: Вернуться на feat-ветку и запушить её**

```bash
git checkout feat/cicd-bootstrap
git push -u origin feat/cicd-bootstrap
```
Expected: ветка создана на remote, tracking установлен.

- [ ] **Step 5: Проверить, что репо виден и main — default branch**

```bash
gh repo view qquiqlerr/second-brain --json defaultBranchRef --jq .defaultBranchRef.name
```
Expected: `main`.

- [ ] **Step 6: (Без коммита — мы только манипулировали remote)**

---

## Phase B — VPS preparation

### Task 2: Сгенерировать deploy SSH-ключ для GitHub Actions

**Files:** ключ-пара локально в `~/.ssh/`

- [ ] **Step 1: Сгенерировать ed25519 пару**

```bash
ssh-keygen -t ed25519 \
  -f ~/.ssh/id_ed25519_secondbrain_deploy \
  -N "" \
  -C "github-actions-deploy"
```
Expected: `Your identification has been saved in ...`, два файла появились: приватный и `.pub`.

- [ ] **Step 2: Распечатать публичную часть для следующего шага**

```bash
cat ~/.ssh/id_ed25519_secondbrain_deploy.pub
```
Сохрани вывод — он понадобится в Task 3.

- [ ] **Step 3: Распечатать приватную часть для GitHub Secret (понадобится в Task 12)**

```bash
cat ~/.ssh/id_ed25519_secondbrain_deploy
```
Сохрани целиком, включая `-----BEGIN OPENSSH PRIVATE KEY-----` и `-----END OPENSSH PRIVATE KEY-----`.

### Task 3: Добавить deploy-ключ на VPS и проверить deploy-пользователя

**Files:** `authorized_keys` на VPS

- [ ] **Step 1: Скопировать публичный ключ на VPS (под существующим личным аккаунтом)**

Замени `<deploy-user>` и `<vps-host>` на свои значения. Если deploy-пользователь = это же твой текущий пользователь, оставь его. Если deploy-пользователь — отдельный (например, `deploy`), его сначала надо создать (см. Step 3).

```bash
ssh-copy-id -i ~/.ssh/id_ed25519_secondbrain_deploy.pub <deploy-user>@<vps-host>
```
Expected: `Number of key(s) added: 1`.

- [ ] **Step 2: Проверить SSH-подключение по новому ключу**

```bash
ssh -i ~/.ssh/id_ed25519_secondbrain_deploy <deploy-user>@<vps-host> 'echo ok'
```
Expected: `ok`.

- [ ] **Step 3: Проверить, что deploy-пользователь в группе `docker` (на VPS)**

```bash
ssh -i ~/.ssh/id_ed25519_secondbrain_deploy <deploy-user>@<vps-host> 'groups; docker ps'
```
Expected: в списке групп есть `docker`, команда `docker ps` отрабатывает без `sudo` и без ошибок permission denied.

Если `docker` нет в группах — на VPS под root: `usermod -aG docker <deploy-user>` и переоткрыть SSH-сессию.

### Task 4: Прописать локальный SSH-алиас `second-brain-vps`

**Files:** `~/.ssh/config`

- [ ] **Step 1: Открыть `~/.ssh/config` и добавить блок**

Замени `<vps-host>` и `<deploy-user>` на свои значения. `IdentityFile` — это твой **личный** ключ для повседневной работы (НЕ deploy-key, deploy-key для GitHub Actions).

Добавить в конец `~/.ssh/config`:
```
Host second-brain-vps
  HostName <vps-host>
  User <deploy-user>
  IdentityFile ~/.ssh/<personal-key>
  Port 22
```

- [ ] **Step 2: Проверить алиас**

```bash
ssh second-brain-vps 'echo ok && uname -m'
```
Expected: `ok` и `x86_64`.

---

## Phase C — Repository changes

Все задачи в этой фазе делаются на ветке `feat/cicd-bootstrap`. Убедись, что ты на ней:
```bash
git checkout feat/cicd-bootstrap
```

### Task 5: Создать `docker-compose.prod.yml`

**Files:**
- Create: `docker-compose.prod.yml`

- [ ] **Step 1: Создать файл с содержимым**

`docker-compose.prod.yml`:
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

- [ ] **Step 2: Валидировать compose-файл**

```bash
docker compose -f docker-compose.prod.yml config -q
```
Expected: no output, exit code 0. Ошибка означает синтаксическую проблему — исправить.

- [ ] **Step 3: Закоммитить**

```bash
git add docker-compose.prod.yml
git commit -m "ci: add docker-compose.prod.yml referencing GHCR image"
```

### Task 6: Создать `scripts/sync-prod.sh`

**Files:**
- Create: `scripts/sync-prod.sh`

- [ ] **Step 1: Создать директорию и файл**

```bash
mkdir -p scripts
```

`scripts/sync-prod.sh`:
```bash
#!/usr/bin/env bash
# Sync local config files (.env, taxonomy, compose) to production VPS over SSH.
# Design: docs/superpowers/specs/2026-05-28-cicd-design.md §6.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VPS="${VPS_HOST_ALIAS:-second-brain-vps}"
VPS_DIR="${VPS_DIR:-second-brain}"

LOCAL_ENV="${REPO_ROOT}/.env"
LOCAL_TAX="${REPO_ROOT}/config/taxonomy.yml"
LOCAL_COMPOSE="${REPO_ROOT}/docker-compose.prod.yml"

sync_env=0
sync_taxonomy=0
sync_compose=0
do_init=0
force=0

usage() {
  cat <<EOF
Usage: $0 [--env] [--taxonomy] [--compose] [--all] [--init] [--force]

  --env        upload .env to ~/${VPS_DIR}/.env and recreate container
  --taxonomy   upload config/taxonomy.yml; no restart (TaxonomyLoader picks up via mtime)
  --compose    upload docker-compose.prod.yml to ~/${VPS_DIR}/docker-compose.yml
  --all        all three plus a single docker compose pull && up -d
  --init       first-time bootstrap: mkdir + scp all three + up -d
  --force      with --init, skip confirmation if ~/${VPS_DIR} already exists

Environment:
  VPS_HOST_ALIAS   SSH alias (default: second-brain-vps)
  VPS_DIR          remote dir under \$HOME (default: second-brain)
EOF
  exit 2
}

[[ $# -eq 0 ]] && usage

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env) sync_env=1 ;;
    --taxonomy) sync_taxonomy=1 ;;
    --compose) sync_compose=1 ;;
    --all) sync_env=1; sync_taxonomy=1; sync_compose=1 ;;
    --init) do_init=1; sync_env=1; sync_taxonomy=1; sync_compose=1 ;;
    --force) force=1 ;;
    -h|--help) usage ;;
    *) echo "Unknown flag: $1" >&2; usage ;;
  esac
  shift
done

if [[ $sync_env -eq 1 && ! -f "$LOCAL_ENV" ]]; then
  echo "ERROR: $LOCAL_ENV not found" >&2
  exit 1
fi
if [[ $sync_taxonomy -eq 1 && ! -f "$LOCAL_TAX" ]]; then
  echo "ERROR: $LOCAL_TAX not found" >&2
  exit 1
fi
if [[ ($sync_compose -eq 1 || $do_init -eq 1) && ! -f "$LOCAL_COMPOSE" ]]; then
  echo "ERROR: $LOCAL_COMPOSE not found" >&2
  exit 1
fi

if ! ssh -o ConnectTimeout=5 -o BatchMode=yes "$VPS" 'true' 2>/dev/null; then
  echo "ERROR: cannot SSH to alias '$VPS'. Check ~/.ssh/config" >&2
  exit 1
fi

if [[ $sync_compose -eq 1 || $do_init -eq 1 ]]; then
  if ! docker compose -f "$LOCAL_COMPOSE" config -q; then
    echo "ERROR: $LOCAL_COMPOSE failed local validation" >&2
    exit 1
  fi
fi

if [[ $do_init -eq 1 ]]; then
  exists=$(ssh "$VPS" "if [ -d \"\$HOME/${VPS_DIR}\" ]; then echo yes; else echo no; fi")
  if [[ "$exists" == "yes" && $force -eq 0 ]]; then
    read -r -p "~/${VPS_DIR} already exists on $VPS. Continue and overwrite configs? [y/N] " ans
    [[ "$ans" =~ ^[Yy]$ ]] || { echo "Aborted."; exit 1; }
  fi
  ssh "$VPS" "mkdir -p \"\$HOME/${VPS_DIR}/config\" \"\$HOME/${VPS_DIR}/data/notes\""
fi

upload_atomic() {
  local src="$1"
  local dst="$2"
  scp -q "$src" "${VPS}:${dst}.tmp"
  ssh "$VPS" "mv \"${dst}.tmp\" \"${dst}\""
}

uploaded_any=0
restart_needed=0

if [[ $sync_compose -eq 1 ]]; then
  upload_atomic "$LOCAL_COMPOSE" "\$HOME/${VPS_DIR}/docker-compose.yml"
  restart_needed=1
  uploaded_any=1
  echo "✓ compose synced"
fi

if [[ $sync_env -eq 1 ]]; then
  upload_atomic "$LOCAL_ENV" "\$HOME/${VPS_DIR}/.env"
  restart_needed=1
  uploaded_any=1
  echo "✓ .env synced"
fi

if [[ $sync_taxonomy -eq 1 ]]; then
  upload_atomic "$LOCAL_TAX" "\$HOME/${VPS_DIR}/config/taxonomy.yml"
  uploaded_any=1
  echo "✓ taxonomy synced (no restart — TaxonomyLoader uses mtime cache)"
fi

if [[ $uploaded_any -eq 0 ]]; then
  echo "Nothing to do."
  exit 0
fi

if [[ $restart_needed -eq 1 || $do_init -eq 1 ]]; then
  if ! ssh "$VPS" "cd \"\$HOME/${VPS_DIR}\" && docker compose config -q"; then
    echo "ERROR: compose on remote is invalid — aborting restart" >&2
    exit 1
  fi
  echo "→ pulling and recreating container..."
  ssh "$VPS" "cd \"\$HOME/${VPS_DIR}\" && docker compose pull && docker compose up -d"
  echo "✓ container restarted"
fi
```

- [ ] **Step 2: Сделать скрипт исполняемым**

```bash
chmod +x scripts/sync-prod.sh
```

- [ ] **Step 3: Прогнать shellcheck**

```bash
shellcheck scripts/sync-prod.sh
```
Expected: no output, exit code 0. Если есть warnings — исправить.

- [ ] **Step 4: Smoke-тест usage**

```bash
./scripts/sync-prod.sh
```
Expected: вывод usage-блока, exit code 2.

- [ ] **Step 5: Smoke-тест валидации flag-парсера**

```bash
./scripts/sync-prod.sh --invalid-flag 2>&1 || true
```
Expected: `Unknown flag: --invalid-flag`, затем usage.

- [ ] **Step 6: Закоммитить**

```bash
git add scripts/sync-prod.sh
git commit -m "ci(sync): add scripts/sync-prod.sh for VPS config sync"
```

### Task 7: Обновить Makefile с целями `sync-*`

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Добавить новые `.PHONY` цели в конец файла**

В конец `Makefile` (после цели `run:`):
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

- [ ] **Step 2: Проверить, что цели видны в `make`**

```bash
make -n sync-env
```
Expected: `./scripts/sync-prod.sh --env` (только печать, без выполнения).

- [ ] **Step 3: Закоммитить**

```bash
git add Makefile
git commit -m "ci(makefile): add sync-{env,taxonomy,compose,all} targets"
```

### Task 8: Создать `.github/workflows/ci.yml`

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Создать директорию**

```bash
mkdir -p .github/workflows
```

- [ ] **Step 2: Создать файл с содержимым**

`.github/workflows/ci.yml`:
```yaml
name: CI

on:
  pull_request:
  push:
    branches-ignore:
      - main

permissions:
  contents: read

concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true

jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true

      - name: Lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: latest

      - name: Unit tests
        run: go test -count=1 ./...

      - name: Integration tests
        run: go test -count=1 -tags=integration ./...

      - name: Docker build (no push)
        uses: docker/build-push-action@v6
        with:
          context: .
          push: false
          platforms: linux/amd64
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

- [ ] **Step 3: Прогнать actionlint**

```bash
actionlint .github/workflows/ci.yml
```
Expected: no output, exit code 0.

- [ ] **Step 4: Закоммитить**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add CI workflow (lint + unit + integration + docker build)"
```

### Task 9: Создать `.github/workflows/deploy.yml`

**Files:**
- Create: `.github/workflows/deploy.yml`

- [ ] **Step 1: Создать файл с содержимым**

`.github/workflows/deploy.yml`:
```yaml
name: Deploy

on:
  push:
    branches: [main]
  workflow_dispatch:

permissions:
  contents: read
  packages: write

concurrency:
  group: deploy-prod
  cancel-in-progress: false

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true

      - name: Lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: latest

      - name: Unit tests
        run: go test -count=1 ./...

      - name: Integration tests
        run: go test -count=1 -tags=integration ./...

      - uses: docker/setup-buildx-action@v3

      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - id: meta
        uses: docker/metadata-action@v5
        with:
          images: ghcr.io/${{ github.repository_owner }}/second-brain
          tags: |
            type=raw,value=latest
            type=sha,prefix=sha-,format=short

      - uses: docker/build-push-action@v6
        with:
          context: .
          push: true
          platforms: linux/amd64
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max

      - name: Deploy via SSH
        uses: appleboy/ssh-action@v1
        with:
          host: ${{ secrets.VPS_HOST }}
          username: ${{ secrets.VPS_USER }}
          key: ${{ secrets.VPS_SSH_KEY }}
          port: ${{ secrets.VPS_PORT }}
          script: |
            set -euo pipefail
            cd ~/second-brain
            docker compose pull
            docker compose up -d
            docker image prune -f
```

- [ ] **Step 2: Прогнать actionlint**

```bash
actionlint .github/workflows/deploy.yml
```
Expected: no output, exit code 0.

- [ ] **Step 3: Закоммитить**

```bash
git add .github/workflows/deploy.yml
git commit -m "ci: add deploy workflow (build → push GHCR → SSH-pull on VPS)"
```

### Task 10: Обновить README.md секцией Deployment

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Вставить новую секцию между «Test» и «Структура репозитория»**

Найти в `README.md` строку 64 (`## Структура репозитория`) и **перед ней** вставить:

```markdown
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

```

- [ ] **Step 2: Проверить, что markdown корректный**

```bash
cat README.md | head -120
```
Expected: новая секция вставлена, форматирование сохранено.

- [ ] **Step 3: Закоммитить**

```bash
git add README.md
git commit -m "docs(readme): add Deployment section"
```

### Task 11: Запушить ветку и открыть PR

**Files:** только git

- [ ] **Step 1: Запушить накопленные коммиты**

```bash
git push
```
Expected: 6 коммитов улетели на remote (compose, sync-prod.sh, Makefile, ci.yml, deploy.yml, readme — плюс изначальный спека-коммит).

- [ ] **Step 2: Открыть PR в main**

```bash
gh pr create --base main --head feat/cicd-bootstrap \
  --title "ci: bootstrap GitHub Actions + sync-prod script" \
  --body "$(cat <<'EOF'
## Summary
- Дизайн в `docs/superpowers/specs/2026-05-28-cicd-design.md`
- CI на PR (lint + unit + integration + docker build)
- Deploy на main (build → push GHCR → SSH-pull на VPS)
- `docker-compose.prod.yml` со ссылкой на GHCR-образ
- `scripts/sync-prod.sh` + Makefile-обёртки для синка конфигов

## Test plan
- [ ] CI workflow прошёл на этом же PR (smoke на свой workflow)
- [ ] После merge: deploy workflow собрал и запушил образ в GHCR
- [ ] После `sync-prod.sh --init`: контейнер поднялся, бот отвечает
EOF
)"
```
Expected: PR создан, URL выведен.

- [ ] **Step 3: Дождаться зелёного CI на этом PR**

```bash
gh pr checks --watch
```
Expected: `verify  pass`. Если красное — посмотреть `gh run view --log-failed` и починить.

**Не мерджить сейчас** — мердж в Task 13 после фазы D (нужны GitHub Secrets для deploy).

---

## Phase D — GitHub Secrets

### Task 12: Поставить секреты для deploy workflow

**Files:** GitHub repository secrets

Подставь свои значения вместо `<...>`.

- [ ] **Step 1: VPS_HOST (IP или домен)**

```bash
gh secret set VPS_HOST --body '<vps-ip-or-domain>'
```

- [ ] **Step 2: VPS_USER (deploy-пользователь)**

```bash
gh secret set VPS_USER --body '<deploy-user>'
```

- [ ] **Step 3: VPS_PORT (обычно 22)**

```bash
gh secret set VPS_PORT --body '22'
```

- [ ] **Step 4: VPS_SSH_KEY (приватный deploy-ключ из Task 2)**

```bash
gh secret set VPS_SSH_KEY < ~/.ssh/id_ed25519_secondbrain_deploy
```

- [ ] **Step 5: Проверить, что 4 секрета установлены**

```bash
gh secret list
```
Expected: `VPS_HOST`, `VPS_USER`, `VPS_PORT`, `VPS_SSH_KEY` в выводе.

---

## Phase E — First deploy and smoke-test

### Task 13: Смерджить PR и дождаться первого деплоя

**Files:** GitHub

- [ ] **Step 1: Замерджить PR**

```bash
gh pr merge feat/cicd-bootstrap --merge --delete-branch
```
(Или через UI: «Create a merge commit».)
Expected: PR merged, ветка удалена на remote.

- [ ] **Step 2: Обновить локальный main**

```bash
git checkout main
git pull --ff-only
```

- [ ] **Step 3: Дождаться deploy workflow**

```bash
gh run watch
```
Expected: workflow `Deploy` зелёный. На последнем шаге `Deploy via SSH` ожидаем ошибку — на VPS пока **нет** каталога `~/second-brain` и compose-файла. Это ОК — образ всё равно запушится в GHCR.

Если deploy упал именно на SSH-шаге — посмотри логи:
```bash
gh run view --log-failed
```
Подтвердить, что ошибка вида `no such file or directory: ~/second-brain` или подобная, а не сбой на push в registry.

### Task 14: Сделать GHCR-пакет публичным

**Files:** GitHub package settings

- [ ] **Step 1: Открыть страницу пакета**

В браузере: https://github.com/users/qquiqlerr/packages/container/second-brain/settings

- [ ] **Step 2: Изменить visibility на public**

Раздел «Danger Zone» → «Change package visibility» → выбрать **Public** → подтвердить вводом названия пакета.

- [ ] **Step 3: Проверить через CLI**

```bash
gh api /users/qquiqlerr/packages/container/second-brain --jq .visibility
```
Expected: `public`.

- [ ] **Step 4: Проверить анонимный pull**

```bash
docker pull ghcr.io/qquiqlerr/second-brain:latest
```
Expected: образ скачался без `docker login`.

### Task 15: Первичное развёртывание через `--init`

**Files:** на VPS — `~/second-brain/...`

- [ ] **Step 1: Убедиться, что локально есть все три файла**

```bash
ls -la .env config/taxonomy.yml docker-compose.prod.yml
```
Expected: все три существуют. Если `config/taxonomy.yml` нет — `cp testdata/taxonomy.yml.example config/taxonomy.yml` и заполнить.

- [ ] **Step 2: Запустить init**

```bash
./scripts/sync-prod.sh --init
```
Expected:
```
✓ compose synced
✓ .env synced
✓ taxonomy synced (no restart — TaxonomyLoader uses mtime cache)
→ pulling and recreating container...
✓ container restarted
```

- [ ] **Step 3: Проверить статус контейнера на VPS**

```bash
ssh second-brain-vps 'cd ~/second-brain && docker compose ps'
```
Expected: `ingest` в статусе `Up`, image — `ghcr.io/qquiqlerr/second-brain:latest`.

- [ ] **Step 4: Проверить логи (последние 30 строк)**

```bash
ssh second-brain-vps 'cd ~/second-brain && docker compose logs --tail 30'
```
Expected: бот стартанул, Telegram poller активен, ошибок нет.

### Task 16: Telegram smoke-test

**Files:** нет — ручной тест

- [ ] **Step 1: Отправить тестовое сообщение боту**

В Telegram отправь короткий дамп — текст или voice (например: «купил молоко, сделал стэндап, идея переписать middleware»).

- [ ] **Step 2: Дождаться ответа бота**

Expected: бот отвечает summary в формате `✅ Сохранено N заметок: ...` с подсветкой summary.

- [ ] **Step 3: Проверить, что файлы появились на VPS**

```bash
ssh second-brain-vps 'ls -la ~/second-brain/data/notes/'
ssh second-brain-vps 'ls -la ~/second-brain/data/notes/summaries/'
```
Expected: новые `.md` файлы за сегодняшнюю дату.

### Task 17: End-to-end auto-deploy test

**Files:**
- Modify: что-то незначительное (например, `internal/adapter/in/telegram/reply.go` — изменить текст emoji или префикс)

- [ ] **Step 1: Создать тестовую ветку**

```bash
git checkout -b chore/test-auto-deploy
```

- [ ] **Step 2: Сделать минимальное видимое изменение**

Например, в `internal/adapter/in/telegram/reply.go` поменять префикс с `✅ Сохранено` на `🟢 Сохранено` (или другой emoji).

- [ ] **Step 3: Локально прогнать тесты**

```bash
make test
make test-int
```
Expected: всё зелёное. Если интеграционный голден сломался — нормально, это часть проверки. Запустить с `-update`:
```bash
go test -count=1 -tags=integration ./internal/integration/ -update
```
И ещё раз `make test-int`.

- [ ] **Step 4: Закоммитить и запушить**

```bash
git add -A
git commit -m "chore(telegram): switch reply prefix to test auto-deploy"
git push -u origin chore/test-auto-deploy
```

- [ ] **Step 5: Открыть PR и дождаться CI**

```bash
gh pr create --fill
gh pr checks --watch
```
Expected: CI зелёный.

- [ ] **Step 6: Смерджить и дождаться deploy**

```bash
gh pr merge --merge --delete-branch
gh run watch
```
Expected: deploy workflow прошёл зелёным, включая SSH-шаг (теперь на VPS есть `~/second-brain/`).

- [ ] **Step 7: Проверить новую версию в Telegram**

Отправить ещё один дамп → проверить, что ответ начинается с нового emoji.

- [ ] **Step 8: Локально обновить main**

```bash
git checkout main
git pull --ff-only
```

---

## Self-review checklist

Эти проверки — для агентного executor'а перед закрытием плана.

- [ ] Все 6 файлов из фазы C запушены в main: `docker-compose.prod.yml`, `scripts/sync-prod.sh`, `.github/workflows/{ci,deploy}.yml`, обновлённые `Makefile` и `README.md`
- [ ] GitHub Actions показывает 2 успешных run'а: один CI (на PR) и минимум один Deploy (на merge)
- [ ] GHCR-пакет `second-brain` имеет visibility `public`
- [ ] На VPS `docker compose ps` показывает `ingest` в `Up`, образ из `ghcr.io/...`
- [ ] Тестовый дамп в Telegram записался в `~/second-brain/data/notes/` + `summaries/`
- [ ] End-to-end из Task 17 прошёл: мердж feature → автодеплой → видимое изменение в Telegram
- [ ] `~/.ssh/config` имеет алиас `second-brain-vps`; deploy-ключ `id_ed25519_secondbrain_deploy` отдельно от личного
- [ ] `gh secret list` показывает 4 секрета: `VPS_HOST`, `VPS_USER`, `VPS_PORT`, `VPS_SSH_KEY`

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
  ssh "$VPS" "mkdir -p \"\$HOME/${VPS_DIR}/config\" \"\$HOME/${VPS_DIR}/data/notes\" \"\$HOME/${VPS_DIR}/data/index\""
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
  upload_atomic "$LOCAL_COMPOSE" "${VPS_DIR}/docker-compose.yml"
  restart_needed=1
  uploaded_any=1
  echo "✓ compose synced"
fi

if [[ $sync_env -eq 1 ]]; then
  upload_atomic "$LOCAL_ENV" "${VPS_DIR}/.env"
  restart_needed=1
  uploaded_any=1
  echo "✓ .env synced"
fi

if [[ $sync_taxonomy -eq 1 ]]; then
  upload_atomic "$LOCAL_TAX" "${VPS_DIR}/config/taxonomy.yml"
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

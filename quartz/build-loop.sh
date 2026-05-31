#!/bin/sh
set -eu

INTERVAL="${QUARTZ_BUILD_INTERVAL_SEC:-300}"
CONTENT="${QUARTZ_CONTENT_DIR:-/content}"
# Quartz rmdir's the output dir before writing — point it at a subdirectory
# of the mounted volume so it can wipe its own working area without
# touching the mount point.
OUTPUT="${QUARTZ_OUTPUT_DIR:-/output/site}"

mkdir -p "$(dirname "$OUTPUT")"

while true; do
  echo "[$(date -u +%FT%TZ)] building quartz from $CONTENT → $OUTPUT"
  if npx quartz build --directory "$CONTENT" --output "$OUTPUT" 2>&1; then
    echo "[$(date -u +%FT%TZ)] build ok"
  else
    echo "[$(date -u +%FT%TZ)] build FAILED" >&2
  fi
  sleep "$INTERVAL"
done

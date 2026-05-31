#!/usr/bin/env bash
# Hourly backup of the notes corpus to a private GitHub repo.
# Designed to run from cron on the production VPS.
#
# Repo layout: notes are committed at the root of qquiqlerr/second-brain-data
# (the working tree IS ~/second-brain/data/notes). Linked_notes section is
# already in body — backup captures the same shape Quartz/Obsidian see.
#
# Exits cleanly with no commit when there's nothing to back up.
# Auth via SSH alias `github-secondbrain-backup` defined in ~/.ssh/config.

set -euo pipefail

NOTES_DIR="${SECOND_BRAIN_NOTES_DIR:-$HOME/second-brain/data/notes}"

cd "$NOTES_DIR"

# Stage everything (additions, mods, deletions).
git add -A

# Nothing to commit → exit cleanly so cron stays quiet.
if git diff --staged --quiet; then
  exit 0
fi

# Build a useful commit subject. Counts come from --staged.
n_changed=$(git diff --staged --name-only | wc -l | tr -d ' ')
ts=$(date -u +%FT%TZ)
git commit --quiet -m "backup ${ts}: ${n_changed} files changed"

# Push; fail loudly if it does — cron should write to /var/log/backup.log
# and we'll see the trail.
git push --quiet origin main

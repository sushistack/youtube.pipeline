#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-dev}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$SCRIPT_DIR"
WEB_ROOT="$REPO_ROOT/web"

# Defaults align with the project-root layout (./config.yaml, ./pipeline.db,
# ./output). For an isolated dev sandbox, override per-run, e.g.:
#   DB_PATH=./.tmp/dev.db OUTPUT_DIR=./.tmp/output ./startup.sh dev
APP_PORT="${APP_PORT:-8080}"
VITE_PORT="${VITE_PORT:-5173}"
DATA_DIR="${DATA_DIR:-$REPO_ROOT/testdata}"
DB_PATH="${DB_PATH:-$REPO_ROOT/pipeline.db}"
OUTPUT_DIR="${OUTPUT_DIR:-$REPO_ROOT/output}"

PIDS=()

log() {
  printf '[startup] %s\n' "$*"
}

fail() {
  printf '[startup] %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "Missing required command: $1"
}

cleanup() {
  local exit_code=$?

  for pid in "${PIDS[@]:-}"; do
    if kill -0 "$pid" >/dev/null 2>&1; then
      kill "$pid" >/dev/null 2>&1 || true
      wait "$pid" 2>/dev/null || true
    fi
  done

  exit "$exit_code"
}

usage() {
  cat <<EOF
Usage:
  ./startup.sh [dev|prod]

Modes:
  dev   Start Vite on http://127.0.0.1:${VITE_PORT} and Go API/SPA proxy on http://127.0.0.1:${APP_PORT}
  prod  Build the SPA and serve the embedded app on http://127.0.0.1:${APP_PORT}

Optional environment variables (all default to the project-root layout):
  APP_PORT   Go server port (default: ${APP_PORT})
  VITE_PORT  Vite dev server port (default: ${VITE_PORT})
  DATA_DIR   Data directory passed to the Go server (default: ${DATA_DIR})
  DB_PATH    SQLite database path (default: ${DB_PATH})
  OUTPUT_DIR Output directory (default: ${OUTPUT_DIR})

For a dev sandbox isolated from your live ./pipeline.db / ./output:
  DB_PATH=./.tmp/dev.db OUTPUT_DIR=./.tmp/dev-output ./startup.sh dev
EOF
}

start_dev() {
  require_cmd go
  require_cmd npm

  mkdir -p "$OUTPUT_DIR"

  log "Starting Vite dev server on http://127.0.0.1:${VITE_PORT}"
  (
    cd "$WEB_ROOT"
    npm run dev -- --host 127.0.0.1 --port "$VITE_PORT"
  ) &
  PIDS+=("$!")

  log "Starting Go server on http://127.0.0.1:${APP_PORT} (proxying frontend to Vite)"
  (
    cd "$REPO_ROOT"
    if command -v humanlog >/dev/null 2>&1; then
      DATA_DIR="$DATA_DIR" DB_PATH="$DB_PATH" OUTPUT_DIR="$OUTPUT_DIR" \
        go run ./cmd/pipeline serve --dev --port "$APP_PORT" 2>&1 | humanlog
    else
      DATA_DIR="$DATA_DIR" DB_PATH="$DB_PATH" OUTPUT_DIR="$OUTPUT_DIR" \
        go run ./cmd/pipeline serve --dev --port "$APP_PORT"
    fi
  ) &
  PIDS+=("$!")

  log "App URL:  http://127.0.0.1:${APP_PORT}"
  log "Vite URL: http://127.0.0.1:${VITE_PORT}"
  log "Press Ctrl+C to stop both processes."

  wait
}

start_prod() {
  require_cmd go
  require_cmd npm

  mkdir -p "$OUTPUT_DIR"

  log "Building web app"
  (
    cd "$WEB_ROOT"
    npm run build
  )

  log "Starting embedded Go server on http://127.0.0.1:${APP_PORT}"
  cd "$REPO_ROOT"
  if command -v humanlog >/dev/null 2>&1; then
    DATA_DIR="$DATA_DIR" DB_PATH="$DB_PATH" OUTPUT_DIR="$OUTPUT_DIR" \
      go run ./cmd/pipeline serve --port "$APP_PORT" 2>&1 | humanlog
  else
    DATA_DIR="$DATA_DIR" DB_PATH="$DB_PATH" OUTPUT_DIR="$OUTPUT_DIR" \
      go run ./cmd/pipeline serve --port "$APP_PORT"
  fi
}

trap cleanup INT TERM EXIT

case "$MODE" in
  dev)
    start_dev
    ;;
  prod)
    start_prod
    ;;
  -h|--help|help)
    usage
    ;;
  *)
    usage
    fail "Unknown mode: $MODE"
    ;;
esac

#!/usr/bin/env bash
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

RUN_API=1
RUN_WORKER=1
RUN_WEB=1
WORKER_REPLICAS=1
WATCH=0
MIGRATE=0
KILL_PORT=0
LOG_DIR="$ROOT/.dev/logs"

usage() {
  cat <<'USAGE'
Usage: ./scripts/dev.sh [options]

Runs the API server, the worker(s) and the Next.js dashboard together in one
terminal with colour-tagged, interleaved logs. Ctrl-C stops everything.

Options:
  --api-only            run only the API server
  --worker-only         run only the worker
  --web-only            run only the dashboard
  --no-web              skip the dashboard
  --no-worker           skip the worker
  --workers N           run N worker processes (default 1)
  --watch               rebuild + restart api/worker when .go files change
  --migrate             run database migrations before starting
  --kill-port           free ports 8080 / 3000 if something else holds them
  --log-dir DIR         where to tee per-service logs (default .dev/logs)
  -h, --help            this help

Environment: read from .env in the repo root and passed to every service.
Override per run, e.g.  PORT=8081 WEB_PORT=3001 ./scripts/dev.sh
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --api-only)    RUN_WORKER=0; RUN_WEB=0 ;;
    --worker-only) RUN_API=0; RUN_WEB=0 ;;
    --web-only)    RUN_API=0; RUN_WORKER=0 ;;
    --no-web)      RUN_WEB=0 ;;
    --no-worker)   RUN_WORKER=0 ;;
    --workers)     WORKER_REPLICAS="${2:?--workers needs a number}"; shift ;;
    --watch)       WATCH=1 ;;
    --migrate)     MIGRATE=1 ;;
    --kill-port)   KILL_PORT=1 ;;
    --log-dir)     LOG_DIR="${2:?--log-dir needs a path}"; shift ;;
    -h|--help)     usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

if [[ -t 1 ]]; then
  BOLD=$'\033[1m'; DIM=$'\033[2m'; RESET=$'\033[0m'
  RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'
  BLUE=$'\033[34m'; MAGENTA=$'\033[35m'; CYAN=$'\033[36m'
else
  BOLD=''; DIM=''; RESET=''; RED=''; GREEN=''; YELLOW=''; BLUE=''; MAGENTA=''; CYAN=''
fi

say()  { printf '%s%s%s\n' "$CYAN$BOLD" "$*" "$RESET"; }
warn() { printf '%s%s%s\n' "$YELLOW" "$*" "$RESET" >&2; }
die()  { printf '%s%s%s\n' "$RED$BOLD" "$*" "$RESET" >&2; exit 1; }

if [[ -f "$ROOT/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$ROOT/.env"
  set +a
else
  warn "no .env in $ROOT — copy .env.example to .env and fill it in"
fi

: "${PORT:=8080}"
: "${WEB_PORT:=3000}"
: "${ENV:=development}"
export PORT ENV
export NEXT_PUBLIC_API_URL="${NEXT_PUBLIC_API_URL:-http://localhost:$PORT}"

[[ -n "${DATABASE_URL:-}" ]] || die "DATABASE_URL is not set (check .env)"
if (( RUN_WORKER )) && [[ -z "${PROJECT_ID:-}" ]]; then
  die "PROJECT_ID is not set (the worker needs it; check .env)"
fi
if (( RUN_API )) && [[ -z "${JWT_SECRET:-}" ]]; then
  die "JWT_SECRET is not set (check .env)"
fi

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required but not installed"; }
(( RUN_API || RUN_WORKER )) && need go
(( RUN_WEB )) && { need node; need npm; }

port_in_use() {
  local p="$1"
  if command -v ss >/dev/null 2>&1; then
    [[ -n "$(ss -ltnH "sport = :$p" 2>/dev/null)" ]]
  elif command -v lsof >/dev/null 2>&1; then
    [[ -n "$(lsof -ti "tcp:$p" -sTCP:LISTEN 2>/dev/null)" ]]
  else
    (exec 3<>"/dev/tcp/127.0.0.1/$p") 2>/dev/null
  fi
}

port_holder_pid() {
  local p="$1"
  if command -v ss >/dev/null 2>&1; then
    ss -ltnpH "sport = :$p" 2>/dev/null | grep -oP 'pid=\K[0-9]+' | head -1
  elif command -v lsof >/dev/null 2>&1; then
    lsof -ti "tcp:$p" -sTCP:LISTEN 2>/dev/null | head -1
  fi
}

port_holder_desc() {
  local p="$1" pid container
  pid="$(port_holder_pid "$p")"
  if command -v docker >/dev/null 2>&1; then
    container="$(docker ps --filter "publish=$p" --format '{{.Names}}' 2>/dev/null | head -1)"
    [[ -n "$container" ]] && { echo "docker container '$container'"; return; }
  fi
  if [[ -n "$pid" ]]; then
    echo "pid $pid ($(ps -p "$pid" -o comm= 2>/dev/null || echo '?'))"
  else
    echo "another process (owned by a different user or a container)"
  fi
}

check_port() {
  local p="$1" label="$2" pid var
  port_in_use "$p" || return 0
  var=$( [[ "$label" == api ]] && echo PORT || echo WEB_PORT )
  pid="$(port_holder_pid "$p")"
  if (( KILL_PORT )) && [[ -n "$pid" ]]; then
    warn "port $p ($label) held by $(port_holder_desc "$p") — killing it (--kill-port)"
    kill -TERM "$pid" 2>/dev/null
    for _ in $(seq 1 20); do
      port_in_use "$p" || return 0
      sleep 0.25
    done
    kill -KILL "$pid" 2>/dev/null
    sleep 0.5
    port_in_use "$p" && die "could not free port $p"
    return 0
  fi
  die "port $p ($label) is already in use by $(port_holder_desc "$p").
     Free it$( [[ -n "$pid" ]] && echo ", rerun with --kill-port," || echo "," ) or pick another port:
       $var=$((p+1)) ./scripts/dev.sh"
}

(( RUN_API )) && check_port "$PORT" api
(( RUN_WEB )) && check_port "$WEB_PORT" web

db_host="$(sed -E 's#^[a-z+]+://([^@]*@)?([^:/?]+).*#\2#' <<<"$DATABASE_URL")"
if [[ "$db_host" =~ ^(localhost|127\.0\.0\.1|\[::1\])$ ]]; then
  if command -v docker >/dev/null 2>&1; then
    say "▸ starting local postgres + redis (docker compose)"
    docker compose up -d postgres redis >/dev/null || die "docker compose failed"
    for _ in $(seq 1 60); do
      if docker compose exec -T postgres pg_isready -U djq -d disjobqueue >/dev/null 2>&1; then break; fi
      sleep 1
    done
  else
    warn "DATABASE_URL points at localhost but docker is not installed — start Postgres yourself"
  fi
fi

if (( MIGRATE )); then
  MIGRATE_BIN="$(command -v migrate || echo "$HOME/.local/bin/migrate")"
  [[ -x "$MIGRATE_BIN" ]] || die "migrate CLI not found (install golang-migrate or drop --migrate)"
  say "▸ running migrations"
  "$MIGRATE_BIN" -path db/migrations -database "$DATABASE_URL" up || die "migrations failed"
fi

if (( RUN_WEB )) && [[ ! -d "$ROOT/web/node_modules" ]]; then
  say "▸ installing web dependencies (first run)"
  (cd "$ROOT/web" && npm install) || die "npm install failed"
fi

mkdir -p "$LOG_DIR"

set -m
PIDS=(); NAMES=()
READY_PID=""
SHUTTING_DOWN=0

prefix_stream() {
  local label="$1" color="$2" logfile="$3" line
  while IFS= read -r line || [[ -n "$line" ]]; do
    printf '%s%-9s%s%s│%s %s\n' "$color$BOLD" "$label" "$RESET" "$DIM" "$RESET" "$line"
    printf '%s\n' "$line" >>"$logfile"
    line=""
  done
  return 0
}

go_fingerprint() {
  find "$1" -name '*.go' -printf '%T@ %p\n' 2>/dev/null | sort | cksum
}

start_service() {
  local label="$1" color="$2" workdir="$3" watchdir="$4"; shift 4
  local logfile="$LOG_DIR/$label.log"
  : >"$logfile"
  (
    set -m
    child=""
    trap 'kill -TERM -"$child" 2>/dev/null || kill -TERM "$child" 2>/dev/null; wait "$child" 2>/dev/null; exit 0' TERM INT

    while :; do
      ( cd "$workdir" && exec "$@" ) &
      child=$!

      if [[ "$watchdir" == "-" ]]; then
        wait "$child"; exit $?
      fi

      restarting=0
      last_fp="$(go_fingerprint "$watchdir")"
      while kill -0 "$child" 2>/dev/null; do
        sleep 1
        fp="$(go_fingerprint "$watchdir")"
        if [[ "$fp" != "$last_fp" ]]; then
          last_fp="$fp"; restarting=1
          echo "── change detected, restarting ──"
          kill -TERM -"$child" 2>/dev/null || kill -TERM "$child" 2>/dev/null
          wait "$child" 2>/dev/null
          break
        fi
      done
      (( restarting )) && continue

      wait "$child" 2>/dev/null; code=$?
      echo "── exited (code $code) — waiting for a code change ──"
      while :; do
        sleep 1
        fp="$(go_fingerprint "$watchdir")"
        if [[ "$fp" != "$last_fp" ]]; then
          last_fp="$fp"
          echo "── change detected, restarting ──"
          break
        fi
      done
    done
  ) > >(prefix_stream "$label" "$color" "$logfile") 2>&1 &
  PIDS+=("$!"); NAMES+=("$label")
}

shutdown() {
  (( SHUTTING_DOWN )) && return
  SHUTTING_DOWN=1
  printf '\n%s▸ shutting down…%s\n' "$CYAN$BOLD" "$RESET"
  local pid
  [[ -n "$READY_PID" ]] && kill -TERM "$READY_PID" 2>/dev/null
  for pid in "${PIDS[@]}"; do
    kill -TERM -"$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null
  done
  for _ in $(seq 1 40); do
    local alive=0
    for pid in "${PIDS[@]}"; do kill -0 "$pid" 2>/dev/null && alive=1; done
    (( alive )) || break
    sleep 0.25
  done
  for pid in "${PIDS[@]}"; do
    kill -KILL -"$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null
  done
  wait 2>/dev/null
  printf '%s▸ all services stopped. logs: %s%s\n' "$DIM" "$LOG_DIR" "$RESET"
}
trap 'shutdown; exit 0' INT TERM
trap 'shutdown' EXIT

printf '\n%s┌─ dis-job-queue dev ────────────────────────────────────%s\n' "$BOLD" "$RESET"
printf '%s│%s  api    %s\n' "$BOLD" "$RESET" "$( ((RUN_API))    && echo "http://localhost:$PORT"     || echo "${DIM}skipped$RESET" )"
printf '%s│%s  web    %s\n' "$BOLD" "$RESET" "$( ((RUN_WEB))    && echo "http://localhost:$WEB_PORT" || echo "${DIM}skipped$RESET" )"
printf '%s│%s  worker %s\n' "$BOLD" "$RESET" "$( ((RUN_WORKER)) && echo "$WORKER_REPLICAS × queues: ${WORKER_QUEUES:-default}" || echo "${DIM}skipped$RESET" )"
printf '%s│%s  watch  %s\n' "$BOLD" "$RESET" "$( ((WATCH)) && echo "on (go rebuild on change)" || echo "${DIM}off (--watch to enable)$RESET" )"
printf '%s└─ Ctrl-C stops everything ──────────────────────────────%s\n\n' "$BOLD" "$RESET"

if (( RUN_API )); then
  start_service "api" "$GREEN" "$ROOT/api" "$( ((WATCH)) && echo "$ROOT/api" || echo - )" \
    go run ./cmd/api
fi

if (( RUN_WORKER )); then
  if (( WORKER_REPLICAS <= 1 )); then
    start_service "worker" "$MAGENTA" "$ROOT/worker" "$( ((WATCH)) && echo "$ROOT/worker" || echo - )" \
      go run ./cmd/worker
  else
    for i in $(seq 1 "$WORKER_REPLICAS"); do
      start_service "worker-$i" "$MAGENTA" "$ROOT/worker" "$( ((WATCH)) && echo "$ROOT/worker" || echo - )" \
        go run ./cmd/worker
    done
  fi
fi

if (( RUN_WEB )); then
  start_service "web" "$BLUE" "$ROOT/web" "-" \
    npm run dev -- --port "$WEB_PORT"
fi

if (( RUN_API )) && command -v curl >/dev/null 2>&1; then
  (
    for _ in $(seq 1 120); do
      sleep 0.5
      if curl -sf "http://localhost:$PORT/health" >/dev/null 2>&1; then
        printf '\n%s✓ API ready at http://localhost:%s%s' "$GREEN$BOLD" "$PORT" "$RESET"
        (( RUN_WEB )) && printf '  ·  dashboard http://localhost:%s' "$WEB_PORT"
        printf '\n\n'
        exit 0
      fi
    done
  ) &
  READY_PID=$!
fi

while :; do
  for idx in "${!PIDS[@]}"; do
    if ! kill -0 "${PIDS[$idx]}" 2>/dev/null; then
      wait "${PIDS[$idx]}" 2>/dev/null; code=$?
      warn "▸ ${NAMES[$idx]} exited (code $code) — stopping the rest"
      shutdown
      exit "$code"
    fi
  done
  sleep 0.4
done

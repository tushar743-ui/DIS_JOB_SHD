#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
CALLER_PORT="${PORT:-}"
CALLER_PROJECT="${PROJECT_ID:-}"
[[ -f .env ]] && { set -a; source .env; set +a; }
[[ -n "$CALLER_PORT" ]] && PORT="$CALLER_PORT"
[[ -n "$CALLER_PROJECT" ]] && PROJECT_ID="$CALLER_PROJECT"

BASE="${API_BASE:-http://localhost:${PORT:-8080}}"
API="$BASE/api/v1"
EMAIL="${TEST_EMAIL:-tusharpatle743@gmail.com}"
PASSWORD="${TEST_PASSWORD:-password123}"
QUEUE_NAME="${CRON_QUEUE:-default}"
CRON_EXPR="${CRON_EXPR:-0 */2 * * *}"
COUNT="${CRON_COUNT:-50}"

usage() {
  cat <<USAGE
Seed recurring cron jobs.

  CRON_COUNT=50            how many distinct jobs      (default 50)
  CRON_EXPR="0 */2 * * *"  schedule                    (default every 2 hours)
  CRON_QUEUE=default       target queue by name        (default "default")
  PROJECT_ID=<uuid>        project to seed into        (default from .env)
  PORT=8080 | API_BASE=... where the API is listening    (caller wins over .env)

Re-running is safe: each job carries a stable idempotency_key, so existing
jobs are reported as skipped instead of duplicated.
USAGE
}

[[ "${1:-}" == "--help" || "${1:-}" == "-h" ]] && { usage; exit 0; }

command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

curl -sf "$BASE/health" >/dev/null || {
  echo "API not reachable at $BASE — start it with 'make dev' first" >&2
  exit 1
}

TOKEN="$(curl -s -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d "$(jq -n --arg e "$EMAIL" --arg p "$PASSWORD" '{email:$e,password:$p}')" |
  jq -r '.access_token // empty')"
[[ -n "$TOKEN" ]] || { echo "login failed for $EMAIL" >&2; exit 1; }

PROJECT="${PROJECT_ID:-}"
if [[ -z "$PROJECT" ]]; then
  ORG="$(curl -s "$API/orgs" -H "Authorization: Bearer $TOKEN" | jq -r '.[0].id // empty')"
  PROJECT="$(curl -s "$API/orgs/$ORG/projects" -H "Authorization: Bearer $TOKEN" | jq -r '.[0].id // empty')"
fi
[[ -n "$PROJECT" ]] || { echo "could not resolve a project" >&2; exit 1; }

QUEUE_ID="$(curl -s "$API/projects/$PROJECT/queues" -H "Authorization: Bearer $TOKEN" |
  jq -r --arg n "$QUEUE_NAME" '.[] | select(.name==$n) | .id' | head -1)"
[[ -n "$QUEUE_ID" ]] || { echo "queue \"$QUEUE_NAME\" not found in project $PROJECT" >&2; exit 1; }

NEXT_RUN="$(date -u -d "$(date -u +%Y-%m-%dT%H:00:00Z) +2 hours" +%Y-%m-%dT%H:00:00Z)"

PAYLOAD="$(jq -n \
  --arg cron "$CRON_EXPR" \
  --arg at "$NEXT_RUN" \
  --argjson count "$COUNT" \
  '[
     "process_order","sync_inventory","generate_report","cleanup_temp_files",
     "send_email","send_bulk_email","push_notification","send_sms",
     "etl_batch","transcode_video","process_payment","fraud_check",
     "compliance_alert","compliance_report","heartbeat_check","batch_op",
     "batch_process","scheduled_cleanup"
   ] as $types
   | [range(0; $count) | {
       type: $types[. % ($types | length)],
       payload: { seed: "cron-2h", slot: . },
       priority: (5 + (. % 3)),
       max_attempts: 3,
       timeout_secs: 300,
       cron_expression: $cron,
       scheduled_at: $at,
       idempotency_key: "cron-2h-\(.)",
       tags: ["cron","seeded"]
     }]')"

RESPONSE="$(curl -s -X POST "$API/queues/$QUEUE_ID/jobs/batch" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  --data-binary "$PAYLOAD")"

CREATED="$(jq -r '.count // 0' <<<"$RESPONSE")"
SKIPPED="$(jq -r '.skipped // 0' <<<"$RESPONSE")"

if [[ "$CREATED" == "0" && "$SKIPPED" == "0" ]]; then
  echo "batch rejected: $RESPONSE" >&2
  exit 1
fi

printf 'queue     %s (%s)\n' "$QUEUE_NAME" "$QUEUE_ID"
printf 'schedule  %s — next run %s\n' "$CRON_EXPR" "$NEXT_RUN"
printf 'created   %s\n' "$CREATED"
printf 'skipped   %s (already seeded)\n' "$SKIPPED"
printf 'batch_id  %s\n' "$(jq -r '.batch_id // "-"' <<<"$RESPONSE")"

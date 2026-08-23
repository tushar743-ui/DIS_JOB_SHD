#!/usr/bin/env bash
# Scheduled-job test suite: delayed jobs, cron jobs, and every action that can be
# taken on them (promote, cancel, retry, delete, DLQ round-trip).
#
# Needs the stack running (./scripts/dev.sh) and credentials in .env.

set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
[[ -f .env ]] && { set -a; source .env; set +a; }

BASE="http://localhost:${PORT:-8080}"
API="$BASE/api/v1"
EMAIL="${TEST_EMAIL:-tusharpatle743@gmail.com}"
PASSWORD="${TEST_PASSWORD:-password123}"
PROJECT="${PROJECT_ID:?PROJECT_ID must be set}"

PASS=0; FAIL=0; FAILURES=()

green() { echo -e "\033[32m$*\033[0m"; }
red()   { echo -e "\033[31m$*\033[0m"; }
dim()   { echo -e "\033[2m$*\033[0m"; }

ok()   { PASS=$((PASS+1)); green "  [PASS] $1"; }
fail() { FAIL=$((FAIL+1)); FAILURES+=("$1 - $2"); red "  [FAIL] $1 - $2"; }

# req METHOD PATH [BODY] -> sets CODE and BODY_OUT
req() {
  local method="$1" path="$2" body="${3:-}" out
  if [[ -n "$body" ]]; then
    out=$(curl -s -w $'\n%{http_code}' -X "$method" "$API$path" \
      -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$body")
  else
    out=$(curl -s -w $'\n%{http_code}' -X "$method" "$API$path" \
      -H "Authorization: Bearer $TOKEN")
  fi
  CODE="${out##*$'\n'}"
  BODY_OUT="${out%$'\n'*}"
}

expect_code() {  # label expected
  if [[ "$CODE" == "$2" ]]; then ok "$1 (HTTP $CODE)"
  else fail "$1" "expected HTTP $2, got $CODE — $(head -c 200 <<<"$BODY_OUT")"; fi
}

job_status() { req GET "/jobs/$1"; jq -r '.status' <<<"$BODY_OUT"; }

# wait_status JOB_ID TIMEOUT STATUS... -> 0 when the job reaches one of STATUS
wait_status() {
  local id="$1" timeout="$2"; shift 2
  local deadline=$((SECONDS + timeout)) s
  while (( SECONDS < deadline )); do
    s="$(job_status "$id")"
    for want in "$@"; do [[ "$s" == "$want" ]] && { LAST_STATUS="$s"; return 0; }; done
    sleep 1
  done
  LAST_STATUS="$s"
  return 1
}

iso_in() { date -u -d "+$1 seconds" +%Y-%m-%dT%H:%M:%SZ; }

echo "=============================================================="
echo " Scheduled jobs — test suite    $(date '+%F %T')"
echo "=============================================================="

curl -sf "$BASE/health" >/dev/null || { red "API not reachable at $BASE — start it with ./scripts/dev.sh"; exit 1; }

TOKEN=$(curl -s -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}" | jq -r '.access_token // .token // empty')
[[ -n "$TOKEN" ]] || { red "login failed for $EMAIL"; exit 1; }

QUEUE=$(curl -s -H "Authorization: Bearer $TOKEN" "$API/projects/$PROJECT/queues" | jq -r '.[0].id')
[[ -n "$QUEUE" && "$QUEUE" != "null" ]] || { red "no queue found in project $PROJECT"; exit 1; }
dim "  queue $QUEUE"

# new_job JSON -> sets JOB_ID (plus CODE/BODY_OUT from the create call)
new_job() {
  req POST "/queues/$QUEUE/jobs" "$1"
  JOB_ID=$(jq -r '.id // empty' <<<"$BODY_OUT")
}

echo ""
echo "=== 1. CREATE — a job scheduled for the future is 'scheduled' ==="
new_job "{\"type\":\"scheduled_cleanup\",\"scheduled_at\":\"$(iso_in 8)\",\"payload\":{\"t\":1}}"; J1=$JOB_ID
expect_code "create scheduled job" 201
[[ -n "$J1" ]] && ok "returned job id" || fail "returned job id" "no id in $BODY_OUT"
S=$(job_status "$J1")
[[ "$S" == "scheduled" ]] && ok "status is scheduled" || fail "status is scheduled" "got $S"

echo ""
echo "=== 2. CREATE — a past scheduled_at runs immediately ==="
new_job "{\"type\":\"scheduled_cleanup\",\"scheduled_at\":\"$(date -u -d '-60 seconds' +%Y-%m-%dT%H:%M:%SZ)\"}"; J2=$JOB_ID
S=$(job_status "$J2")
[[ "$S" != "scheduled" ]] && ok "past scheduled_at is not left scheduled (status $S)" \
  || fail "past scheduled_at" "still scheduled"

echo ""
echo "=== 3. PROMOTION — the scheduler promotes it and a worker runs it ==="
if wait_status "$J1" 45 completed failed dead; then
  [[ "$LAST_STATUS" == "completed" ]] && ok "scheduled job ran and completed" \
    || fail "scheduled job ran" "ended as $LAST_STATUS"
else
  fail "scheduled job promoted within 45s" "still $LAST_STATUS"
fi
req GET "/jobs/$J1/executions"
[[ "$(jq 'length' <<<"$BODY_OUT")" -ge 1 ]] && ok "execution row recorded" \
  || fail "execution row recorded" "none found"
req GET "/jobs/$J1/logs"
[[ "$(jq 'length' <<<"$BODY_OUT")" -ge 1 ]] && ok "job log recorded" || fail "job log recorded" "none found"

echo ""
echo "=== 4. LIST — status filter returns scheduled jobs ==="
new_job "{\"type\":\"scheduled_cleanup\",\"scheduled_at\":\"$(iso_in 3600)\"}"; J3=$JOB_ID
req GET "/queues/$QUEUE/jobs?status=scheduled&limit=100"
expect_code "list ?status=scheduled" 200
jq -e --arg id "$J3" '.data | map(.id) | index($id)' <<<"$BODY_OUT" >/dev/null \
  && ok "scheduled job present in filtered list" || fail "scheduled job in list" "id $J3 missing"
jq -e '.data | map(select(.status != "scheduled")) | length == 0' <<<"$BODY_OUT" >/dev/null \
  && ok "filter returns only scheduled jobs" || fail "filter purity" "other statuses returned"

echo ""
echo "=== 5. CANCEL — a scheduled job can be cancelled ==="
req DELETE "/jobs/$J3"
expect_code "cancel scheduled job" 200
S=$(job_status "$J3"); [[ "$S" == "cancelled" ]] && ok "status is cancelled" || fail "status cancelled" "got $S"
req DELETE "/jobs/$J3"
expect_code "cancel again is rejected" 409
sleep 7
S=$(job_status "$J3")
[[ "$S" == "cancelled" ]] && ok "scheduler leaves cancelled jobs alone" \
  || fail "cancelled job stays cancelled" "scheduler moved it to $S"

echo ""
echo "=== 6. RETRY — a cancelled job can be re-queued and runs ==="
req POST "/jobs/$J3/retry"
expect_code "retry cancelled job" 200
if wait_status "$J3" 40 completed failed dead; then
  [[ "$LAST_STATUS" == "completed" ]] && ok "retried job completed" || fail "retried job" "ended as $LAST_STATUS"
else
  fail "retried job ran within 40s" "still $LAST_STATUS"
fi

echo ""
echo "=== 7. RETRY — a scheduled job cannot be retried ==="
new_job "{\"type\":\"scheduled_cleanup\",\"scheduled_at\":\"$(iso_in 3600)\"}"; J4=$JOB_ID
req POST "/jobs/$J4/retry"
expect_code "retry of a scheduled job rejected" 409

echo ""
echo "=== 8. DELETE — a scheduled job can be purged ==="
req DELETE "/jobs/$J4/purge"
expect_code "purge scheduled job" 200
req GET "/jobs/$J4"
expect_code "purged job is gone" 404
req DELETE "/jobs/00000000-0000-0000-0000-000000000000/purge"
expect_code "purge of unknown job" 404

echo ""
echo "=== 9. DLQ — a scheduled job that keeps failing lands in the DLQ ==="
new_job "{\"type\":\"always_fail\",\"max_attempts\":2,\"scheduled_at\":\"$(iso_in 6)\"}"; J5=$JOB_ID
if wait_status "$J5" 60 dead; then ok "failing scheduled job reached 'dead'"
else fail "failing scheduled job reaches dead" "still $LAST_STATUS"; fi
req GET "/queues/$QUEUE/dlq?limit=100"
DLQ_ID=$(jq -r --arg id "$J5" '.data[]? | select(.job_id == $id and .resolved_at == null) | .id' <<<"$BODY_OUT" | head -1)
[[ -n "$DLQ_ID" ]] && ok "DLQ entry created" || fail "DLQ entry created" "job $J5 not in DLQ list"
if [[ -n "$DLQ_ID" ]]; then
  req POST "/dlq/$DLQ_ID/retry"
  expect_code "DLQ retry" 200
  S=$(job_status "$J5")
  [[ "$S" == "queued" || "$S" == "claimed" || "$S" == "running" ]] && ok "job re-queued from DLQ (status $S)" \
    || fail "job re-queued from DLQ" "status $S"
  wait_status "$J5" 60 dead >/dev/null
  req GET "/queues/$QUEUE/dlq?limit=100"
  DLQ_ID2=$(jq -r --arg id "$J5" '.data[]? | select(.job_id == $id and .resolved_at == null) | .id' <<<"$BODY_OUT" | head -1)
  [[ -n "$DLQ_ID2" ]] && ok "re-failed job is back in the DLQ" || fail "re-failed job back in DLQ" "no entry"
  [[ -n "$DLQ_ID2" ]] && { req DELETE "/dlq/$DLQ_ID2"; expect_code "discard DLQ entry" 204; }
  req DELETE "/dlq/00000000-0000-0000-0000-000000000000"
  expect_code "discard of unknown DLQ entry" 404
fi

echo ""
echo "=== 10. CRON — a recurring job reschedules itself ==="
new_job '{"type":"scheduled_cleanup","cron_expression":"*/10 * * * * *"}'; J6=$JOB_ID
expect_code "create cron job" 201
if wait_status "$J6" 45 scheduled; then ok "cron job returned to 'scheduled' after its first run"
else fail "cron job rescheduled" "status $LAST_STATUS"; fi
req GET "/jobs/$J6"
RUNAT=$(jq -r '.run_at' <<<"$BODY_OUT")
DELTA=$(( $(date -d "$RUNAT" +%s) - $(date +%s) ))
if (( DELTA > -5 && DELTA < 60 )); then
  ok "cron next run_at is the next tick (${DELTA}s away)"
else
  fail "cron next run_at computed" "run_at is $RUNAT (${DELTA}s away) — not the next cron tick"
fi
req GET "/jobs/$J6/executions"
RUNS1=$(jq 'length' <<<"$BODY_OUT")
dim "  waiting 25s for a second cron run…"; sleep 25
req GET "/jobs/$J6/executions"
RUNS2=$(jq 'length' <<<"$BODY_OUT")
(( RUNS2 > RUNS1 )) && ok "cron job ran again ($RUNS1 → $RUNS2 executions)" \
  || fail "cron job ran again" "still $RUNS2 executions after 25s"

echo ""
echo "=== 11. CRON — cancel and purge a recurring job ==="
req DELETE "/jobs/$J6/purge"
expect_code "purge the 10s cron job" 200
# An hourly job sits in 'scheduled' between runs, so cancel is not racing a run.
new_job '{"type":"scheduled_cleanup","cron_expression":"0 0 * * * *"}'; J8=$JOB_ID
wait_status "$J8" 45 scheduled >/dev/null
req DELETE "/jobs/$J8"
expect_code "cancel cron job" 200
sleep 12
S=$(job_status "$J8")
[[ "$S" == "cancelled" ]] && ok "cancelled cron job is not rescheduled" \
  || fail "cancelled cron job stays cancelled" "scheduler moved it to $S"
req DELETE "/jobs/$J8/purge"
expect_code "purge cron job" 200

echo ""
echo "=== 12. IDEMPOTENCY — duplicate key on a scheduled job is rejected ==="
KEY="sched-idem-$RANDOM$RANDOM"
new_job "{\"type\":\"scheduled_cleanup\",\"idempotency_key\":\"$KEY\",\"scheduled_at\":\"$(iso_in 3600)\"}"; J7=$JOB_ID
expect_code "create with idempotency key" 201
req POST "/queues/$QUEUE/jobs" "{\"type\":\"scheduled_cleanup\",\"idempotency_key\":\"$KEY\",\"scheduled_at\":\"$(iso_in 3600)\"}"
expect_code "duplicate idempotency key rejected" 409
[[ -n "$J7" ]] && { req DELETE "/jobs/$J7/purge"; expect_code "cleanup purge" 200; }

echo ""
echo "=== 13. CRON — an explicit first run time is honoured ==="
FIRST=$(iso_in 1800)
new_job "{\"type\":\"scheduled_cleanup\",\"cron_expression\":\"*/10 * * * * *\",\"scheduled_at\":\"$FIRST\"}"; J9=$JOB_ID
expect_code "create cron job with scheduled_at" 201
sleep 7   # give the scheduler a tick to (not) overwrite it
req GET "/jobs/$J9"
RUNAT=$(jq -r '.run_at' <<<"$BODY_OUT")
DELTA=$(( $(date -d "$RUNAT" +%s) - $(date -d "$FIRST" +%s) ))
(( DELTA > -5 && DELTA < 5 )) && ok "first run stays at scheduled_at ($RUNAT)" \
  || fail "first run honours scheduled_at" "run_at moved to $RUNAT"
S=$(job_status "$J9")
[[ "$S" == "scheduled" ]] && ok "cron job with a future first run stays scheduled" || fail "cron job stays scheduled" "got $S"
req DELETE "/jobs/$J9/purge"; expect_code "cleanup purge" 200

echo ""
echo "=== 14. BATCH — scheduled jobs can be created in bulk ==="
req POST "/queues/$QUEUE/jobs/batch" "[{\"type\":\"scheduled_cleanup\",\"scheduled_at\":\"$(iso_in 3600)\"},{\"type\":\"scheduled_cleanup\",\"scheduled_at\":\"$(iso_in 3600)\"}]"
expect_code "batch create scheduled jobs" 201
mapfile -t BATCH_IDS < <(jq -r '.job_ids[]' <<<"$BODY_OUT")
(( ${#BATCH_IDS[@]} == 2 )) && ok "batch returned 2 job ids" || fail "batch job ids" "got ${#BATCH_IDS[@]}"
BAD=0
for id in "${BATCH_IDS[@]}"; do [[ "$(job_status "$id")" == "scheduled" ]] || BAD=1; done
(( BAD == 0 )) && ok "batch jobs are scheduled" || fail "batch jobs scheduled" "some are not"
for id in "${BATCH_IDS[@]}"; do req DELETE "/jobs/$id/purge"; done
ok "batch jobs purged"

echo ""
echo "=============================================================="
echo " passed: $PASS   failed: $FAIL"
for f in "${FAILURES[@]:-}"; do [[ -n "$f" ]] && red "  ✗ $f"; done
echo "=============================================================="
(( FAIL == 0 ))

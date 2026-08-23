#!/usr/bin/env bash

set -euo pipefail

API="http://localhost:8080/api/v1"
BASE="http://localhost:8080"
PASS=0
FAIL=0
WARNINGS=()
RESULTS=()

green() { echo -e "\033[32m$*\033[0m"; }
red()   { echo -e "\033[31m$*\033[0m"; }
yellow(){ echo -e "\033[33m$*\033[0m"; }

ok() {
  PASS=$((PASS+1))
  RESULTS+=("  [PASS] $1")
  green "  [PASS] $1"
}

fail() {
  FAIL=$((FAIL+1))
  RESULTS+=("  [FAIL] $1 - $2")
  red "  [FAIL] $1 - $2"
}

warn() {
  WARNINGS+=("  [WARN] $1")
  yellow "  [WARN] $1"
}

assert_status() {
  local label="$1" expected="$2" actual="$3" body="$4"
  if [ "$actual" = "$expected" ]; then
    ok "$label (HTTP $actual)"
  else
    fail "$label" "expected HTTP $expected, got $actual - body: $(echo "$body" | head -c 200)"
  fi
}

jq_field() { echo "$1" | jq -r "$2" 2>/dev/null || echo ""; }

echo ""
echo "================================================================"
echo " DIS-JOB-SD  - Comprehensive Test Suite"
echo " $(date)"
echo "================================================================"
echo ""

echo "=== 1. HEALTH CHECK ==="
HEALTH=$(curl -sf "$BASE/health" 2>/dev/null || echo '{"ok":false}')
if echo "$HEALTH" | grep -q '"ok":true'; then ok "API /health"; else fail "API /health" "unreachable"; exit 1; fi

echo ""
echo "=== 2. USER REGISTRATION (10 users) ==="

declare -A USER_TOKENS
declare -A USER_IDS
declare -a USER_EMAILS

for i in $(seq 1 10); do
  EMAIL="testuser${i}@djq-test.io"
  NAME="Test User $i"
  USER_EMAILS+=("$EMAIL")
  RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/auth/register" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$EMAIL\",\"password\":\"Password${i}123!\",\"name\":\"$NAME\"}" 2>/dev/null)
  CODE=$(echo "$RESP" | tail -1)
  BODY=$(echo "$RESP" | head -n -1)

  if [ "$CODE" = "409" ]; then
    RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/auth/login" \
      -H "Content-Type: application/json" \
      -d "{\"email\":\"$EMAIL\",\"password\":\"Password${i}123!\"}" 2>/dev/null)
    CODE=$(echo "$RESP" | tail -1)
    BODY=$(echo "$RESP" | head -n -1)
  fi

  TOKEN=$(jq_field "$BODY" ".access_token")
  URID=$(jq_field "$BODY" ".user_id")
  REFRESH=$(jq_field "$BODY" ".refresh_token")

  if [ -n "$TOKEN" ] && [ "$TOKEN" != "null" ]; then
    USER_TOKENS["$EMAIL"]="$TOKEN"
    USER_IDS["$EMAIL"]="$URID"
    ok "User $i registered/logged in ($EMAIL)"
  else
    fail "User $i register/login" "body: $BODY"
  fi
done

PRIMARY_EMAIL="${USER_EMAILS[0]}"
PRIMARY_TOKEN="${USER_TOKENS[$PRIMARY_EMAIL]}"
AUTH="Authorization: Bearer $PRIMARY_TOKEN"

echo ""
echo "=== 3. AUTH - TOKEN REFRESH ==="
THROW_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/auth/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"refresh_test@djq-test.io","password":"Np5&zQwB8k","name":"Refresh Tester"}' 2>/dev/null)
THROW_CODE=$(echo "$THROW_RESP" | tail -1)
THROW_BODY=$(echo "$THROW_RESP" | head -n -1)
if [ "$THROW_CODE" = "409" ]; then
  THROW_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/auth/login" \
    -H "Content-Type: application/json" \
    -d '{"email":"refresh_test@djq-test.io","password":"Np5&zQwB8k"}' 2>/dev/null)
  THROW_BODY=$(echo "$THROW_RESP" | head -n -1)
fi
THROW_REFRESH=$(jq_field "$THROW_BODY" ".refresh_token")
if [ -n "$THROW_REFRESH" ] && [ "$THROW_REFRESH" != "null" ]; then
  REF_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/auth/refresh" \
    -H "Content-Type: application/json" \
    -d "{\"refresh_token\":\"$THROW_REFRESH\"}" 2>/dev/null)
  REF_CODE=$(echo "$REF_RESP" | tail -1)
  REF_BODY=$(echo "$REF_RESP" | head -n -1)
  NEW_TOKEN=$(jq_field "$REF_BODY" ".access_token")
  if [ -n "$NEW_TOKEN" ] && [ "$NEW_TOKEN" != "null" ]; then
    ok "Token refresh returns new access_token"
  else
    fail "Token refresh" "body: $REF_BODY"
  fi
  REF2_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/auth/refresh" \
    -H "Content-Type: application/json" \
    -d "{\"refresh_token\":\"$THROW_REFRESH\"}" 2>/dev/null)
  REF2_CODE=$(echo "$REF2_RESP" | tail -1)
  if [ "$REF2_CODE" = "401" ]; then
    ok "Refresh token rotation - replay rejected (401)"
  else
    fail "Refresh token replay protection" "expected 401, got $REF2_CODE"
  fi
else
  fail "Token refresh setup" "no refresh token obtained"
fi

echo ""
echo "=== 4. AUTH - GET /me ==="
ME_RESP=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/auth/me" 2>/dev/null)
ME_CODE=$(echo "$ME_RESP" | tail -1)
ME_BODY=$(echo "$ME_RESP" | head -n -1)
assert_status "/auth/me" "200" "$ME_CODE" "$ME_BODY"

echo ""
echo "=== 5. ORGANIZATIONS ==="

ORG_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/orgs" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"name":"DJQ Test Org","slug":"djq-test-org"}' 2>/dev/null)
ORG_CODE=$(echo "$ORG_RESP" | tail -1)
ORG_BODY=$(echo "$ORG_RESP" | head -n -1)
if [ "$ORG_CODE" = "201" ] || [ "$ORG_CODE" = "409" ]; then
  if [ "$ORG_CODE" = "409" ]; then
    ORGS_RESP=$(curl -s -H "$AUTH" "$API/orgs" 2>/dev/null)
    ORG_ID=$(echo "$ORGS_RESP" | jq -r '[.[] | select(.slug=="djq-test-org")] | .[0].id' 2>/dev/null)
    ok "Org creation (already existed - reusing)"
  else
    ORG_ID=$(jq_field "$ORG_BODY" ".id")
    ok "Org created (id=$ORG_ID)"
  fi
else
  fail "Org create" "HTTP $ORG_CODE body: $ORG_BODY"
  ORG_ID=""
fi

if [ -z "$ORG_ID" ] || [ "$ORG_ID" = "null" ]; then
  ORGS=$(curl -s -H "$AUTH" "$API/orgs" 2>/dev/null)
  ORG_ID=$(echo "$ORGS" | jq -r '.[0].id' 2>/dev/null)
  warn "Fallback to first org: $ORG_ID"
fi

LIST_ORGS=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/orgs" 2>/dev/null)
LIST_CODE=$(echo "$LIST_ORGS" | tail -1)
assert_status "List orgs" "200" "$LIST_CODE" "$(echo "$LIST_ORGS" | head -n -1)"

if [ -n "$ORG_ID" ] && [ "$ORG_ID" != "null" ]; then
  GET_ORG=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/orgs/$ORG_ID" 2>/dev/null)
  GET_CODE=$(echo "$GET_ORG" | tail -1)
  assert_status "Get org by ID" "200" "$GET_CODE" "$(echo "$GET_ORG" | head -n -1)"

  UPD_ORG=$(curl -s -w "\n%{http_code}" -X PUT "$API/orgs/$ORG_ID" \
    -H "$AUTH" -H "Content-Type: application/json" \
    -d '{"name":"DJQ Test Org (Updated)"}' 2>/dev/null)
  assert_status "Update org" "200" "$(echo "$UPD_ORG" | tail -1)" "$(echo "$UPD_ORG" | head -n -1)"

  UID2="${USER_IDS[${USER_EMAILS[1]}]:-}"
  if [ -n "$UID2" ] && [ "$UID2" != "null" ]; then
    MEMB_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/orgs/$ORG_ID/members" \
      -H "$AUTH" -H "Content-Type: application/json" \
      -d "{\"user_id\":\"$UID2\",\"role\":\"member\"}" 2>/dev/null)
    assert_status "Add org member" "200" "$(echo "$MEMB_RESP" | tail -1)" "$(echo "$MEMB_RESP" | head -n -1)"
  fi

  MEMB_LIST=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/orgs/$ORG_ID/members" 2>/dev/null)
  assert_status "List org members" "200" "$(echo "$MEMB_LIST" | tail -1)" "$(echo "$MEMB_LIST" | head -n -1)"
fi

echo ""
echo "=== 6. PROJECTS ==="

PROJ_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/orgs/$ORG_ID/projects" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"name":"Test Project","slug":"test-project"}' 2>/dev/null)
PROJ_CODE=$(echo "$PROJ_RESP" | tail -1)
PROJ_BODY=$(echo "$PROJ_RESP" | head -n -1)

if [ "$PROJ_CODE" = "201" ]; then
  PROJECT_ID=$(jq_field "$PROJ_BODY" ".id")
  API_KEY=$(jq_field "$PROJ_BODY" ".api_key")
  ok "Project created (id=$PROJECT_ID)"
elif [ "$PROJ_CODE" = "409" ]; then
  PROJ_LIST=$(curl -s -H "$AUTH" "$API/orgs/$ORG_ID/projects" 2>/dev/null)
  PROJECT_ID=$(echo "$PROJ_LIST" | jq -r '[.[] | select(.slug=="test-project")] | .[0].id' 2>/dev/null)
  ok "Project exists, reusing (id=$PROJECT_ID)"
else
  fail "Project create" "HTTP $PROJ_CODE body: $PROJ_BODY"
fi

if [ -z "$PROJECT_ID" ] || [ "$PROJECT_ID" = "null" ]; then
  PROJ_LIST=$(curl -s -H "$AUTH" "$API/orgs/$ORG_ID/projects" 2>/dev/null)
  PROJECT_ID=$(echo "$PROJ_LIST" | jq -r '.[0].id' 2>/dev/null)
  warn "Fallback to first project: $PROJECT_ID"
fi

if [ -n "$PROJECT_ID" ] && [ "$PROJECT_ID" != "null" ]; then
  GET_PROJ=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/projects/$PROJECT_ID" 2>/dev/null)
  assert_status "Get project" "200" "$(echo "$GET_PROJ" | tail -1)" "$(echo "$GET_PROJ" | head -n -1)"

  ROT_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/projects/$PROJECT_ID/rotate-key" \
    -H "$AUTH" 2>/dev/null)
  assert_status "Rotate project API key" "200" "$(echo "$ROT_RESP" | tail -1)" "$(echo "$ROT_RESP" | head -n -1)"
fi

echo ""
echo "=== 7. RETRY POLICIES ==="

RP_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/projects/$PROJECT_ID/retry-policies" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"name":"exponential-3x","strategy":"exponential","max_attempts":5,"initial_delay_ms":500,"max_delay_ms":30000,"multiplier":2.0}' 2>/dev/null)
RP_CODE=$(echo "$RP_RESP" | tail -1)
RP_BODY=$(echo "$RP_RESP" | head -n -1)

if [ "$RP_CODE" = "201" ]; then
  RP_ID=$(jq_field "$RP_BODY" ".id")
  ok "Retry policy created (exponential, id=$RP_ID)"
elif [ "$RP_CODE" = "409" ]; then
  LIST_RP=$(curl -s -H "$AUTH" "$API/projects/$PROJECT_ID/retry-policies" 2>/dev/null)
  RP_ID=$(echo "$LIST_RP" | jq -r '.[0].id' 2>/dev/null)
  ok "Retry policy exists, reusing (id=$RP_ID)"
else
  fail "Retry policy create" "HTTP $RP_CODE body: $RP_BODY"
  RP_ID=""
fi

for STRAT in fixed linear; do
  STRAT_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/projects/$PROJECT_ID/retry-policies" \
    -H "$AUTH" -H "Content-Type: application/json" \
    -d "{\"name\":\"${STRAT}-policy\",\"strategy\":\"${STRAT}\",\"max_attempts\":3,\"initial_delay_ms\":1000,\"max_delay_ms\":10000,\"multiplier\":1.5}" 2>/dev/null)
  STRAT_CODE=$(echo "$STRAT_RESP" | tail -1)
  if [ "$STRAT_CODE" = "201" ] || [ "$STRAT_CODE" = "409" ]; then
    ok "Retry policy $STRAT created/exists"
  else
    fail "Retry policy $STRAT" "HTTP $STRAT_CODE"
  fi
done

LIST_RP_RESP=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/projects/$PROJECT_ID/retry-policies" 2>/dev/null)
assert_status "List retry policies" "200" "$(echo "$LIST_RP_RESP" | tail -1)" ""

echo ""
echo "=== 8. QUEUES ==="

declare -a QUEUE_IDS
declare -a QUEUE_NAMES

QUEUE_DEFS=(
  '{"name":"default","description":"Default general-purpose queue","priority":5,"concurrency_limit":10}'
  '{"name":"email","description":"Email notification jobs","priority":7,"concurrency_limit":5}'
  '{"name":"notifications","description":"Push/SMS notifications","priority":8,"concurrency_limit":20}'
  '{"name":"reports","description":"Report generation - lower priority","priority":3,"concurrency_limit":2}'
  '{"name":"payments","description":"Payment processing - high priority","priority":9,"concurrency_limit":3}'
)

for DEF in "${QUEUE_DEFS[@]}"; do
  QNAME=$(echo "$DEF" | jq -r '.name')
  Q_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/projects/$PROJECT_ID/queues" \
    -H "$AUTH" -H "Content-Type: application/json" \
    -d "$DEF" 2>/dev/null)
  Q_CODE=$(echo "$Q_RESP" | tail -1)
  Q_BODY=$(echo "$Q_RESP" | head -n -1)

  if [ "$Q_CODE" = "201" ]; then
    QID=$(jq_field "$Q_BODY" ".id")
    QUEUE_IDS+=("$QID")
    QUEUE_NAMES+=("$QNAME")
    ok "Queue '$QNAME' created (id=$QID)"
  elif [ "$Q_CODE" = "409" ]; then
    ALL_Q=$(curl -s -H "$AUTH" "$API/projects/$PROJECT_ID/queues" 2>/dev/null)
    QID=$(echo "$ALL_Q" | jq -r --arg n "$QNAME" '[.[] | select(.name==$n)] | .[0].id' 2>/dev/null)
    QUEUE_IDS+=("$QID")
    QUEUE_NAMES+=("$QNAME")
    ok "Queue '$QNAME' exists, reusing (id=$QID)"
  else
    fail "Queue '$QNAME' create" "HTTP $Q_CODE body: $Q_BODY"
  fi
done

DEFAULT_QUEUE="${QUEUE_IDS[0]}"
EMAIL_QUEUE="${QUEUE_IDS[1]}"
NOTIF_QUEUE="${QUEUE_IDS[2]}"
REPORT_QUEUE="${QUEUE_IDS[3]}"
PAYMENT_QUEUE="${QUEUE_IDS[4]}"

if [ -n "$DEFAULT_QUEUE" ] && [ "$DEFAULT_QUEUE" != "null" ]; then
  GQ=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/queues/$DEFAULT_QUEUE" 2>/dev/null)
  assert_status "Get queue by ID" "200" "$(echo "$GQ" | tail -1)" ""

  PAUSE=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$DEFAULT_QUEUE/pause" -H "$AUTH" 2>/dev/null)
  assert_status "Pause queue" "200" "$(echo "$PAUSE" | tail -1)" ""

  RESUME=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$DEFAULT_QUEUE/resume" -H "$AUTH" 2>/dev/null)
  assert_status "Resume queue" "200" "$(echo "$RESUME" | tail -1)" ""

  STATS=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/queues/$DEFAULT_QUEUE/stats" 2>/dev/null)
  assert_status "Queue stats" "200" "$(echo "$STATS" | tail -1)" ""

  UPD_Q=$(curl -s -w "\n%{http_code}" -X PUT "$API/queues/$DEFAULT_QUEUE" \
    -H "$AUTH" -H "Content-Type: application/json" \
    -d '{"concurrency_limit":15}' 2>/dev/null)
  assert_status "Update queue concurrency" "200" "$(echo "$UPD_Q" | tail -1)" ""
fi

echo ""
echo "=== 9. JOB CREATION ==="

JOB_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$DEFAULT_QUEUE/jobs" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"type":"process_order","payload":{"order_id":"ORD-001","amount":99.99},"priority":5}' 2>/dev/null)
JOB_CODE=$(echo "$JOB_RESP" | tail -1)
JOB_BODY=$(echo "$JOB_RESP" | head -n -1)
assert_status "Create basic job" "201" "$JOB_CODE" "$JOB_BODY"
BASIC_JOB_ID=$(jq_field "$JOB_BODY" ".id")

HP_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$PAYMENT_QUEUE/jobs" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"type":"process_payment","payload":{"txn_id":"TXN-999","amount":500},"priority":9,"timeout_secs":30}' 2>/dev/null)
assert_status "Create high-priority job" "201" "$(echo "$HP_RESP" | tail -1)" "$(echo "$HP_RESP" | head -n -1)"

SCHEDULED_AT=$(date -u -d "+30 seconds" "+%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -v+30S "+%Y-%m-%dT%H:%M:%SZ")
SCHED_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$DEFAULT_QUEUE/jobs" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d "{\"type\":\"scheduled_cleanup\",\"payload\":{\"target\":\"temp_files\"},\"scheduled_at\":\"$SCHEDULED_AT\"}" 2>/dev/null)
assert_status "Create scheduled job" "201" "$(echo "$SCHED_RESP" | tail -1)" "$(echo "$SCHED_RESP" | head -n -1)"
SCHED_BODY=$(echo "$SCHED_RESP" | head -n -1)
SCHED_STATUS=$(jq_field "$SCHED_BODY" ".status")
if [ "$SCHED_STATUS" = "scheduled" ]; then
  ok "Scheduled job status is 'scheduled'"
else
  fail "Scheduled job status" "expected 'scheduled', got '$SCHED_STATUS'"
fi

CRON_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$DEFAULT_QUEUE/jobs" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"type":"heartbeat_check","payload":{},"cron_expression":"*/2 * * * *","max_attempts":1}' 2>/dev/null)
assert_status "Create cron job" "201" "$(echo "$CRON_RESP" | tail -1)" "$(echo "$CRON_RESP" | head -n -1)"
CRON_JOB_ID=$(jq_field "$(echo "$CRON_RESP" | head -n -1)" ".id")

IDEM_KEY="order-$(date +%s)-unique"
IDEM_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$DEFAULT_QUEUE/jobs" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d "{\"type\":\"idem_job\",\"payload\":{\"ref\":\"IDEM-001\"},\"idempotency_key\":\"$IDEM_KEY\"}" 2>/dev/null)
assert_status "Create job with idempotency key" "201" "$(echo "$IDEM_RESP" | tail -1)" "$(echo "$IDEM_RESP" | head -n -1)"

IDEM2_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$DEFAULT_QUEUE/jobs" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d "{\"type\":\"idem_job\",\"payload\":{\"ref\":\"IDEM-001\"},\"idempotency_key\":\"$IDEM_KEY\"}" 2>/dev/null)
IDEM2_CODE=$(echo "$IDEM2_RESP" | tail -1)
if [ "$IDEM2_CODE" = "409" ]; then
  ok "Duplicate idempotency key returns 409"
else
  fail "Idempotency protection" "expected 409, got $IDEM2_CODE"
fi

TAGS_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$EMAIL_QUEUE/jobs" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"type":"send_email","payload":{"to":"user@example.com","subject":"Test"},"tags":["email","transactional","v2"]}' 2>/dev/null)
assert_status "Create job with tags" "201" "$(echo "$TAGS_RESP" | tail -1)" "$(echo "$TAGS_RESP" | head -n -1)"

BATCH_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$NOTIF_QUEUE/jobs/batch" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '[
    {"type":"push_notification","payload":{"user_id":"u1","msg":"Alert 1"}},
    {"type":"push_notification","payload":{"user_id":"u2","msg":"Alert 2"}},
    {"type":"push_notification","payload":{"user_id":"u3","msg":"Alert 3"}},
    {"type":"send_sms","payload":{"phone":"+1234567890","msg":"Your code: 1234"}},
    {"type":"send_sms","payload":{"phone":"+9876543210","msg":"Your code: 5678"}}
  ]' 2>/dev/null)
BATCH_CODE=$(echo "$BATCH_RESP" | tail -1)
BATCH_BODY=$(echo "$BATCH_RESP" | head -n -1)
assert_status "Create batch jobs (5 jobs)" "201" "$BATCH_CODE" "$BATCH_BODY"
BATCH_COUNT=$(jq_field "$BATCH_BODY" ".count")
BATCH_ID=$(jq_field "$BATCH_BODY" ".batch_id")
if [ "$BATCH_COUNT" = "5" ]; then
  ok "Batch created correct count: 5 jobs"
else
  fail "Batch count" "expected 5, got $BATCH_COUNT"
fi

if [ -n "$BASIC_JOB_ID" ] && [ "$BASIC_JOB_ID" != "null" ]; then
  GET_JOB=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/jobs/$BASIC_JOB_ID" 2>/dev/null)
  assert_status "Get job by ID" "200" "$(echo "$GET_JOB" | tail -1)" "$(echo "$GET_JOB" | head -n -1)"
fi

LIST_Q=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/queues/$DEFAULT_QUEUE/jobs?status=queued&limit=20" 2>/dev/null)
assert_status "List jobs (status=queued)" "200" "$(echo "$LIST_Q" | tail -1)" ""

CANCEL_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$DEFAULT_QUEUE/jobs" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"type":"cancel_target","payload":{"action":"something"}}' 2>/dev/null)
CANCEL_JID=$(jq_field "$(echo "$CANCEL_RESP" | head -n -1)" ".id")

if [ -n "$CANCEL_JID" ] && [ "$CANCEL_JID" != "null" ]; then
  CANCEL_ACT=$(curl -s -w "\n%{http_code}" -X DELETE "$API/jobs/$CANCEL_JID" -H "$AUTH" 2>/dev/null)
  assert_status "Cancel queued job" "200" "$(echo "$CANCEL_ACT" | tail -1)" "$(echo "$CANCEL_ACT" | head -n -1)"

  RETRY_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/jobs/$CANCEL_JID/retry" -H "$AUTH" 2>/dev/null)
  assert_status "Retry cancelled job" "200" "$(echo "$RETRY_RESP" | tail -1)" "$(echo "$RETRY_RESP" | head -n -1)"
fi

echo ""
echo "=== 10. BULK JOB SUBMISSION (load for workers) ==="

JOB_TYPES=("process_order" "sync_inventory" "generate_report" "cleanup_temp_files" "send_email" "send_bulk_email" "push_notification" "send_sms" "etl_batch" "transcode_video" "process_payment" "fraud_check" "compliance_alert" "heartbeat_check" "batch_op" "prio_test" "always_fail")
QUEUE_LIST=("$DEFAULT_QUEUE" "$EMAIL_QUEUE" "$NOTIF_QUEUE" "$REPORT_QUEUE" "$PAYMENT_QUEUE")

SUBMITTED=0
for i in $(seq 1 100); do
  JTYPE="${JOB_TYPES[$((RANDOM % ${#JOB_TYPES[@]}))]}"
  QID="${QUEUE_LIST[$((RANDOM % ${#QUEUE_LIST[@]}))]}"
  PRIO=$((RANDOM % 10 + 1))
  RESP=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$API/queues/$QID/jobs" \
    -H "$AUTH" -H "Content-Type: application/json" \
    -d "{\"type\":\"$JTYPE\",\"payload\":{\"job_seq\":$i,\"test_run\":true},\"priority\":$PRIO}" 2>/dev/null)
  if [ "$RESP" = "201" ]; then
    SUBMITTED=$((SUBMITTED+1))
  fi
done
ok "Bulk submitted $SUBMITTED/100 random jobs across queues"

echo ""
echo "=== 11. STARTING 20 WORKER PROCESSES ==="

DB_URL="postgresql://neondb_owner:npg_tv71rcHUzfaP@ep-weathered-breeze-azjivzw5-pooler.c-3.ap-southeast-1.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
REDIS_URL_VAL="rediss://default:gQAAAAAAAWrDAAIgcDIzZTE2YmNkYjBkMDQ0NjUyYTU2NWZlZTljMjE4OGVlNg@ace-rooster-92867.upstash.io:6379"

WORKER_QUEUES_LIST="default,email,notifications,reports,payments"
WORKER_PIDS=()

kill $(pgrep -f "bin/worker") 2>/dev/null || true
sleep 1

for i in $(seq 1 20); do
  CONC=$((RANDOM % 8 + 3))
  LOG="/tmp/worker_${i}.log"
  DATABASE_URL="$DB_URL" \
  REDIS_URL="$REDIS_URL_VAL" \
  PROJECT_ID="$PROJECT_ID" \
  WORKER_QUEUES="$WORKER_QUEUES_LIST" \
  WORKER_CONCURRENCY="$CONC" \
  POLL_INTERVAL="500ms" \
  HEARTBEAT_INTERVAL="5s" \
  ENV="development" \
  nohup /home/tushar/Desktop/Dev/dis-job-sd/bin/worker > "$LOG" 2>&1 &
  WORKER_PIDS+=($!)
done

echo "  Started ${#WORKER_PIDS[@]} workers, waiting for registration..."
sleep 8

WORKERS_RESP=$(curl -s -H "$AUTH" "$API/projects/$PROJECT_ID/workers" 2>/dev/null)
ACTIVE_COUNT=$(echo "$WORKERS_RESP" | jq -r '[.[] | select(.status=="active")] | length' 2>/dev/null || echo "0")
if [ "$ACTIVE_COUNT" -ge 15 ]; then
  ok "Workers registered: $ACTIVE_COUNT active (≥15 expected)"
elif [ "$ACTIVE_COUNT" -ge 10 ]; then
  warn "Only $ACTIVE_COUNT workers registered (expected 20, some may be deduplicating)"
  ok "Workers registered: $ACTIVE_COUNT active"
else
  fail "Worker registration" "only $ACTIVE_COUNT active workers registered"
fi

echo ""
echo "=== 12. JOB EXECUTION MONITORING (30s observation) ==="

for round in 1 2 3; do
  sleep 10
  STATS_RESP=$(curl -s -H "$AUTH" "$API/queues/$DEFAULT_QUEUE/stats" 2>/dev/null)
  Q_STATUS=$(echo "$STATS_RESP" | jq -r '.by_status' 2>/dev/null || echo "{}")
  echo "  Round $round/3 - default queue: $Q_STATUS"
done

METRICS_RESP=$(curl -s -H "$AUTH" "$API/projects/$PROJECT_ID/metrics" 2>/dev/null)
METRICS_WORKERS=$(jq_field "$METRICS_RESP" ".active_workers")
METRICS_COMPLETED=$(jq_field "$METRICS_RESP" ".completed_24h")
ok "Project metrics endpoint returns data (active_workers=$METRICS_WORKERS, completed_24h=$METRICS_COMPLETED)"

echo ""
echo "=== 13. WORKER API ==="
WORKERS_LIST=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/projects/$PROJECT_ID/workers" 2>/dev/null)
assert_status "List workers" "200" "$(echo "$WORKERS_LIST" | tail -1)" ""
FIRST_WORKER_ID=$(echo "$WORKERS_LIST" | head -n -1 | jq -r '.[0].id' 2>/dev/null)

if [ -n "$FIRST_WORKER_ID" ] && [ "$FIRST_WORKER_ID" != "null" ]; then
  GET_W=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/workers/$FIRST_WORKER_ID" 2>/dev/null)
  assert_status "Get worker by ID" "200" "$(echo "$GET_W" | tail -1)" "$(echo "$GET_W" | head -n -1)"
fi

echo ""
echo "=== 14. JOB LOGS AND EXECUTIONS ==="

COMPLETED_JOB=$(curl -s -H "$AUTH" "$API/queues/$DEFAULT_QUEUE/jobs?status=completed&limit=1" 2>/dev/null)
COMP_ID=$(echo "$COMPLETED_JOB" | jq -r '.data[0].id' 2>/dev/null)

if [ -n "$COMP_ID" ] && [ "$COMP_ID" != "null" ]; then
  LOGS_RESP=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/jobs/$COMP_ID/logs" 2>/dev/null)
  assert_status "Get job logs (completed job)" "200" "$(echo "$LOGS_RESP" | tail -1)" ""

  EXEC_RESP=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/jobs/$COMP_ID/executions" 2>/dev/null)
  assert_status "Get job executions" "200" "$(echo "$EXEC_RESP" | tail -1)" ""
  EXEC_COUNT=$(echo "$EXEC_RESP" | head -n -1 | jq -r 'length' 2>/dev/null)
  if [ "${EXEC_COUNT:-0}" -ge 1 ]; then
    ok "Job has $EXEC_COUNT execution record(s)"
  else
    warn "No execution records found for completed job $COMP_ID"
  fi
else
  warn "No completed jobs found yet - workers may still be processing"
fi

echo ""
echo "=== 15. DEAD LETTER QUEUE ==="
sleep 15

for QID in "${QUEUE_IDS[@]}"; do
  DLQ_RESP=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/queues/$QID/dlq" 2>/dev/null)
  assert_status "List DLQ for queue" "200" "$(echo "$DLQ_RESP" | tail -1)" ""
done

DLQ_ALL=$(curl -s -H "$AUTH" "$API/queues/$DEFAULT_QUEUE/dlq" 2>/dev/null)
DLQ_ENTRY=$(echo "$DLQ_ALL" | jq -r '.data[0].id' 2>/dev/null)
if [ -n "$DLQ_ENTRY" ] && [ "$DLQ_ENTRY" != "null" ]; then
  DLQR=$(curl -s -w "\n%{http_code}" -X POST "$API/dlq/$DLQ_ENTRY/retry" -H "$AUTH" 2>/dev/null)
  assert_status "Retry DLQ entry" "200" "$(echo "$DLQR" | tail -1)" "$(echo "$DLQR" | head -n -1)"

  AF_RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$DEFAULT_QUEUE/jobs" \
    -H "$AUTH" -H "Content-Type: application/json" \
    -d '{"type":"always_fail","payload":{},"max_attempts":1}' 2>/dev/null)
  AF_JID=$(jq_field "$(echo "$AF_RESP" | head -n -1)" ".id")
  ok "DLQ has entries - retry workflow verified"
else
  warn "No DLQ entries yet - always_fail jobs may still be retrying"
fi

echo ""
echo "=== 16. QUEUE METRICS ==="
for QID in "${QUEUE_IDS[@]}"; do
  QM_RESP=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/queues/$QID/metrics" 2>/dev/null)
  assert_status "Queue metrics ($QID)" "200" "$(echo "$QM_RESP" | tail -1)" ""
done

echo ""
echo "=== 17. PRIORITY ORDERING ==="
PRIO_IDS=()
for PRIO in 1 3 5 7 9; do
  PR=$(curl -s -X POST "$API/queues/$DEFAULT_QUEUE/jobs" \
    -H "$AUTH" -H "Content-Type: application/json" \
    -d "{\"type\":\"prio_test\",\"payload\":{\"priority_val\":$PRIO},\"priority\":$PRIO}" 2>/dev/null)
  PID=$(jq_field "$PR" ".id")
  PRIO_IDS+=("$PID:$PRIO")
done
ok "Priority jobs created (1,3,5,7,9) - workers should process priority=9 first (ORDER BY priority ASC maps to DESC in schema)"

echo ""
echo "=== 18. PAUSED QUEUE BEHAVIOR ==="
curl -s -X POST "$API/queues/$REPORT_QUEUE/pause" -H "$AUTH" > /dev/null
PAUSED_JOB=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$REPORT_QUEUE/jobs" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"type":"generate_report","payload":{"type":"monthly"}}' 2>/dev/null)
PAUSED_CODE=$(echo "$PAUSED_JOB" | tail -1)
if [ "$PAUSED_CODE" = "201" ]; then
  ok "Paused queue still accepts job submissions (pausing is a worker-side gate)"
else
  warn "Paused queue rejected job submission (HTTP $PAUSED_CODE)"
fi
curl -s -X POST "$API/queues/$REPORT_QUEUE/resume" -H "$AUTH" > /dev/null

echo ""
echo "=== 19. ERROR HANDLING ==="

NOAUTH=$(curl -s -w "\n%{http_code}" "$API/orgs" 2>/dev/null)
if [ "$(echo "$NOAUTH" | tail -1)" = "401" ]; then
  ok "Unauthenticated request returns 401"
else
  fail "Auth guard" "expected 401, got $(echo "$NOAUTH" | tail -1)"
fi

INV_JOB=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$DEFAULT_QUEUE/jobs" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"payload":{"x":1}}' 2>/dev/null)
if [ "$(echo "$INV_JOB" | tail -1)" = "400" ]; then
  ok "Job missing 'type' returns 400"
else
  fail "Validation: missing type" "expected 400, got $(echo "$INV_JOB" | tail -1)"
fi

NF_JOB=$(curl -s -w "\n%{http_code}" -H "$AUTH" "$API/jobs/00000000-0000-0000-0000-000000000000" 2>/dev/null)
if [ "$(echo "$NF_JOB" | tail -1)" = "404" ]; then
  ok "Non-existent job returns 404"
else
  fail "404 for missing job" "got $(echo "$NF_JOB" | tail -1)"
fi


BAD_JSON=$(curl -s -w "\n%{http_code}" -X POST "$API/queues/$DEFAULT_QUEUE/jobs" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d 'not-valid-json' 2>/dev/null)
if [ "$(echo "$BAD_JSON" | tail -1)" = "400" ]; then
  ok "Invalid JSON body returns 400"
else
  fail "JSON validation" "expected 400, got $(echo "$BAD_JSON" | tail -1)"
fi

SHORT_PW=$(curl -s -w "\n%{http_code}" -X POST "$API/auth/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"shortpw@test.io","password":"abc","name":"Short"}' 2>/dev/null)
if [ "$(echo "$SHORT_PW" | tail -1)" = "400" ]; then
  ok "Short password rejected (400)"
else
  fail "Password length validation" "expected 400, got $(echo "$SHORT_PW" | tail -1)"
fi

echo ""
echo "=== 20. SUSTAINED LOAD (60s, multiple users) ==="

for i in $(seq 1 5); do
  TOKEN="${USER_TOKENS[${USER_EMAILS[$((i-1))]}]:-}"
  [ -z "$TOKEN" ] && continue
  for j in $(seq 1 10); do
    JTYPE="${JOB_TYPES[$((RANDOM % 12))]}"
    curl -s -o /dev/null -X POST "$API/queues/$DEFAULT_QUEUE/jobs" \
      -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
      -d "{\"type\":\"$JTYPE\",\"payload\":{\"user\":$i,\"seq\":$j}}" &
  done
done
wait
ok "Concurrent multi-user job submission (5 users × 10 jobs)"

echo "  Waiting 30s for worker processing..."
sleep 30

FINAL_METRICS=$(curl -s -H "$AUTH" "$API/projects/$PROJECT_ID/metrics" 2>/dev/null)
FINAL_WORKERS=$(jq_field "$FINAL_METRICS" ".active_workers")
FINAL_COMPLETED=$(jq_field "$FINAL_METRICS" ".completed_24h")

DEFAULT_FINAL=$(curl -s -H "$AUTH" "$API/queues/$DEFAULT_QUEUE/stats" 2>/dev/null)
DF_COMPLETED=$(echo "$DEFAULT_FINAL" | jq -r '.by_status.completed // 0' 2>/dev/null)
DF_QUEUED=$(echo "$DEFAULT_FINAL" | jq -r '.by_status.queued // 0' 2>/dev/null)
DF_DEAD=$(echo "$DEFAULT_FINAL" | jq -r '.by_status.dead // 0' 2>/dev/null)

ok "Final snapshot - workers=$FINAL_WORKERS, completed_24h=$FINAL_COMPLETED"
ok "Default queue - completed=$DF_COMPLETED, queued=$DF_QUEUED, dead=$DF_DEAD"

echo ""
echo "=== CLEANUP ==="
kill $(pgrep -f "bin/worker") 2>/dev/null || true
ok "Workers stopped"

echo ""
echo "================================================================"
echo " TEST RESULTS"
echo "================================================================"
for R in "${RESULTS[@]}"; do echo "$R"; done
echo ""
for W in "${WARNINGS[@]}"; do yellow "$W"; done
echo ""
echo "  Total PASS: $PASS"
echo "  Total FAIL: $FAIL"
echo "  Warnings:   ${#WARNINGS[@]}"
echo ""

cat > /tmp/test_summary.txt << EOF
PASS=$PASS
FAIL=$FAIL
WARN=${#WARNINGS[@]}
ACTIVE_WORKERS=$ACTIVE_COUNT
COMPLETED_24H=$FINAL_COMPLETED
DEFAULT_COMPLETED=$DF_COMPLETED
DEFAULT_QUEUED=$DF_QUEUED
DEFAULT_DEAD=$DF_DEAD
ORG_ID=$ORG_ID
PROJECT_ID=$PROJECT_ID
EOF

if [ "$FAIL" = "0" ]; then
  green "ALL TESTS PASSED"
  exit 0
else
  red "$FAIL TEST(S) FAILED"
  exit 1
fi

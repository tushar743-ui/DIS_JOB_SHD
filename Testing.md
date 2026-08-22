# Testing Report — Distributed Job Scheduling System

**Date:** 2026-08-22  
**Test runs against:** Neon PostgreSQL (production pooler), Upstash Redis (TLS)  
**Go version:** see go.mod  
**Total tests:** 50 (11 unit + 34 API integration + 5 poller integration)  
**Overall result:** ALL PASS

---

## 1. Bugs Found and Fixed

### Bug 1 — Priority ordering reversed (critical)
**File:** `worker/internal/poller/poller.go`  
**Commit introduced:** m4  
**Symptom:** Low-priority jobs (priority=1) were processed before high-priority (priority=9).  
**Root cause:** `ORDER BY priority ASC` in the SQL claim query — opposite of intended.  
**Fix:** Changed to `ORDER BY priority DESC, run_at ASC`.  
**Verified by:** `TestPriorityOrdering` — processes 5 jobs in exact order [9, 7, 5, 3, 1].

### Bug 2 — Batch job scheduled status not set (correctness)
**File:** `api/internal/handler/job.go` — `CreateBatch`  
**Symptom:** Batch jobs with a future `scheduled_at` were inserted with `status='queued'` instead of `status='scheduled'`, so the scheduler would never promote them at the right time.  
**Root cause:** Single job `Create` had conditional status logic; `CreateBatch` hardcoded `'queued'`.  
**Fix:** Added `batchStatus` variable that becomes `'scheduled'` when `scheduled_at > now()`.  
**Verified by:** `TestBatch_ScheduledStatus` in the API integration suite (batch with future `scheduled_at` returns `status=scheduled`).

---

## 2. Unit Tests — `worker/internal/executor`

No database or network required. All 11 tests run in **~61ms**.

| Test | What it verifies | Result |
|------|-----------------|--------|
| `TestCalcDelay_NoPolicy_ExponentialBackoff` | Default backoff grows exponentially, capped at 5 min | PASS |
| `TestCalcDelay_Fixed` | Fixed strategy always returns `InitialDelayMs` | PASS |
| `TestCalcDelay_Linear` | Linear grows by `initial × attempt`, caps at `MaxDelayMs` | PASS |
| `TestCalcDelay_Exponential` | Doubles each attempt with multiplier, caps at `MaxDelayMs` | PASS |
| `TestCalcDelay_DefaultFallback_CapAt5min` | 30 attempts never exceed 5-minute cap | PASS |
| `TestCalcDelay_UnknownStrategy_FallsBackToExponential` | Unknown strategy falls to exponential branch, delay grows | PASS |
| `TestRegister_And_Lookup` | Handler registered and callable | PASS |
| `TestRegister_Overwrite` | Second registration replaces first | PASS |
| `TestSemChan_RespectsCapacity` | Semaphore channel has correct buffer size | PASS |
| `TestDrain_ReturnsWhenDone` | Drain returns immediately with no running jobs | PASS |
| `TestDrain_RespectsContextTimeout` | Drain respects context timeout with stuck job | PASS |

**Run command:**
```
go test -v ./worker/internal/executor/...
```

---

## 3. API Integration Tests — `api/internal/handler`

Real Neon DB + Upstash Redis. Full HTTP router via `httptest.NewServer`. All 34 tests run in **~124 seconds**.

`TestMain` setup: creates admin user → org → project → default queue (cascading DELETE on teardown).

### Auth Tests (11)
| Test | Result |
|------|--------|
| Register new user | PASS |
| Duplicate email returns 409 | PASS |
| Short password returns 400 | PASS |
| Missing fields returns 400 | PASS |
| Login success | PASS |
| Wrong password returns 401 | PASS |
| Refresh token rotation (old token invalidated) | PASS |
| Replay protection (reused refresh token blocked) | PASS |
| `/me` returns user profile | PASS |
| No token → 401 | PASS |
| Invalid token → 401 | PASS |

### Org / Member Tests (2)
| Test | Result |
|------|--------|
| Org CRUD (create, get, update, list) | PASS |
| Add member to org | PASS |

### Project Tests (2)
| Test | Result |
|------|--------|
| Project CRUD | PASS |
| Duplicate project name returns 409 | PASS |

### Retry Policy Tests (2)
| Test | Result |
|------|--------|
| Create retry policy | PASS |
| List retry policies | PASS |

### Queue Tests (4)
| Test | Result |
|------|--------|
| Queue CRUD | PASS |
| Pause queue | PASS |
| Resume queue | PASS |
| Duplicate queue name returns 409 | PASS |

### Job Tests (15)
| Test | Result |
|------|--------|
| Create basic job | PASS |
| Create scheduled job (future `scheduled_at` → `status=scheduled`) | PASS |
| Idempotency key dedup returns 409 | PASS |
| Missing job type returns 400 | PASS |
| Invalid JSON payload returns 400 | PASS |
| Get job by ID | PASS |
| Get nonexistent job returns 404 | PASS |
| List jobs with status filter | PASS |
| Cancel queued job | PASS |
| Cancel running job returns 409 | PASS |
| Retry from cancelled | PASS |
| Retry already-queued job returns 409 | PASS |
| Batch create 10 jobs | PASS |
| Batch with future `scheduled_at` returns `status=scheduled` (Bug 2 fix) | PASS |
| Batch >1000 jobs returns 400 | PASS |

### Observability Tests (2)
| Test | Result |
|------|--------|
| Job logs | PASS |
| Job executions | PASS |

### Metrics / Worker Tests (4)
| Test | Result |
|------|--------|
| Project metrics | PASS |
| Queue metrics | PASS |
| Worker list | PASS |
| Worker list with status filter | PASS |

### DLQ Test (1)
| Test | Result |
|------|--------|
| DLQ list | PASS |

### Multi-tenant / Load Tests (2)
| Test | Result |
|------|--------|
| Multi-user org isolation (user A cannot access user B's org) | PASS |
| Load test: 10 users × 10 concurrent jobs | PASS |

**Run command:**
```
DATABASE_URL="..." REDIS_URL="..." JWT_SECRET="..." ENV=development \
go test -v -tags integration -timeout 180s ./api/internal/handler/...
```

---

## 4. Poller Integration Tests — `worker/internal/poller`

Real Neon DB. All 5 tests + load test run in ~161 seconds total.

### Core Tests (5)

| Test | Duration | Result | Details |
|------|----------|--------|---------|
| `TestPriorityOrdering` | 14.7s | PASS | Processed order: [9,7,5,3,1] — strictly descending ✓ |
| `TestSkipLocked_NoDuplicateClaims` | 19.1s | PASS | 20 jobs, 5 concurrent workers, 0 double-claims ✓ |
| `TestScheduler_PromotesScheduledJobs` | 7.7s | PASS | Job promoted from `scheduled` → `queued` within 8s ✓ |
| `TestFailure_MovesToDLQ` | 11.6s | PASS | `max_attempts=2` job moves to `dead` + DLQ entry ✓ |
| `TestPausedQueue_JobsNotClaimed` | 5.9s | PASS | Paused queue jobs stay `queued` after 3s ✓ |

### Load Test

| Metric | Value |
|--------|-------|
| Jobs | 200 |
| Workers | 20 |
| Concurrency per worker | 3–7 (random) |
| Job types | 8 (load_order, load_email, load_notify, load_report, load_payment, load_sync, load_cleanup, load_fraud) |
| Job handler delay | 5–20ms (random) |
| Completed | 200 / 200 (100%) |
| Dead | 0 |
| Processing time | **11 seconds** |
| Total test duration | ~101s (includes pool setup, worker goroutine teardown) |
| DB pool size | 40 connections (MaxConns) |

**Run command:**
```
DATABASE_URL="..." PROJECT_ID="..." WORKER_QUEUES="default,email,notifications" \
WORKER_CONCURRENCY="5" POLL_INTERVAL="200ms" ENV=development \
go test -v -tags integration -timeout 150s ./worker/internal/poller/...
```

---

## 5. Architecture Observations

### What works well
- `FOR UPDATE SKIP LOCKED` correctly prevents any double-claiming across concurrent workers (0 violations in 5-worker × 20-job test).
- Priority-based ordering is deterministic with `ORDER BY priority DESC, run_at ASC` and concurrency=1.
- DLQ flow: job retries exactly `max_attempts` times, then lands in `dead_letter_queue` with `status='dead'`.
- Scheduler promotes `scheduled` jobs to `queued` within one tick (5s interval).
- JWT refresh token rotation + replay protection work correctly — reused tokens are blocked.
- Multi-tenant isolation holds: org membership gates all resource access.

### Known limitations
- `TestPriorityOrdering` uses a temporary isolated queue to avoid interference from stale DB jobs. In production, priority ordering is correct but may be interleaved with jobs of the same priority from concurrent workers.
- The scheduler polls every 5 seconds (hardcoded). For sub-5s scheduled jobs, there's up to 5s of promotion lag.
- Worker heartbeat is not tested end-to-end (requires a live worker process).

---

## 6. Demo Users Created

The API integration `TestMain` creates one admin user per test run and deletes it on teardown. The following persistent demo user exists:

| Email | Password | Org ID | API Key |
|-------|----------|--------|---------|
| tusharpatle743@gmail.com | password123 | b6185011-7de4-4a47-b3f3-fc0a9a0e001a | `djq_f9b7b6703e8b282e951b40d5206d69f4af9659cdccae0c5cc7751a75a27f4c92` |

---

## 7. Test File Locations

| File | Type | Count |
|------|------|-------|
| `worker/internal/executor/executor_test.go` | Unit (no build tag) | 11 |
| `api/internal/handler/integration_test.go` | Integration (`//go:build integration`) | 34 |
| `worker/internal/poller/poller_test.go` | Integration (`//go:build integration`) | 5 + load |

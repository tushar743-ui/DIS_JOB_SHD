aim is -
Objective
Design and build a production-inspired distributed job scheduling platform capable of reliably executing
asynchronous background jobs across multiple workers. The project evaluates backend engineering, database
design, concurrency, reliability, API design, and full-stack implementation rather than feature count.

Core Requirements
 Implement authentication and project management. Each project can own multiple job queues.
 Support queue configuration including priority, concurrency limits, retry policy, pause/resume, and
statistics.
 Allow users to create immediate, delayed, scheduled, recurring (cron), and batch jobs through REST APIs.
 Build a worker service that polls queues, atomically claims jobs, executes them concurrently, sends
heartbeats, and supports graceful shutdown.
 Implement the complete job lifecycle: Queued → Scheduled → Claimed → Running → Completed, with
retries and Dead Letter Queue support for permanent failures.
 Support configurable retry strategies such as fixed delay, linear backoff, and exponential backoff.
 Maintain execution logs, retry history, worker assignment, timestamps, and execution metrics for every
job.
 Create a web dashboard to manage queues, inspect jobs, monitor workers, retry failed jobs, and visualize
throughput and system health.
Database Design
Design an efficient relational schema for Users, Organizations, Projects, Queues, Jobs, Job Executions, Retry
Policies, Workers, Worker Heartbeats, Job Logs, Scheduled Jobs, and Dead Letter Queue entries. Explain
primary keys, foreign keys, indexes, normalization, cascading behavior, and performance considerations.
Backend Expectations
Expose clean REST APIs with validation, authentication, pagination, filtering, structured error handling, and
logging. Ensure jobs are claimed atomically to prevent duplicate execution and make execution idempotent
wherever appropriate.
Frontend Expectations
Develop a responsive dashboard showing queue health, worker status, job explorer, execution logs, queue
configuration, and metrics. Live updates may be implemented using polling or WebSockets.
Bonus Features
 Workflow dependencies
 Rate limiting
 Distributed locking
 Queue sharding
 Event-driven execution
 WebSocket live updates
 Role-based access control

 AI-generated failure summaries
Deliverables

 Source code with setup instructions
 Architecture diagram
 ER diagram
 API documentation
 Design decisions document describing major trade-offs
 Automated tests for critical functionality
Evaluation Criteria Marks
System Architecture 20
Database Design 20
Backend Engineering 20
Reliability &amp; Concurrency 15
Frontend &amp; UX 10
API Design 5
Documentation 5
Testing 5
Evaluation will prioritize engineering quality, modular architecture, database design, reliability, concurrency
handling, observability, documentation, and maintainability over simply implementing the largest number of
features.

always ise /remove-ai-marks skill as compulsory
IN the coplete process we have keep in mind that we are a senior top level software developer and we are building a production level system, it should have all the necessary features and should be scalable, maintainable, and reliable, it should not have any flaws,




and donot add comments in the code (anywhere)

tech stack -
Frontend        Next.js 14 App Router · TypeScript · Tailwind · shadcn/ui · Recharts
API Gateway     Go · Chi · JWT middleware · rate limiter (Redis)
Services        Go · sqlc · golang-migrate · zerolog · swaggo
Primary DB      Neon Postgres (branching, pooled, autoscale)
Cache / Lock    Upstash Redis (rate limit · pub/sub · distributed lock)
Object Store    Cloudflare R2 or S3 (job execution logs, large payloads)
Testing         Go table-driven tests · testcontainers-go (real Postgres in CI)
API Docs        swaggo/swag (auto-generated OpenAPI from handler annotations)



always use the following guidelines before writing code - 
1. Does this need to exist?   → no: skip it
2. Already in this codebase?  → reuse it, don't rewrite
3. Stdlib does it?            → use it
4. Native platform feature?   → use it
5. Installed dependency?      → use it
6. One line?                  → one line
7. Only then: the minimum that works



.todos after the completrion of project and verified testing -
remove all the comments from the codebase
remove any trace of the .claude folder and skills from the codebase
revome any trace of ai and specially those half line filled texts like in liscence- make them full line



---

## Work Completed (session log — append-only)

### Session 1 — Core build + initial commits (m1–m4)
- Full project scaffold: Go workspace (`go.work`) with two modules — `api/` (Chi router, JWT, pgxpool) and `worker/` (poller, executor, scheduler).
- Neon Postgres schema: Users, Organizations, Members, Projects, Queues, Jobs, Job Executions, Retry Policies, Workers, Worker Heartbeats, Job Logs, Scheduled Jobs, Dead Letter Queue — all with proper FKs, indexes, cascade rules.
- Upstash Redis wired for rate limiting (sliding-window, 200 req/min per IP).
- JWT auth: short-lived access tokens + PostgreSQL-persisted refresh tokens with rotation and replay protection.
- Multi-tenant hierarchy enforced server-side: Org → Project → Queue → Job.
- REST API handlers: auth (register/login/refresh/me), orgs, members, projects, retry policies, queues (CRUD + pause/resume), jobs (create/get/list/cancel/retry/batch/logs/executions), metrics, workers, DLQ.
- Worker service: poller loop (`FOR UPDATE SKIP LOCKED`), executor with semaphore concurrency, scheduler goroutine (promotes `scheduled` → `queued` every 5s), heartbeat sender, graceful shutdown via `Drain`.
- Retry strategies implemented: fixed, linear, exponential backoff — all capped at configurable `MaxDelayMs`.
- Cron job support: `cron_expression` on jobs, `next_run_at` promoted by scheduler via `robfig/cron`.
- Binaries: `bin/api` and `bin/worker`.

### Session 2 — Bug fixes + comprehensive test suite

#### Bugs fixed
1. **Priority ordering reversed** (`worker/internal/poller/poller.go`)
   - Commit m4 changed `ORDER BY priority DESC` → `ORDER BY priority ASC`, causing low-priority jobs to run first.
   - Fixed: `ORDER BY priority DESC, run_at ASC`.

2. **Batch job status not set** (`api/internal/handler/job.go` — `CreateBatch`)
   - Batch endpoint hardcoded `status='queued'` even when `scheduled_at` was in the future.
   - Fixed: added `batchStatus` variable — becomes `'scheduled'` when `scheduled_at > now()`, matching the single-job `Create` logic.

#### Test files created
Three `_test.go` files cover the entire critical path:

**`worker/internal/executor/executor_test.go`** — pure unit tests (no build tag)
- 11 tests, ~61ms, zero DB dependency.
- Covers: all retry delay strategies (fixed/linear/exponential/unknown/no-policy), 5-minute cap enforcement, handler registration/overwrite, semaphore capacity, Drain return behavior, Drain context timeout.

**`api/internal/handler/integration_test.go`** — `//go:build integration`
- 34 tests, ~124s, real Neon DB + Upstash Redis via `httptest.NewServer`.
- `TestMain` creates org/project/queue fixtures; cascading DELETE on teardown.
- Covers: full auth flow (register, login, refresh rotation, replay protection, /me, 401 guards), org CRUD, member add, project CRUD + 409 duplicate, retry policy create/list, queue CRUD + pause/resume + 409, job create/scheduled/idempotency/validation/get/list/cancel/retry/batch/scheduled-batch/logs/executions, metrics, workers, DLQ, multi-tenant isolation, 10-user load test.

**`worker/internal/poller/poller_test.go`** — `//go:build integration`
- 5 core tests + 1 load test, ~161s total, real Neon DB with `MaxConns=40`.
- `TestPriorityOrdering`: creates an isolated queue (no competing jobs), inserts 5 jobs with priorities [1,3,5,7,9], verifies processing order is [9,7,5,3,1] with concurrency=1.
- `TestSkipLocked_NoDuplicateClaims`: 5 workers × 20 jobs, `sync.Map` verifies zero double-claims.
- `TestScheduler_PromotesScheduledJobs`: job with `run_at=now()+1s` promoted to `queued` within 8s.
- `TestFailure_MovesToDLQ`: `max_attempts=2` always-fail handler → `status='dead'` + DLQ entry.
- `TestPausedQueue_JobsNotClaimed`: paused queue jobs stay `queued` after 3s of polling.
- `TestLoad_20Workers_200Jobs`: 200 jobs × 20 workers (concurrency 3–7 each) → **200/200 completed in 11s**, 0 dead.

#### Test results summary
| Suite | Tests | Result | Duration |
|-------|-------|--------|----------|
| Executor unit | 11 | ALL PASS | 61ms |
| API integration | 34 | ALL PASS | ~124s |
| Poller integration | 5 + load | ALL PASS | ~161s |
| **Total** | **50+** | **ALL PASS** | — |

#### Test design decisions
- Priority test uses an isolated temporary queue (created/deleted per run) so stale DB jobs from other runs don't compete.
- Load test uses `load_*` job type prefix to avoid handler name collisions with non-test job types.
- `TestMain` pool config: `pgxpool.ParseConfig` with `MaxConns=40` / `MinConns=5` to prevent pool exhaustion under 20 concurrent workers.

#### Full results documented in `Testing.md`

### Current state
- All 50 tests pass against production Neon DB + Upstash Redis.
- Both binaries rebuilt clean with bug fixes applied.
- `Testing.md` written with complete results, run commands, architecture observations.
- Remaining work: web frontend verification (Next.js on port 3000 returns "Cannot GET /"), additional demo user creation if needed, and the post-completion todos (remove comments, remove .claude traces, fix license text). 
### Session 3 — Bonus features (workflows, locking, sharding, events, WS, RBAC, AI summaries)

#### New shared module
Added a third Go module `shared/` (wired into `go.work`, plus `require` + `replace` in `api/go.mod` and `worker/go.mod` so each module still builds standalone). It holds the three contracts that must never drift between API and worker:
- `shared/events` — event type constants, `Event` struct, `Publisher` (fire-and-forget, survives a dead broker, uses `context.WithoutCancel` so lifecycle events outlive the request), `Subscribe`, and `Event.Wakes()` which decides whether an event means a worker may have new claimable work. Channels are per project (`djq:events:<projectID>`) so tenants cannot see each other's stream.
- `shared/lock` — Redis distributed lock: `SET NX PX`, Lua CAS release/refresh (a stale holder can never delete a successor's lock), monotonic fencing tokens via `INCR`, and `Guard` for long-lived leadership with auto-renewal that cancels the guarded context the moment the lease is lost. `GuardOptions.OnUnavailable` fires when Redis itself is unreachable so callers can run degraded instead of stalling.
- `shared/shard` — rendezvous (HRW) shard ownership plus a Redis ZSET membership registry with TTL pruning.

#### Migrations
Split into two files because Postgres forbids *using* a new enum value in the same transaction that adds it (golang-migrate wraps each file in one tx):
- `003_job_status_blocked` — `ALTER TYPE job_status ADD VALUE 'blocked'` alone.
- `004_workflows_sharding_rbac` — `job_dependencies`, `job_failure_summaries`, `queues.shard_count`, `jobs.shard` + `jobs.partition_key`, `viewer` added to the member role CHECK, and reworked partial indexes (`idx_jobs_poll` now keys on `(queue_id, shard, run_at, priority DESC) WHERE status='queued'`, plus new `idx_jobs_scheduled`, `idx_jobs_blocked`, `idx_jobs_reclaim`).

Both applied to Neon (version 4). Note: **migrations must use the direct endpoint, not the pooler** — strip `-pooler` from the host. PgBouncer transaction mode does not support the advisory locks golang-migrate takes, and fails with `unnamed prepared statement does not exist`.

#### Feature notes
- **Workflow dependencies** — DAG per job via `depends_on`, and intra-batch DAGs via a batch-local `ref` (one API call defines a whole workflow). Cycles are caught by Kahn's algorithm in `api/internal/workflow` before anything is written, and the error names the cycle path. Dependencies on a `dead`/`cancelled` job are rejected 409 at creation rather than silently blocking forever. Correctness does **not** rest on the `blocked` status: the poller's claim predicate itself carries `NOT EXISTS (unsatisfied dependency)`, so a job can never run early even if the status bookkeeping lags. Completion unblocks direct dependents immediately (fast path, in the executor) and the scheduler sweep re-checks everything (durable fallback). A blocked job whose upstream died permanently is swept to `cancelled` with an explanatory `last_error`.
- **Distributed locking** — used for scheduler leadership (`djq:scheduler:<projectID>`), so only one worker runs the promote/cron/unblock/reclaim sweeps. Every sweep statement is idempotent, so when Redis is unreachable `OnUnavailable` deliberately runs the sweep **unguarded** on every worker: availability is preserved and correctness never depended on the lock. Also used to dedupe concurrent AI summary generation, where duplicate work costs real money.
- **Queue sharding** — `shard_count` is per queue, 1–64, default 1. `shard_count = 1` means sharding is off for that queue and any worker may claim (this preserves the old behaviour; exclusive ownership with one shard would pin a whole queue to one worker). Shards are assigned in SQL at insert time, atomically with the row, using `mod(hashtext(coalesce(partition_key, id::text)) & 2147483647, q.shard_count)` — the `& 2147483647` avoids the `abs(int4min)` overflow. A `partition_key` gives per-key ordering affinity. Workers claim with `queue_id = ANY(unsharded) OR (queue_id = ANY(sharded) AND shard = ANY(owned))`. If the Redis membership registry is unavailable, a worker claims **every** shard rather than none — `FOR UPDATE SKIP LOCKED` still guarantees no duplicate execution, so sharding is a contention optimisation, never a correctness mechanism.
- **Event-driven execution** — the poller now selects over its ticker *and* a coalescing kick channel fed by the Redis subscription, so enqueue-to-pickup is milliseconds instead of up to `POLL_INTERVAL`. The ticker stays as the safety net; a dropped event costs latency, never a lost job. The poller also re-nudges itself when a poll fills every free slot, and re-resolves topology on queue pause/resume events.
- **WebSocket live updates** — `GET /api/v1/projects/{projectID}/events`. Browsers cannot set headers on a WS handshake, so `middleware.Auth` accepts `?token=` **only** when `Upgrade: websocket` is present, and the request logger redacts that parameter. The hub keeps one Redis subscription per project, created on first subscriber and torn down with the last. Per-client send buffers are bounded; a client that cannot keep up is dropped rather than growing memory.
- **RBAC** — this closed a real hole. Before this session `/jobs/{jobID}`, `/queues/{queueID}`, `/workers/{workerID}` and the DLQ routes had **no ownership check at all**, so any authenticated user could read or mutate any job in the system by ID. `api/internal/authz` now resolves the owning org from whichever resource parameter the route carries (one indexed query joining `organization_members`) and enforces a four-role hierarchy `viewer < member < admin < owner`. The whole policy is visible as one auditable table in `router.go` via `r.With(role)`. Non-members get **404, not 403**, so the API never confirms that a resource exists. Unknown or empty roles fail closed. Also added: an admin cannot grant or remove a role at or above its own, and the last owner of an org cannot be removed.
- **AI failure summaries** — `POST/GET /api/v1/jobs/{jobID}/failure-summary`, Anthropic Go SDK, `claude-opus-5`, `output_config.effort = low` with a JSON-schema-constrained response so `category`/`confidence` can never drift from the DB CHECK constraints. Results are cached in `job_failure_summaries` keyed by a SHA-256 fingerprint of the rendered evidence + model, so identical evidence never pays twice and a `GET` reports `stale: true` once the evidence has moved on. Guarded by a distributed lock (409 if generation is already in flight) and a per-project hourly quota in Redis (429). With no `ANTHROPIC_API_KEY` the endpoint returns 503 and `GET /api/v1/features` advertises `ai_failure_summaries: false` so the UI can hide it.

#### Bugs found and fixed
1. **HRW shard ownership was badly skewed** (`shared/shard`) — scoring with bare FNV-1a made the ordering dominated by the member-name prefix, so one worker won nearly every shard. Fixed by running the hash through a splitmix64 finalizer (`mix(fnv(member) ^ mix(shard + golden))`). Caught by the exactly-once coverage test.
2. **Tie-break on a zero hash** (`shared/shard`) — the `best == ""` case was unreachable, so a score of exactly 0 for the first candidate left no owner. Fixed.
3. **Queue resume was not picked up** (`worker/internal/poller`) — queues were resolved once at startup and only re-resolved when the list was empty, so resuming a paused queue never took effect until restart. Now refreshed on a ticker and on `queue.resumed` events.
4. Scheduler sweeps were global rather than project-scoped; a worker was doing other tenants' work. Now scoped via `queue_id IN (SELECT id FROM queues WHERE project_id = $1)`.
5. `DLQHandler.Retry` ignored `Begin`/`Exec`/`Commit` errors and could report success after a failed write. Now fully checked.
6. Error response bodies were inconsistent (`http.Error` appends a newline, `json.NewEncoder` also does, hand-written writes did not). All error writers are now newline-terminated.

#### Other changes
- `router.New` now takes a `router.Deps` struct (config, pool, redis, hub, bus).
- `executor.SemChan()` (leaked the raw semaphore) replaced with `FreeSlots()` / `Running()`.
- `poller.ResolveQueueIDs` replaced with `RefreshTopology(ctx) error`.
- Worker now marks itself `draining` before `Drain` and leaves the shard registry on shutdown; publishes `worker.online` / `worker.offline`.
- API server: `ReadHeaderTimeout` replaces `ReadTimeout` and `WriteTimeout` is disabled, because a fixed write timeout kills long-lived WebSocket connections.
- Config gained `CORS_ORIGINS`, `RATE_LIMIT`, `RATE_LIMIT_WINDOW`, `ANTHROPIC_API_KEY`, `AI_SUMMARY_MODEL`. CORS is now an allowlist rather than always `*`.
- Rate limiter emits `X-RateLimit-*` headers and a numeric `Retry-After`.

#### Tests added this session
| Suite | Tests | Kind |
|-------|-------|------|
| `shared/shard` | 11 | unit, no dependencies |
| `shared/lock` | 8 | unit, miniredis |
| `shared/events` | 7 | unit, miniredis |
| `api/internal/workflow` | 13 | unit, pure |
| `api/internal/authz` | 8 | unit, pure |
| `api/internal/ai` | 17 | unit, stub HTTP server |
| `api/internal/middleware` | +5 | unit |
| `features_integration_test.go` | 18 | integration (`-tags integration`) |
| `rbac_integration_test.go` | 9 | integration (`-tags integration`) |

All unit tests pass; both binaries build clean; `go vet` clean including `-tags integration`.

Test design note: `shared/shard` asserts what HRW actually guarantees — exactly-once coverage, determinism, order-independence, minimal reshuffling on membership change, and unbiasedness averaged over many clusters. It deliberately does **not** assert tight per-worker balance, which HRW does not provide (at 64 shards over 8 workers σ≈2.65, so a worker holding 3 shards is normal variance, not a bug). Operational guidance: set `shard_count` well above the worker count for good balance.

### Remaining work
1. **Frontend for the new features** (largest remaining item) — a `useLiveEvents(projectId)` hook that opens the WebSocket and revalidates the affected SWR keys, so polling in `web/hooks/use-data.ts` becomes the fallback rather than the primary path; a live/polling indicator in `top-bar.tsx`; a dependency graph (upstream/downstream/blocked-by) on the job detail page; an AI failure-summary card on failed and dead jobs, gated on `GET /api/v1/features`; `shard_count` and `partition_key` in the queue config sheet and create-job dialog. Must match the existing theme — neutral OKLCH tokens in `web/app/globals.css` with the semantic `--state-*` / `--status-*` colours and the `STATUS_COLOR` / `STATUS_DOT` maps in `web/lib/status.ts`. Web types in `web/lib/api.ts` also need the new fields (`shard`, `partition_key`, `depends_on`, `blocked` status, `shard_count`).
2. **Run the integration suites against the live DB** — the 27 new integration tests are written and vet-clean but have not been executed yet: `make test-integration` (needs `DATABASE_URL` + `REDIS_URL`). Also worth adding a worker-side integration test that two workers on a sharded queue never double-claim, and that enqueue-to-running latency is well under `POLL_INTERVAL` with events enabled.
3. **Docs and cleanup** — README/API docs for the new endpoints (`/features`, `/jobs/{id}/dependencies`, `/jobs/{id}/failure-summary`, `/projects/{id}/events`), the RBAC permission matrix, updated ER diagram for `job_dependencies` + `job_failure_summaries`, and a design-decisions document. The rationale for these features lives in docs rather than in code, because `claude.md` requires the codebase stay comment-free. Then the post-completion todos: strip remaining comments, remove `.claude`/skills traces, fix the LICENSE half-lines, and the compulsory `/remove-ai-marks` pass.













Backend for all 8 features is implemented, building clean, migrated against the live Neon DB, and covered by 60+ passing unit tests:

- shared/ module (new) — event contract + Redis pub/sub, distributed lock (SET NX + Lua CAS release, auto-renewal, lost-lock cancellation, fencing tokens, OnUnavailable degraded mode), rendezvous-hash shard ownership.
- Workflow dependencies — job_dependencies table, blocked status, intra-batch DAG via ref/depends_on with cycle detection, immediate dependent unblocking on completion plus a sweep as durable fallback. Correctness is enforced in the claim predicate, not just the status field.
- RBAC — closed a real hole: /jobs/{id}, /queues/{id}, /workers/{id} had zero tenant checks, so any authenticated user could read any job in the system. Now a 4-role hierarchy with per-route policy visible in the router, 404 (not 403) for non-members, and privilege-escalation guards.
- Sharding / event-driven / locking / WS / AI summaries — all wired end-to-end.

Two real bugs found and fixed while testing: FNV-1a alone has weak avalanche, so shard ownership was dominated by the worker-name prefix (one worker would win nearly every shard); and Neon's pooled endpoint can't run migrations (needs the direct endpoint).
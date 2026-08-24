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
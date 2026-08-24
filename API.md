# API Documentation

Complete reference for the dis-job-queue REST and WebSocket API.

- **Base URL:** `http://localhost:8080` (development) - all versioned routes are mounted under `/api/v1`
- **Format:** JSON request and response bodies (`Content-Type: application/json`)
- **Authentication:** Bearer JWT, obtained from `/api/v1/auth/login` or `/api/v1/auth/register`
- **Interactive spec:** the API additionally exposes an OpenAPI document generated from handler annotations via `swaggo/swag` (see `Makefile` target `swagger`)

---

## Table of Contents

1. [Conventions](#conventions)
2. [Authentication](#authentication)
3. [Authorization (RBAC)](#authorization-rbac)
4. [Rate Limiting](#rate-limiting)
5. [Errors](#errors)
6. [System](#system)
7. [Auth](#auth-endpoints)
8. [Organizations](#organizations)
9. [Projects](#projects)
10. [Retry Policies](#retry-policies)
11. [Queues](#queues)
12. [Jobs](#jobs)
13. [Failure Summaries (AI)](#failure-summaries-ai)
14. [Workers](#workers)
15. [Dead Letter Queue](#dead-letter-queue)
16. [Metrics](#metrics)
17. [Live Events (WebSocket)](#live-events-websocket)

---

## Conventions

### Resource hierarchy

Every resource is scoped to a strict multi-tenant chain:

```
Organization → Project → Queue → Job
```

A user's access to any resource is derived from their membership in the owning **Organization**, resolved server-side on every request - never trusted from the client.

### Pagination

List endpoints that can grow unbounded (`GET /queues/{queueID}/jobs`, `GET /queues/{queueID}/dlq`) accept:

| Parameter | Default | Max | Description |
|---|---|---|---|
| `limit` | `20` | `100` | Rows per page |
| `offset` | `0` | - | Rows to skip |

Response envelope:

```json
{
  "data": [ ... ],
  "total": 214,
  "limit": 20,
  "offset": 0
}
```

### Timestamps

All timestamps are RFC 3339 / ISO 8601 in UTC (Go's `time.Time` JSON encoding), e.g. `"2026-08-25T09:41:12.384Z"`.

### IDs

All resource identifiers are UUIDv4 strings, generated server-side (`gen_random_uuid()` in Postgres or `uuid.New()` in Go).

---

## Authentication

Two token types are issued together on register/login/refresh:

| Token | Lifetime | Storage | Purpose |
|---|---|---|---|
| **Access token** (JWT, HS256) | `JWT_EXPIRY` (default 15m) | Stateless, held by the client | Sent as `Authorization: Bearer <token>` on every protected request |
| **Refresh token** (256-bit random, SHA-256 hashed at rest) | `REFRESH_EXPIRY` (default 168h) | Persisted in `refresh_tokens`, revocable server-side | Exchanged at `/auth/refresh` for a new pair (rotation - the old refresh token is revoked the moment it is used) |

The access token JWT carries only `uid` (user ID) and `email` as custom claims; it is intentionally **not** scoped to an organization or role - every organization-scoped route re-resolves the caller's role from `organization_members` on each request (see [Authorization](#authorization-rbac)), so a revoked membership takes effect immediately without waiting for token expiry.

WebSocket connections cannot set an `Authorization` header during the handshake, so `GET /api/v1/projects/{projectID}/events` additionally accepts the token as `?token=<jwt>` - this fallback is only honored when the request carries an `Upgrade: websocket` header, and the query parameter is redacted from access logs.

---

## Authorization (RBAC)

Every organization-scoped route (everything except `/auth/*`, `/health`, and `/features`) is guarded by a role check resolved from whichever path parameter the route carries (`orgID`, `projectID`, `queueID`, `jobID`, `workerID`, or `dlqID`) via a single indexed join back to `organization_members`.

Four roles, strictly ordered:

```
viewer  <  member  <  admin  <  owner
```

| Role | Can do |
|---|---|
| **viewer** | Read-only: list/get orgs, projects, queues, jobs, workers, metrics, logs, executions, dependencies |
| **member** | Everything a viewer can, plus: create jobs/batches, cancel/retry jobs, pause/resume queues, retry/requeue DLQ entries, generate AI failure summaries |
| **admin** | Everything a member can, plus: create/update/delete queues and projects, manage retry policies, add organization members up to their own role, purge jobs, discard DLQ entries |
| **owner** | Everything an admin can, plus: delete organizations/projects, rotate project API keys, remove any member (subject to the last-owner guard below) |

**Design notes:**

- **404, not 403, for non-members.** A user with no membership in the owning organization receives `404 Not Found` for any resource under it - the API never confirms that a resource exists to a caller who isn't authorized to know that.
- **Fail closed.** An unknown or empty role never satisfies any `Require(...)` check.
- **No privilege escalation.** `POST /orgs/{orgID}/members` rejects granting a role higher than the caller's own role, and rejects modifying a member whose current role already exceeds the caller's.
- **Last-owner protection.** `DELETE /orgs/{orgID}/members/{userID}` refuses to remove the organization's last remaining `owner`.

---

## Rate Limiting

A Redis-backed sliding-window limiter sits in front of every `/api/v1/*` route (including unauthenticated ones), keyed by client IP (`X-Forwarded-For` first, else the socket address):

- Default: **200 requests / 60 seconds** per IP (`RATE_LIMIT`, `RATE_LIMIT_WINDOW`)
- Implemented with an atomic Redis `INCR` + `EXPIRE NX` - the TTL self-heals if a previous `EXPIRE` call was ever lost to a transient Redis blip, instead of leaving a key incrementing forever
- Exceeding the limit returns `429 Too Many Requests`

Response headers on every request:

| Header | Meaning |
|---|---|
| `X-RateLimit-Limit` | The configured ceiling |
| `X-RateLimit-Remaining` | Requests left in the current window |
| `Retry-After` | (429 only) seconds until the window resets |

The AI failure-summary endpoints additionally enforce a **separate, per-project quota** of 40 generations/hour (see [Failure Summaries](#failure-summaries-ai)) - a cost control on the external LLM call, independent of the IP-based edge limiter.

---

## Errors

All error responses share one shape:

```json
{ "error": "human-readable message" }
```

| Status | Meaning |
|---|---|
| `400 Bad Request` | Malformed JSON or a field failed validation |
| `401 Unauthorized` | Missing, malformed, or expired access token |
| `403 Forbidden` | Authenticated, but the caller's role doesn't satisfy the route's minimum |
| `404 Not Found` | Resource doesn't exist, **or** the caller isn't a member of its owning organization |
| `409 Conflict` | The request conflicts with current state (duplicate name, duplicate idempotency key, job not in a retryable state, dependency in a terminal state, cannot remove the last owner) |
| `422 Unprocessable Entity` | The AI provider refused to answer (content-filtered) |
| `429 Too Many Requests` | IP rate limit or AI summary quota exceeded |
| `502 Bad Gateway` | Upstream AI provider call failed |
| `503 Service Unavailable` | A required dependency is down (Postgres unreachable on `/health`, or `GROQ_API_KEY` unset on the AI summary endpoints) |

---

## System

### `GET /health`

Unauthenticated liveness/readiness probe. Pings Postgres and Redis with a 2-second timeout each.

```json
{ "ok": true, "checks": { "database": "ok", "redis": "ok" } }
```

Returns `503` (with `"database": "unreachable"`) if Postgres is unreachable. A degraded Redis is reported (`"redis": "degraded"`) but does **not** fail the probe, since Redis is not a durability dependency.

### `GET /api/v1/features`

Unauthenticated. Lets the frontend detect which capabilities are live on this deployment without guessing:

```json
{
  "ai_failure_summaries": true,
  "ai_summary_model": "openai/gpt-oss-20b",
  "live_events": true,
  "workflow_dependencies": true,
  "queue_sharding": true,
  "rbac_roles": ["viewer", "member", "admin", "owner"]
}
```

`ai_failure_summaries` is `false` whenever `GROQ_API_KEY` is unset, so the dashboard can hide the feature entirely rather than surface a broken button.

---

## Auth Endpoints

### `POST /api/v1/auth/register`

Public.

**Request**
```json
{ "email": "user@example.com", "password": "correct horse battery staple", "name": "Ada Lovelace" }
```
Password is checked against a minimum-strength policy (rejects passwords containing the email local-part or name). On success, `201`:
```json
{ "access_token": "...", "refresh_token": "...", "user_id": "..." }
```
`409` if the email is already registered.

### `POST /api/v1/auth/login`

Public. `{ "email": "...", "password": "..." }` → `200` with the same token pair plus `"name"`. `401` on any credential mismatch (the same message for "no such user" and "wrong password", to avoid user enumeration).

### `POST /api/v1/auth/demo`

Public, no body. Issues a normal token pair for the read-write demo account so a visitor can explore the dashboard without registering → `200` with `access_token`, `refresh_token`, `user_id`, `email`, `name`. The session it returns is an ordinary one: same tokens, same RBAC, same expiry, so nothing downstream special-cases it.

Enabled only when the API has `DEMO_USER_EMAIL` set to a provisioned account; otherwise `503`. `GET /api/v1/features` advertises `demo_login` so the UI can hide the button when the deployment has no demo account.

### `POST /api/v1/auth/refresh`

Public. `{ "refresh_token": "..." }` → `200` with a **new** access/refresh pair. The presented refresh token is revoked in the same call (rotation) - reusing it afterward returns `401`, which is what makes token replay detectable.

### `POST /api/v1/auth/logout`

Revokes the given refresh token server-side. Always returns `200` even if the token was already invalid, so logout is idempotent from the client's perspective.

### `GET /api/v1/auth/me`

Returns the caller's own profile: `id`, `email`, `name`, `created_at`.

---

## Organizations

Base path: `/api/v1/orgs`. All routes below require authentication; per-route minimum role is noted.

| Method & Path | Role | Description |
|---|---|---|
| `GET /orgs` | any authenticated user | Organizations the caller belongs to, with their role in each |
| `POST /orgs` | any authenticated user | Create an organization; the creator becomes its first `owner` |
| `GET /orgs/{orgID}` | viewer | Organization detail |
| `PUT /orgs/{orgID}` | admin | Rename (slug is re-derived) |
| `DELETE /orgs/{orgID}` | owner | Delete (cascades to every project/queue/job beneath it) |
| `GET /orgs/{orgID}/members` | viewer | List members with role and join date |
| `POST /orgs/{orgID}/members` | admin | Add/update a member by email; body `{ "email": "...", "role": "member" }` |
| `DELETE /orgs/{orgID}/members/{userID}` | admin | Remove a member (subject to privilege-escalation and last-owner guards above) |

`POST /orgs` example:
```json
{ "name": "Meridian Labs" }
```
```json
{ "id": "8f2c...", "slug": "meridian-labs" }
```

---

## Projects

Base path: `/api/v1/orgs/{orgID}/projects` and `/api/v1/projects/{projectID}`.

| Method & Path | Role | Description |
|---|---|---|
| `GET /orgs/{orgID}/projects` | viewer | List projects in an org |
| `POST /orgs/{orgID}/projects` | admin | Create a project; response includes the **plaintext** API key once - only its SHA-256 hash is stored |
| `GET /projects/{projectID}` | viewer | Project detail |
| `PUT /projects/{projectID}` | admin | Rename |
| `DELETE /projects/{projectID}` | owner | Delete (cascades to queues/jobs/workers) |
| `POST /projects/{projectID}/rotate-key` | owner | Invalidate the current API key and issue a new one |
| `GET /projects/{projectID}/job-types` | viewer | Job `type` strings currently handled by at least one live (heartbeat within 2 minutes) worker - powers the dashboard's job-type autocomplete |

`POST /orgs/{orgID}/projects` response:
```json
{ "id": "b91a...", "api_key": "djq_9f3a...(shown once)", "slug": "payments-pipeline" }
```

---

## Retry Policies

Base path: `/api/v1/projects/{projectID}/retry-policies`. A retry policy is a named, reusable back-off configuration attached to a queue.

| Method & Path | Role | Description |
|---|---|---|
| `GET /projects/{projectID}/retry-policies` | viewer | List policies defined in the project |
| `POST /projects/{projectID}/retry-policies` | admin | Create a policy |

**Request**
```json
{
  "name": "aggressive-retry",
  "strategy": "exponential",
  "max_attempts": 5,
  "initial_delay_ms": 1000,
  "max_delay_ms": 60000,
  "multiplier": 2.0
}
```

| Strategy | Delay for attempt *n* |
|---|---|
| `fixed` | `initial_delay_ms` (constant) |
| `linear` | `initial_delay_ms * n`, capped at `max_delay_ms` |
| `exponential` | `initial_delay_ms * multiplier^(n-1)`, capped at `max_delay_ms` |

A job created against a queue with no `retry_policy_id` falls back to worker-side exponential back-off (`2^attempt` seconds, capped at 5 minutes).

---

## Queues

Base path: `/api/v1/projects/{projectID}/queues` and `/api/v1/queues/{queueID}`.

| Method & Path | Role | Description |
|---|---|---|
| `GET /projects/{projectID}/queues` | viewer | List queues in a project |
| `POST /projects/{projectID}/queues` | admin | Create a queue |
| `GET /queues/{queueID}` | viewer | Queue detail |
| `PUT /queues/{queueID}` | admin | Partial update (only supplied fields change) |
| `DELETE /queues/{queueID}` | admin | Delete (cascades to its jobs) |
| `POST /queues/{queueID}/pause` | member | Stop new claims; publishes `queue.paused` |
| `POST /queues/{queueID}/resume` | member | Resume claims; publishes `queue.resumed` |
| `GET /queues/{queueID}/stats` | viewer | Live counts by status, and by shard for sharded queues |

**Create/Update body**
```json
{
  "name": "email-dispatch",
  "description": "Transactional email delivery",
  "priority": 7,
  "concurrency_limit": 25,
  "shard_count": 8,
  "retry_policy_id": "c4e1..."
}
```

| Field | Range | Notes |
|---|---|---|
| `priority` | 1–10 | Higher runs first; default 5 |
| `concurrency_limit` | ≥ 1 | Advisory ceiling surfaced in queue stats - actual concurrency is enforced per worker, not centrally |
| `shard_count` | 1–64 | `1` = sharding disabled (any worker may claim any job); see [Architecture](README.md#queue-sharding) |

`POST` returns `409` if a queue with that name already exists in the project.

**Stats response**
```json
{
  "queue_id": "a1c2...",
  "by_status": { "queued": 42, "running": 8, "completed": 1904, "dead": 2 },
  "by_shard": { "0": 12, "1": 9, "2": 21 },
  "total": 1956
}
```

---

## Jobs

Base path: `/api/v1/queues/{queueID}/jobs` and `/api/v1/jobs/{jobID}`.

### Job status FSM

```
scheduled ──(due)──► queued ──(claimed)──► claimed ──► running ─┬─► completed
                        ▲                                        │
                        │                                        ├─► failed ──(attempts left)──► queued
   blocked ──(deps met)─┘                                        │
                                                                   └─► dead  (attempts exhausted → DLQ)

queued/scheduled/blocked ──(cancel)──► cancelled
blocked ──(upstream dependency died permanently)──► cancelled
```

| Status | Meaning |
|---|---|
| `scheduled` | Future-dated (`run_at` in the future) or a cron job awaiting its next tick |
| `blocked` | Waiting on one or more incomplete `depends_on` jobs |
| `queued` | Eligible for claim right now |
| `claimed` | A worker has reserved the row (`FOR UPDATE SKIP LOCKED`) but hasn't started executing yet |
| `running` | Executing inside a worker goroutine, under its `timeout_secs` deadline |
| `completed` | Finished successfully. A cron job is immediately re-armed back to `scheduled` for its next tick instead of staying terminal |
| `failed` | Errored with retry attempts remaining; requeued with a back-off delay |
| `dead` | Errored with no attempts remaining; a `dead_letter_queue` row is written in the same transaction |
| `cancelled` | User-cancelled while queued/scheduled/blocked, or auto-cancelled because an upstream dependency reached `dead`/`cancelled` |

### `GET /queues/{queueID}/jobs`

Query params: `status` (exact match on the FSM values above), plus [pagination](#pagination). Returns the paginated envelope.

### `GET /projects/{projectID}/jobs`

Every job in the project, newest first, in one request. Same query params as the per-queue listing (`status`, plus [pagination](#pagination)), and each row carries an extra `queue_name` so a cross-queue table needs no second lookup.

Prefer this over fanning out `GET /queues/{queueID}/jobs` per queue: one round trip instead of one per queue, and the ordering is a true global `created_at DESC` rather than a client-side merge of per-queue pages, which silently mis-ranks once any single queue has more recent jobs than the page size.

### `GET /projects/{projectID}/job-types`

See [Projects](#projects) - listed here too since it's job-domain data.

### `POST /queues/{queueID}/jobs`

Creates one job. `member` role required.

```json
{
  "type": "send_email",
  "payload": { "to": "user@example.com", "template": "welcome" },
  "priority": 5,
  "max_attempts": 3,
  "timeout_secs": 300,
  "scheduled_at": null,
  "cron_expression": null,
  "idempotency_key": "welcome-email-user-42",
  "tags": ["transactional"],
  "partition_key": "user-42",
  "depends_on": []
}
```

| Field | Required | Notes |
|---|---|---|
| `type` | yes | Free-text; only matters in that a worker must have a handler registered for it, or execution fails loudly with `no handler registered for job type "..."` |
| `payload` | no | Arbitrary JSON, defaults to `{}` |
| `priority` | no | 1–10, default 5 |
| `scheduled_at` | no | A future timestamp creates the job as `scheduled` instead of `queued` |
| `cron_expression` | no | Standard 5-field cron; combined with `scheduled_at` marks the job recurring |
| `idempotency_key` | no | Unique across the whole system; a duplicate key is silently ignored (`ON CONFLICT DO NOTHING`) and reported as `409` |
| `partition_key` | no | Only meaningful on a sharded queue (`shard_count > 1`) - jobs with the same key always land on the same shard, giving per-key ordering affinity. Falls back to the job's own ID (i.e. no affinity) when omitted |
| `depends_on` | no | Up to `workflow.MaxDependenciesPerJob` existing job IDs in the same project. If any are unresolved, the job is created as `blocked` instead of `queued`/`scheduled` |

`201` response:
```json
{ "id": "d4a1...", "status": "queued", "depends_on": [] }
```

Rejections: `400` on a missing/invalid field or an unknown dependency ID; `409` on a duplicate `idempotency_key` or a dependency that is already `dead`/`cancelled` (and therefore can never complete - the API refuses to create a job that would block forever); `404` if the queue doesn't exist.

### `POST /queues/{queueID}/jobs/batch`

Creates up to 1000 jobs - and, optionally, an entire workflow DAG - in one call and one transaction. `member` role required.

```json
[
  { "ref": "extract", "type": "extract", "payload": {} },
  { "ref": "transform", "type": "transform", "payload": {}, "depends_on": ["extract"] },
  { "ref": "load", "type": "load", "payload": {}, "depends_on": ["transform"] }
]
```

`ref` is a **batch-local** name (not a job ID) that lets a job in the same array declare a dependency on a sibling that doesn't have a real ID yet. `depends_on` entries that aren't a `ref` in this batch are resolved as real job IDs from elsewhere in the project. Cycles are detected before anything is written (Kahn's algorithm) and rejected with `400` and the offending path.

`201` response:
```json
{ "batch_id": "f0e2...", "count": 3, "skipped": 0, "job_ids": ["...", "...", "..."] }
```

`skipped` counts jobs dropped because their `idempotency_key` already existed; if a skipped job was itself a dependency target for another job in the batch, the whole batch fails with `409` rather than silently creating an unsatisfiable dependency.

### `GET /jobs/{jobID}`

Full job record - status, payload, priority, attempt counters, scheduling fields, shard/partition key, tags, timestamps.

### `GET /jobs/{jobID}/dependencies`

```json
{
  "job_id": "d4a1...",
  "depends_on": [{ "job_id": "...", "type": "extract", "status": "completed" }],
  "dependents": [{ "job_id": "...", "type": "load", "status": "blocked" }],
  "blocked_by": [],
  "satisfied": true
}
```

### `DELETE /jobs/{jobID}` - Cancel

`member` role. Only valid from `queued`, `scheduled`, or `blocked`; `409` otherwise (a `running` job cannot be cancelled out from under its worker - let it finish or time out).

### `POST /jobs/{jobID}/retry`

`member` role. Valid from `failed`, `dead`, or `cancelled`. Resets attempt count and clears the previous error, then re-evaluates dependencies - if an upstream job isn't `completed`, the job re-enters as `blocked` rather than `queued`. Also clears any associated DLQ entry.

### `DELETE /jobs/{jobID}/purge`

`admin` role. Hard-deletes the job row (cascades to its executions, logs, DLQ entry, and dependency edges). Irreversible - use for cleaning up test data, not for normal failure handling.

### `GET /jobs/{jobID}/logs`

Paginated. Structured log lines (`level`, `message`, `metadata`) written by the executor during the job's lifetime, newest first.

### `GET /jobs/{jobID}/executions`

Every attempt for this job, newest first - `attempt_number`, `status`, `worker_id`, `started_at`/`completed_at`, `duration_ms`, `error_message`.

---

## Failure Summaries (AI)

Base path: `/api/v1/jobs/{jobID}/failure-summary`. An optional feature - see `GET /features`. Requires `GROQ_API_KEY` to be configured.

### `GET /jobs/{jobID}/failure-summary`

`viewer` role. Returns the most recently generated summary, or `404` if none exists yet. The response includes `"stale": true` once the job has failed again (or its evidence has otherwise changed) since the summary was generated - the fingerprint is a SHA-256 hash of the rendered evidence plus the model name, so identical evidence is guaranteed to reuse the cached result.

```json
{
  "job_id": "d4a1...",
  "summary": "The job failed while calling the downstream payment gateway...",
  "likely_cause": "Upstream service returned HTTP 503 for all three attempts.",
  "suggested_action": "Verify the payment gateway's status page before retrying.",
  "category": "external_dependency",
  "confidence": "high",
  "is_transient": true,
  "model": "openai/gpt-oss-20b",
  "input_tokens": 812,
  "output_tokens": 143,
  "stale": false,
  "created_at": "2026-08-24T18:02:11Z",
  "updated_at": "2026-08-24T18:02:11Z"
}
```

### `POST /jobs/{jobID}/failure-summary` - Generate

`member` role. Only valid for jobs in `failed` or `dead` status (`409` otherwise). Guarded three ways:

1. **Cache short-circuit** - if the evidence fingerprint is unchanged since the last generation, the cached row is returned as-is (no LLM call, no cost).
2. **Distributed lock** (`djq:ai:summary:{jobID}`, Redis `SET NX PX` + Lua CAS release) - a concurrent generation request for the same job gets `409` instead of paying for the call twice.
3. **Per-project quota** - 40 generations/hour via Redis `INCR`/`EXPIRE`; exceeding it returns `429`.

The model is called with a JSON-schema-constrained response, so `category` and `confidence` can never drift from the values the `job_failure_summaries` table's `CHECK` constraints allow. `502` if the provider call fails outright, `422` if the provider refuses to answer, `503` if no API key is configured.

---

## Workers

Base path: `/api/v1/projects/{projectID}/workers` and `/api/v1/workers/{workerID}`. Read-only from the API - workers register and deregister themselves directly against Postgres.

| Method & Path | Role | Description |
|---|---|---|
| `GET /projects/{projectID}/workers` | viewer | List workers; `?status=active` additionally requires a heartbeat within the last 2 minutes (a worker that crashed without deregistering is not reported as active just because its `status` column was never flipped) |
| `GET /workers/{workerID}` | viewer | Worker detail plus its 10 most recent heartbeat samples (`jobs_running`, `jobs_completed`) |

```json
{
  "worker": {
    "id": "9c3e...", "project_id": "...", "hostname": "worker-7f2a",
    "pid": 4821, "status": "active", "concurrency": 5,
    "registered_at": "...", "last_heartbeat_at": "..."
  },
  "heartbeats": [{ "at": "...", "jobs_running": 3, "jobs_completed": 118 }]
}
```

---

## Dead Letter Queue

Base path: `/api/v1/queues/{queueID}/dlq`, `/api/v1/dlq/{dlqID}`, and `/api/v1/projects/{projectID}/dlq/*`. Every permanently failed job (attempts exhausted) lands here.

| Method & Path | Role | Description |
|---|---|---|
| `GET /queues/{queueID}/dlq` | viewer | Paginated list of dead-letter entries for a queue |
| `POST /dlq/{dlqID}/retry` | member | Requeue one entry |
| `DELETE /dlq/{dlqID}` | admin | Discard one entry (marks resolved, does not delete the job) |
| `POST /projects/{projectID}/dlq/retry-all` | member | Bulk-requeue every unresolved entry in the project whose job `type` is currently handled by at least one live worker |
| `DELETE /projects/{projectID}/dlq/unhandled` | admin | Bulk-discard entries whose job `type` has **no** live handler - cleans up orphaned entries (e.g. from a decommissioned job type) without touching ones that are still actionable |

A retry is refused with `409` if no live worker currently advertises a handler for that job's `type` - retrying it would just fail again identically. "Live" means `status='active'` and a heartbeat within the last 2 minutes, and "handles type X" is read from `workers.handled_types`, which each worker publishes on startup from its own handler registry.

```json
{ "requeued": 14, "skipped_unhandled": 3 }
```

---

## Metrics

Base path: `/api/v1/projects/{projectID}/metrics` and `/api/v1/queues/{queueID}/metrics`. `viewer` role.

Query param `hours` (default 24, max 720) controls the look-back window; bucket width auto-scales so the series stays readable (15-minute buckets ≤ 6h, hourly ≤ 48h, daily beyond that).

**Project metrics** additionally break down job counts per queue and report `active_workers`:

```json
{
  "queues": [{ "queue_id": "...", "queue_name": "email-dispatch", "by_status": { "completed": 1904, "queued": 42 } }],
  "active_workers": 6,
  "completed_24h": 5310,
  "throughput_24h": [{ "hour": "2026-08-25T08:00:00Z", "completed": 214, "failed": 3 }],
  "range_hours": 24,
  "bucket_seconds": 3600,
  "avg_duration_ms": 842.6,
  "generated_at": "2026-08-25T09:00:00Z"
}
```

**Queue metrics** is the same throughput/duration shape scoped to one queue, plus a flat `by_status` count.

---

## Live Events (WebSocket)

### `GET /api/v1/projects/{projectID}/events`

`viewer` role. Upgrades to a WebSocket connection (`?token=<jwt>` since browsers can't set headers on the handshake - see [Authentication](#authentication)).

On connect the server sends `{"type":"stream.ready"}` once its Redis subscription for the project is confirmed live - no event published after that point can be lost to a subscribe race. Thereafter it streams every event published to that project's channel (`djq:events:<projectID>`) as a JSON text frame:

```json
{
  "type": "job.completed",
  "project_id": "...", "queue_id": "...", "job_id": "...", "job_type": "send_email",
  "worker_id": "...", "status": "completed", "attempt": 1, "error": ""
}
```

**Event types:** `job.enqueued`, `job.started`, `job.completed`, `job.failed`, `job.dead_lettered`, `job.unblocked`, `job.cancelled`, `queue.paused`, `queue.resumed`, `worker.online`, `worker.offline`.

Server behavior:
- One Redis subscription per project, shared by every connected client, created on the first subscriber and torn down with the last.
- Per-client send buffers are bounded (64 frames); a client that can't keep up is disconnected (`1008 Policy Violation`) rather than allowed to grow the server's memory unboundedly.
- A project is capped at 200 concurrent live connections; the 201st gets `1013 Try Again Later`.
- Idle connections are kept alive with a ping every 25 seconds.

This is a **supplement**, not a replacement, for polling - every list/detail endpoint remains fully functional on its own, and the dashboard falls back to its normal refresh interval if the socket drops.

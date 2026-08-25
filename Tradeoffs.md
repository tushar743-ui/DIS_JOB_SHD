# Design Decisions and Trade-offs

Every choice below was made against at least one credible alternative. This
document records what was picked, what was given up, and the condition under
which the other option would have won instead.

---

## Tech Stack

### Go for API and worker (vs Node / Python / JVM)
Go gives goroutines and channels for the exact concurrency shape a poller +
executor needs, a static binary with no runtime to provision, and a type
system that catches the kind of nil/interface bugs that are expensive to hit
in a job-execution hot path. Cost: slower to prototype than Python, and
generics-heavy shared code (`shared/lock`, `shared/shard`) reads denser than
the equivalent in a dynamic language. Would flip to Node/TypeScript if the
team were frontend-heavy and threw a single language across the stack, or to
Python if the workload were ML-pipeline-shaped rather than I/O-shaped.

### PostgreSQL as the only durable store (vs Postgres + a broker)
No Kafka, no SQS, no RabbitMQ. `FOR UPDATE SKIP LOCKED` does atomic claiming,
and every other durable fact (queue config, worker health, retry history,
auth tokens) lives in the same database. One store to back up, one store to
reason about consistency in, no dual-write problem between a broker and a
system of record. Cost: throughput ceiling is whatever one Postgres primary
can do - tens of thousands of claims/sec, not the millions a purpose-built
broker manages. Right call at the target scale (single org's internal job
traffic); wrong call past roughly 5-10k jobs/sec sustained, where a real
broker's partitioned log beats row-level locking.

### Neon (serverless Postgres) over self-hosted
Branching for throwaway test databases, autoscaling, pooled connections
without running PgBouncer by hand. Cost: the pooled endpoint can't run
migrations (PgBouncer transaction mode doesn't support the advisory locks
`golang-migrate` takes - migrations have to hit the direct endpoint), and cold
branches add latency on first connect. Self-hosted Postgres would remove both
issues but hands back all the operational work Neon does automatically.

### Upstash Redis over self-hosted / managed cluster
Redis here is intentionally narrow - rate limits, pub/sub, locks, shard
registry, AI quota - nothing durable. A serverless, pay-per-request Redis
fits that shape better than provisioning and sizing a cluster for what is,
by design, disposable state. Cost: per-command pricing gets worse than a
fixed-size instance once event volume is high (every job transition
publishes an event). Acceptable because nothing here needs sub-millisecond
latency at extreme volume; would reconsider only if event throughput grew an
order of magnitude past current levels.

### sqlc over an ORM (GORM / ent)
Hand-written SQL, compiled into typed Go functions. Every hot-path query
(the poll claim, the reclaim sweep) is visible and tunable as actual SQL,
not generated through an abstraction that might not produce the plan you
expect. Cost: more boilerplate per query, no automatic migrations-from-struct
convenience. Worth it because this system's correctness depends on exact
lock semantics (`FOR UPDATE SKIP LOCKED`) that most ORMs don't expose
cleanly.

### chi router over a full framework (gin / echo / Fiber)
chi is a thin `net/http`-compatible router; middleware is just
`func(http.Handler) http.Handler`, so the auth/RBAC/rate-limit chain is
composed from standard interfaces instead of framework-specific hooks.
Cost: no built-in validation/binding helpers, both handled by hand in each
handler. Trade accepted because the extra framework surface wasn't buying
anything the standard library plus one router didn't already cover - see the
project's own build rule ("stdlib does it → use it").

### REST over gRPC
The dashboard is a browser client and the primary external integration
surface is "any HTTP client with a JWT." REST plus JSON keeps that audience
maximally wide with zero tooling requirement. Cost: no strict schema
contract at the wire level (mitigated by swaggo-generated OpenAPI) and
slightly more bytes on the wire than protobuf. Would reconsider for a
worker-to-worker internal protocol at much larger fleet sizes, where gRPC's
binary framing and streaming would actually matter.

### JWT access token + DB-persisted rotating refresh token (vs pure stateless JWT, vs server sessions)
Short-lived access tokens keep most requests stateless; the refresh token is
the one piece of state, stored hashed in Postgres so it can be revoked
server-side and rotation can detect replay. Pure stateless JWT would mean a
compromised token stays valid until expiry with no way to kill it early.
Server-side sessions for everything would remove the stateless benefit
entirely. This is the middle point: fast common path, revocable exception
path.

### Next.js App Router + SWR polling with a WebSocket overlay (vs pure polling, vs pure WebSocket)
SWR's polling is the baseline - always correct, degrades gracefully, works
even if a socket never connects. The WebSocket hook (`useLiveEvents`) layers
targeted revalidation on top for sub-second freshness but is never the only
path to correct data. Pure WebSocket-only would mean a dropped connection
silently goes stale; pure polling-only would mean up to a full interval of
lag on every job transition. The `LiveIndicator` pill exists specifically so
an operator can tell which mode they're actually in.

### Groq (OpenAI-compatible Chat Completions) over Anthropic direct
Originally built against Anthropic's Messages API; swapped when the user
supplied a Groq key. Groq's `strict: true` JSON-schema mode gives the same
guarantee Anthropic's structured output did - the AI's `category`/
`confidence` fields can never violate the DB's `CHECK` constraints - at
lower cost and lower latency per call. Cost: smaller open-weight models
(`gpt-oss-20b/120b`) reason less carefully on ambiguous failures than a
frontier model would. Acceptable here because the feature is a diagnostic
hint for a human, not an autonomous action.

### R2/S3 as the design for large payloads and logs (vs everything in Postgres)
Job payloads and execution logs stay in Postgres for now, since actual
volume in this deployment target doesn't justify object storage's added
moving part. The schema and code are written so payload/log blobs can be
redirected to object storage without a breaking change, but that migration
is deferred until row/table size actually demands it - premature
infrastructure has a real maintenance cost too.

---

## Concurrency and Reliability

### `FOR UPDATE SKIP LOCKED` as the entire claim mechanism (vs an app-level lock, vs a broker)
One `UPDATE ... RETURNING` per poll tick. Postgres grants the row lock to
exactly one competing worker and the rest skip past it with no blocking, no
retry loop, no separate coordination service. This is strictly simpler than
a Redis-based claim lock (which would add a second failure mode: lock says
claimed, DB write fails) and correctness never depends on Redis being up.
Cost: contention scales with concurrent pollers hammering the same rows,
which sharding exists specifically to relieve.

### Scheduler leadership: elected, but correctness never depends on it
Only one worker runs the promote/cron/unblock/reclaim sweep at a time,
elected via a Redis lock - this avoids redundant work, not incorrect work.
Every sweep statement is idempotent, so when Redis is unreachable the sweep
runs unguarded on every worker instead of stopping. The alternative - treat
the lock as required - would turn a Redis outage into a scheduling outage.
Trading a little duplicate work for continued availability was the explicit
choice.

### Rendezvous hashing (HRW) for shard ownership (vs consistent hashing ring, vs fixed modulo)
HRW needs no ring structure or virtual nodes, computes ownership
independently per shard, and reshuffles the minimum number of assignments
when a worker joins or leaves. Fixed modulo (`shard % worker_count`) would
reshuffle nearly everything on every membership change. Cost: HRW does not
guarantee tight per-worker balance - at 64 shards over 8 workers the
standard deviation is around 2.65, so one worker holding 3 shards is normal
variance, not a bug. Mitigation is operational: keep `shard_count` well
above worker count.

### Sharding is a contention optimization, never a correctness mechanism
If the Redis shard registry is unreachable, a worker claims every shard
instead of none. This looks wasteful but is deliberate: `SKIP LOCKED` still
guarantees no duplicate execution regardless of shard ownership, so
degrading to "everyone claims everything" only costs some redundant lock
contention, never a double-run job. The alternative - refuse to claim
without a live registry - would turn a Redis blip into a total processing
stall.

### Event-driven wake-up layered on the poll ticker, not replacing it
Redis pub/sub cuts enqueue-to-pickup latency from up to a full poll
interval down to milliseconds, but the ticker keeps running underneath as
the fallback. A dropped pub/sub message costs latency, never a lost job.
Building this as pub/sub-only would have made Redis load-bearing for
correctness, which the rest of the system deliberately avoids.

### Retry backoff strategies capped by policy, computed at execute time
Fixed, linear, exponential - all capped at a configurable `max_delay_ms` so
a misconfigured exponential policy can't produce an hours-long delay by
accident. The cap is enforced in the executor, not just documented as a
convention, so a bad policy value degrades to "always wait the cap" instead
of silently growing without bound.

### Idempotency keys are optional, not a global constraint
Callers may attach an idempotency key per job; the API does not force one on
every submission. This trusts the caller for jobs where duplicate execution
is harmless (most background jobs) and only pays the uniqueness-check cost
where it's asked for. A blanket requirement would be safer by default but
would force every integration to invent a key even for naturally idempotent
work.

---

## Data Model

### Three tables for job state instead of one wide table
`jobs`, `job_executions`, `job_logs` are separated because they have
different write cardinality - one row per job, one row per attempt, many
rows per attempt - and different retention needs. A single wide table would
simplify some queries but would force every log line to either bloat the
jobs table or live in a JSON blob no index can target individually.

### Mixed primary key strategy: UUID by default, BIGSERIAL for log tables
UUIDs avoid exposing sequential IDs and work cleanly with distributed
inserts across workers. `worker_heartbeats` and `job_logs` use `BIGSERIAL`
instead, because they're purely additive high-volume tables where a
sequential integer is both cheaper to index and naturally chronological.
Using UUID everywhere would have been more consistent; using BIGSERIAL
everywhere would leak row counts and ordering on every other table.

### Partial indexes scoped to the exact hot query
`idx_jobs_poll` only covers `WHERE status='queued'` rows; `idx_jobs_reclaim`
only covers stuck `claimed`/`running` rows. These stay small and fast
regardless of how many millions of `completed` rows pile up in the same
table. Cost: each partial index adds a small predicate check on every
insert/update that might match it. Full-table indexes would avoid that
check but grow linearly with historical data the poller never looks at
again.

### `ON DELETE` policy chosen per relationship, not a blanket default
`CASCADE` where a child has no meaning without its parent (jobs, executions,
logs, dependencies); `SET NULL` where the child clearly outlives the parent
and only loses an attribution (`jobs.claimed_by` when a worker is
deregistered, `queues.retry_policy_id` when a named policy is deleted). A
single global policy would either orphan rows that should have been cleaned
up, or delete rows that should have survived their parent's removal.

### `job_status` as a Postgres ENUM, not TEXT
Smaller on disk and self-documenting as a `CHECK` constraint that can never
silently drift out of sync with the application's status list - the
database itself rejects an invalid status string. Cost: adding a new status
value later requires a migration that runs `ALTER TYPE ... ADD VALUE` in its
own transaction (Postgres forbids using a new enum value in the same
transaction that adds it), which is exactly what happened when `blocked` was
introduced.

---

## API and Authorization

### Per-request, DB-derived role resolution (vs roles embedded in the JWT)
Every protected route re-resolves the caller's role from
`organization_members` on that request, rather than trusting a role claim
baked into the access token at login. Removing someone from an organization
takes effect on their very next request instead of waiting for token expiry.
Cost: one extra indexed query per authorized request. Embedding roles in the
JWT would remove that query but reintroduce a stale-permission window equal
to the token's lifetime.

### 404, not 403, for resources outside a caller's org
A non-member requesting someone else's job gets "not found," not "forbidden"
- the API never confirms a resource exists to someone who can't see it.
This closes an enumeration side-channel that 403 responses open. Cost: a
legitimate caller who mistypes an ID gets the same response as one probing
for other tenants' data, which is a slightly worse debugging experience,
accepted deliberately for the security property.

### Rate limiting before authentication, not after
The Redis sliding-window limiter sits ahead of JWT verification in the
middleware chain, so unauthenticated abuse (credential stuffing, token
guessing) is shed before it reaches any handler logic. The alternative
(rate limit per authenticated user only) leaves the login endpoint itself
unprotected against volume attacks.

### CORS as an explicit allowlist, not a wildcard
Once `CORS_ORIGINS` is set, only listed origins are honored. A wildcard is
simpler to configure and was the default early on, but combined with
credentialed requests (JWT-bearing) a wildcard origin is a real exposure,
not just a convenience trade-off.

---

## Testing

### Integration tests against a real Neon DB and real Upstash Redis (vs mocks)
`FOR UPDATE SKIP LOCKED` behavior, migration ordering, and lock/CAS
semantics are exactly the kind of logic that mocks would either get wrong or
not bother modeling at all. Running the concurrency tests (20 workers × 200
jobs, exactly-once shard coverage) against the real database is what
actually caught the race condition in the WebSocket hub's subscribe timing
and the shard-hashing skew bug - neither would have surfaced against an
in-memory fake. Cost: the integration suite takes minutes, not
milliseconds, and needs live credentials in CI. Split into a separate
`-tags integration` build specifically so the fast unit suite stays fast for
routine iteration.

### Table-driven unit tests kept separate from integration tests
Pure logic (retry delay math, HRW scoring, workflow cycle detection, RBAC
role comparisons) is tested with zero I/O and runs in milliseconds. Only the
tests that need real concurrency or real Postgres locking semantics pay the
network cost. Merging them into one suite would make every CI run slow for
no benefit to the tests that don't need it.

---

## Code and Process

### Multi-module Go workspace (`api/`, `worker/`, `shared/`) over one module
`shared/` holds exactly the contracts that must never drift between the API
and the worker fleet - event types, lock semantics, shard math - imported by
both, each module still buildable and deployable independently. A single
module would make that sharing implicit and easier to accidentally couple;
fully separate repos would make keeping the contract in sync a manual,
error-prone process across repo boundaries.

### `net/http` for the Groq client instead of a vendor SDK
The AI summarizer is one HTTP POST with a JSON body and a JSON-schema
response contract. Pulling in an SDK for that is a dependency (and its
transitive tree) for something the standard library already does in about
forty lines. This is also why the earlier Anthropic SDK was removed
entirely on the provider swap rather than kept around unused.

### No inline comments; rationale lives in documentation instead
Code stays self-explanatory through naming and structure; the "why" behind a
non-obvious decision (why a lock degrades instead of blocking, why a status
check exists in two places) is recorded here and in the architecture docs
instead of scattered as comments that rot as the code moves. Cost: this
document has to be kept honest and current, or the reasoning it holds goes
stale exactly the way a comment would have.

### Structured logging (zerolog) over the standard `log` package
Every log line carries request ID, latency, status, and caller identity as
structured fields, queryable in whatever log aggregation the deployment
uses, rather than parsed out of a formatted string. Cost: slightly more
verbose call sites than `log.Printf`. Worth it the moment more than one
service is emitting logs into the same place, which is the normal case here
(API plus N worker pods).

### Worker liveness by heartbeat freshness, not a status push on shutdown
A worker is "active" only if its last heartbeat is under two minutes old;
nothing flips a status column to `offline` on a clean exit, and a crashed
process is treated identically to one that shut down cleanly. This is
simpler and strictly more correct than trusting a graceful-shutdown signal,
because a crash never sends one. Cost: up to two minutes of a dead worker
still showing as live in the dashboard, accepted as a bounded, known window
rather than an unbounded one.

---

## Demo Mode

### A dedicated `/auth/demo` endpoint, not demo credentials in the client
The button could have called the normal login with a hardcoded email and
password. That ships a working password in the JS bundle, and anyone reading
it can also use the ordinary sign-in form. Instead the server mints a session
for an account named by `DEMO_USER_EMAIL`, so no password exists client-side
and the demo account is configuration rather than code. Cost: one more public
endpoint to reason about. It issues an ordinary session with ordinary RBAC, so
it widens who can reach the demo workspace, not what a session can do.

### The demo account gets full access, not a read-only viewer role
A public button granting `owner` on a real workspace is a deliberate choice,
not an oversight: the point is to let someone evaluate the system - create a
queue, pause it, retry a dead job - which a `viewer` role would block. The
trade is that any visitor can also mutate that workspace. That is acceptable
for a workspace whose entire contents are machine-generated demo traffic, and
would not be for one holding anything real.

### The generator lives in the worker, gated by a flag
It could have been its own binary or an external script. The worker is already
the always-running process with database access, graceful shutdown, and lock
infrastructure, so a goroutine behind `DEMO_MODE` is less machinery than a new
service to deploy and supervise. Cost: a consumer process now also produces,
which muddies its single responsibility. Contained by keeping it in its own
package and off by default.

### Standing down when Redis is unavailable, unlike the scheduler
The scheduler deliberately runs unguarded when it cannot take its lock, because
every sweep statement is idempotent and availability matters more than doing
the work once. The generator does the opposite: creating jobs is not
idempotent, so running unguarded on every worker would multiply demo traffic by
the fleet size. Same lock, opposite fallback, because the operations have
opposite duplication semantics.

### Bounded by a backlog ceiling and a retention window
"Runs forever" has to mean a steady state, not unbounded growth. The generator
skips any queue already at `DEMO_BACKLOG_MAX` in-flight jobs, so it never
outruns the workers, and prunes its own completed jobs past `DEMO_RETENTION`.
The pruner is scoped to `status='completed'` rows tagged `demo`, so it can
never touch real data, and deliberately leaves `dead` jobs alone - those are
the DLQ's demo content and the failure rate already bounds them.

### One project-wide jobs endpoint instead of per-queue fan-out
The dashboard originally built its cross-queue table by requesting each queue's
jobs separately and merging client-side. Under continuous traffic that turned
every live event into one request per queue, and the browser's per-host
connection limit meant the page never finished loading. `GET
/projects/{id}/jobs` replaced it with a single query. It also fixed a
correctness bug that low traffic had hidden: merging fixed-size per-queue pages
mis-ranks the combined list as soon as one queue has more recent jobs than the
page size.

### Live-event revalidation is coalesced, not immediate
Each WebSocket event used to trigger its revalidations synchronously, which is
fine when jobs arrive occasionally and pathological when they arrive
constantly. Events now accumulate into a deduplicated set flushed once a
second. The upper bound on refetching is now the flush interval rather than the
event rate, which is what makes the dashboard stable under demo traffic. Cost:
up to a second of extra latency on a live update, imperceptible for a
monitoring view.

## Known Debt (left as-is deliberately, not by oversight)

- Queue config sheet collects `rate_limit_enabled` / `rate_limit_per_minute`
  in the form but the backend never receives them - the per-queue rate
  limiting feature was scoped out after the form was built; the dead field
  is left visible rather than silently dropped, pending a decision to wire
  it up or remove it.
- No distributed tracing (OpenTelemetry or equivalent) across API → worker
  → Postgres/Redis. Structured logs plus request IDs cover the common
  debugging path today; tracing would matter more once the worker fleet is
  large enough that correlating a single job's path by log-grepping stops
  being practical.
- A dependent job's status change on someone else's open dashboard page
  currently relies on the 5-second polling fallback rather than immediate
  WebSocket push, since the live-event revalidation map only targets the
  page currently being viewed. Cheap to extend, not yet load-bearing enough
  to prioritize.

# dis-job-queue

A distributed job queue system with priority scheduling, retry policies, cron support, dead-letter queue, and a real-time dashboard.

---

## System Architecture

```mermaid
flowchart TD
    Browser["🖥️ Browser / API Client"]

    subgraph Frontend["Next.js Dashboard  · port 3000"]
        UI["Pages: Jobs · Queues · Workers\nDLQ · Metrics · Settings"]
    end

    subgraph API["Go API Server  · port 8080  (chi router)"]
        direction TB
        MW["Middleware\nJWT Auth · Redis Rate Limiter · CORS · Logger"]
        Routes["REST Handlers\n/auth  /orgs  /projects  /queues\n/jobs  /workers  /dlq  /metrics"]
        MW --> Routes
    end

    subgraph Workers["Worker Pods  ×N  (Go)"]
        direction TB
        Poller["Poller\nSELECT … FOR UPDATE SKIP LOCKED"]
        Executor["Executor\nconcurrency semaphore · timeout · retry"]
        Scheduler["Cron Scheduler\nschedule → queued promotion"]
        Heartbeat["Heartbeat\nworker_heartbeats every 10 s"]
        Poller --> Executor
    end

    subgraph PG["PostgreSQL 16"]
        direction TB
        Schema["users · organizations · projects\nqueues · retry_policies\njobs  ➜  status FSM\njob_executions · job_logs\ndead_letter_queue\nworkers · worker_heartbeats\nrefresh_tokens"]
    end

    Redis["Redis 7\nRate-limit counters\n(IP-based sliding window)"]

    Browser -->|"HTTP"| Frontend
    Frontend -->|"REST / JWT"| API
    Browser -->|"REST / JWT"| API

    API -->|pgxpool| PG
    API -->|INCR / EXPIRE| Redis

    Workers -->|"FOR UPDATE SKIP LOCKED\nclaim jobs"| PG
    Workers -->|heartbeat rows| PG
    Workers -->|execution & log rows| PG
    Workers -->|Redis client| Redis

    Executor -->|"failed → DLQ\nretry → re-queue"| PG
    Scheduler -->|"next_run_at update"| PG
```

---

## How It Works

The system is organized into four distinct layers that communicate through PostgreSQL as the single source of truth. A client — either the Next.js dashboard or any HTTP consumer — authenticates against the Go API server using JWT tokens issued on login; refresh tokens are persisted in PostgreSQL and Redis is used solely for a sliding-window rate limiter that caps each IP at 200 requests per minute. Once authenticated, clients interact with a resource hierarchy of Organizations → Projects → Queues → Jobs: jobs are inserted into the `jobs` table with a `status` of `queued` (or `scheduled` for future-dated work), carrying a JSON payload, a priority value between 1 and 10, configurable retry limits, an optional idempotency key to prevent duplicates, and an optional cron expression for recurring execution. On the processing side, one or more Worker pods connect directly to PostgreSQL and run a tight polling loop that issues `SELECT … FOR UPDATE SKIP LOCKED` — a lock-free, race-safe pattern that lets any number of workers compete for jobs without stepping on each other. Each claimed job is handed to the Executor, which enforces per-job timeouts and a concurrency semaphore so a single worker never exceeds its configured parallelism limit. If a job fails, the Executor applies the queue's retry policy — fixed, linear, or exponential back-off — and re-queues it up to the configured maximum attempts; once all attempts are exhausted the job row is marked `dead` and a corresponding `dead_letter_queue` record is written, from which operators can inspect, retry, or discard from the dashboard. A separate Cron Scheduler goroutine inside each worker wakes every five seconds to promote due `scheduled` jobs to `queued` and to compute the next `run_at` for any recurring cron job. Workers also emit a heartbeat row every ten seconds so the API can report live worker health and detect stale processes. All structural changes — schema, indexes, triggers — are managed through versioned SQL migrations in `db/migrations/`, and the entire stack is wired together via `docker-compose.yml` with hot-restart policies and health checks on both PostgreSQL and Redis.

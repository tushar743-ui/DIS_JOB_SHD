# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Backend developers integrating asynchronous job processing into their own services. They interact primarily through the REST API and JWT-authenticated clients; the dashboard serves as their control plane for monitoring, managing, and debugging job pipelines across projects.

## Product Purpose

A self-hosted distributed job queue system with a first-class dashboard. Provides priority scheduling (1–10), configurable retry policies (fixed / linear / exponential back-off), cron-based recurring jobs, dead-letter queue with manual requeue, and multi-tenant organization. Jobs are durable in PostgreSQL; workers claim them via `FOR UPDATE SKIP LOCKED` and execute with per-job timeouts and concurrency semaphores. The dashboard surfaces job status, execution logs, worker health, and queue-depth metrics in real time.

## Positioning

Open decision - not yet confirmed. The system uses PostgreSQL as its sole durable store (Redis is rate-limiting only, never job state), which simplifies operational overhead relative to broker-based queues. The built-in multi-tenant dashboard (org → project → queue → job) is a differentiator over library-only solutions. These may be the positioning axes; record the one that resonates once validated.

## Operating Context

- Developers configure queues and submit jobs via the REST API (`POST /jobs`, queue config endpoints).
- The dashboard is the primary interface for operations: browsing job history, inspecting execution logs, reviewing dead-letter entries, and monitoring live worker count and throughput.
- Workers run as one or more Go pods, connect directly to PostgreSQL, and are managed via docker-compose in typical deployment.
- The resource hierarchy - Organization → Project → Queue → Job - is the mental model users navigate; UI language must reinforce it consistently.

## Capabilities and Constraints

- **Job lifecycle states:** queued → running → succeeded / failed (retry or dead).
- **Retry policies:** fixed, linear, exponential back-off; per-queue configuration.
- **Cron scheduling:** optional cron expression on any job; promoted by the worker's scheduler goroutine every 5 seconds.
- **Idempotency:** optional idempotency key on job submission to prevent duplicates.
- **Auth:** JWT access tokens (short-lived) + PostgreSQL-persisted refresh tokens (revocable); multi-tenant scoping enforced server-side.
- **Rate limiting:** Redis sliding-window IP limiter (200 req/min) in the API middleware chain.
- **No deployment constraint set yet** - keep design decisions flexible across self-hosted and potential future hosted paths.

## Brand Commitments

**Anti-pattern constraint (binding):** The UI must not look like a typical AI-generated interface. Specifically:
- Do not use fonts that are closely associated with AI tools or generic developer dashboards (e.g., Inter as the sole or obvious workhorse, Geist).
- Avoid layout patterns, color applications, and compositional habits that produce the characteristic "AI slop" aesthetic - safe neutral palettes, low-contrast card grids, predictable sidebar + content splits with no visual personality.
- The dashboard must read as craft: deliberate typographic choices, considered hierarchy, a distinct visual point of view.

No logo, wordmark, or other brand assets are confirmed yet.

## Evidence on Hand

No testimonials, case studies, screenshots, or benchmark data available. Do not fabricate any.

## Product Principles

1. **Postgres is the source of truth** - job state, retry history, worker health, and auth all live in one place. Operational simplicity over distributed cleverness.
2. **The dashboard earns its keep** - it is not a bonus feature; it is the primary control plane. Every job operation should be inspectable, recoverable, and understandable from the UI.
3. **Multi-tenancy is structural, not cosmetic** - the org → project → queue → job hierarchy is load-bearing in the data model and must be equally legible in the interface.
4. **Craft over convention** - the UI should have a distinct, considered point of view. Avoid defaults that produce forgettable developer tooling aesthetics.
5. **Failure is first-class** - retries, dead-letter queues, and execution logs are not edge-case screens; they are the workflows operators live in under pressure.

## Accessibility & Inclusion

No product-specific requirement established yet. Aim for WCAG 2.1 AA as a baseline given the professional tool context.

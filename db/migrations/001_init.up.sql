-- Enable UUID generation
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ──────────────────────────────────────────────────────────────────
-- AUTH / IAM
-- ──────────────────────────────────────────────────────────────────
CREATE TABLE users (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,
    name          TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE organizations (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL,
    slug       TEXT        NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE organization_members (
    org_id     UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT        NOT NULL DEFAULT 'member' CHECK (role IN ('owner','admin','member')),
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);

-- ──────────────────────────────────────────────────────────────────
-- PROJECTS
-- ──────────────────────────────────────────────────────────────────
CREATE TABLE projects (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    slug         TEXT        NOT NULL,
    api_key_hash TEXT        NOT NULL UNIQUE,   -- hashed project API key
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, slug)
);

-- ──────────────────────────────────────────────────────────────────
-- RETRY POLICIES (own table so queues can share policies)
-- ──────────────────────────────────────────────────────────────────
CREATE TABLE retry_policies (
    id                UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name              TEXT    NOT NULL,
    strategy          TEXT    NOT NULL CHECK (strategy IN ('fixed','linear','exponential')),
    max_attempts      INT     NOT NULL DEFAULT 3 CHECK (max_attempts >= 1),
    initial_delay_ms  INT     NOT NULL DEFAULT 1000,
    max_delay_ms      INT     NOT NULL DEFAULT 60000,
    multiplier        NUMERIC(5,2) NOT NULL DEFAULT 2.0,   -- for exponential
    UNIQUE (project_id, name)
);

-- ──────────────────────────────────────────────────────────────────
-- QUEUES
-- ──────────────────────────────────────────────────────────────────
CREATE TABLE queues (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    retry_policy_id   UUID        REFERENCES retry_policies(id) ON DELETE SET NULL,
    name              TEXT        NOT NULL,
    description       TEXT,
    priority          INT         NOT NULL DEFAULT 5 CHECK (priority BETWEEN 1 AND 10),
    concurrency_limit INT         NOT NULL DEFAULT 10 CHECK (concurrency_limit >= 1),
    paused            BOOLEAN     NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

-- ──────────────────────────────────────────────────────────────────
-- JOBS
-- ──────────────────────────────────────────────────────────────────
CREATE TYPE job_status AS ENUM (
    'queued','scheduled','claimed','running','completed','failed','cancelled','dead'
);

CREATE TABLE jobs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    queue_id        UUID        NOT NULL REFERENCES queues(id) ON DELETE CASCADE,
    type            TEXT        NOT NULL,           -- handler/processor name
    payload         JSONB       NOT NULL DEFAULT '{}',
    status          job_status  NOT NULL DEFAULT 'queued',
    priority        INT         NOT NULL DEFAULT 5 CHECK (priority BETWEEN 1 AND 10),
    max_attempts    INT         NOT NULL DEFAULT 3,
    attempt_count   INT         NOT NULL DEFAULT 0,
    -- timing
    scheduled_at    TIMESTAMPTZ,                    -- NULL = immediate
    run_at          TIMESTAMPTZ NOT NULL DEFAULT now(), -- effective dispatch time
    timeout_secs    INT         NOT NULL DEFAULT 300,
    -- recurring
    cron_expression TEXT,
    next_run_at     TIMESTAMPTZ,
    -- batch
    batch_id        UUID,
    -- metadata
    idempotency_key TEXT        UNIQUE,
    tags            TEXT[]      NOT NULL DEFAULT '{}',
    -- result
    last_error      TEXT,
    result          JSONB,
    -- timestamps
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ,
    claimed_at      TIMESTAMPTZ,
    -- worker that owns this job right now
    claimed_by      UUID        -- FK to workers added below
);

-- Index for the hot polling path: unclaimed, due, ordered by priority DESC then run_at ASC
CREATE INDEX idx_jobs_poll ON jobs (queue_id, status, run_at, priority DESC)
    WHERE status IN ('queued','scheduled');

-- Index for monitoring / explorer
CREATE INDEX idx_jobs_status ON jobs (status, created_at DESC);
CREATE INDEX idx_jobs_batch  ON jobs (batch_id) WHERE batch_id IS NOT NULL;
CREATE INDEX idx_jobs_queue_created ON jobs (queue_id, created_at DESC);

-- ──────────────────────────────────────────────────────────────────
-- WORKERS
-- ──────────────────────────────────────────────────────────────────
CREATE TABLE workers (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    hostname          TEXT        NOT NULL,
    pid               INT         NOT NULL,
    version           TEXT,
    status            TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active','draining','offline')),
    concurrency       INT         NOT NULL DEFAULT 5,
    queue_ids         UUID[]      NOT NULL DEFAULT '{}',
    registered_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Add FK now that workers table exists
ALTER TABLE jobs ADD CONSTRAINT fk_jobs_worker
    FOREIGN KEY (claimed_by) REFERENCES workers(id) ON DELETE SET NULL;

CREATE INDEX idx_workers_project    ON workers (project_id, status);
CREATE INDEX idx_workers_heartbeat  ON workers (last_heartbeat_at) WHERE status = 'active';

-- ──────────────────────────────────────────────────────────────────
-- JOB EXECUTIONS (one row per attempt)
-- ──────────────────────────────────────────────────────────────────
CREATE TYPE execution_status AS ENUM ('running','completed','failed','timed_out','cancelled');

CREATE TABLE job_executions (
    id             UUID             PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id         UUID             NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    worker_id      UUID             REFERENCES workers(id) ON DELETE SET NULL,
    attempt_number INT              NOT NULL,
    status         execution_status NOT NULL DEFAULT 'running',
    started_at     TIMESTAMPTZ      NOT NULL DEFAULT now(),
    completed_at   TIMESTAMPTZ,
    duration_ms    INT,
    error_message  TEXT,
    result         JSONB,
    UNIQUE (job_id, attempt_number)
);

CREATE INDEX idx_executions_job    ON job_executions (job_id, attempt_number DESC);
CREATE INDEX idx_executions_worker ON job_executions (worker_id, started_at DESC);

-- ──────────────────────────────────────────────────────────────────
-- WORKER HEARTBEATS (time-series; partition in prod if needed)
-- ──────────────────────────────────────────────────────────────────
CREATE TABLE worker_heartbeats (
    id              BIGSERIAL   PRIMARY KEY,
    worker_id       UUID        NOT NULL REFERENCES workers(id) ON DELETE CASCADE,
    heartbeat_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    jobs_running    INT         NOT NULL DEFAULT 0,
    jobs_completed  INT         NOT NULL DEFAULT 0,
    metadata        JSONB       NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_heartbeats_worker ON worker_heartbeats (worker_id, heartbeat_at DESC);

-- ──────────────────────────────────────────────────────────────────
-- JOB LOGS
-- ──────────────────────────────────────────────────────────────────
CREATE TABLE job_logs (
    id           BIGSERIAL   PRIMARY KEY,
    job_id       UUID        NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    execution_id UUID        REFERENCES job_executions(id) ON DELETE SET NULL,
    level        TEXT        NOT NULL DEFAULT 'info' CHECK (level IN ('debug','info','warn','error')),
    message      TEXT        NOT NULL,
    metadata     JSONB       NOT NULL DEFAULT '{}',
    logged_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_logs_job    ON job_logs (job_id, logged_at DESC);
CREATE INDEX idx_logs_exec   ON job_logs (execution_id) WHERE execution_id IS NOT NULL;

-- ──────────────────────────────────────────────────────────────────
-- DEAD LETTER QUEUE
-- ──────────────────────────────────────────────────────────────────
CREATE TABLE dead_letter_queue (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id      UUID        NOT NULL UNIQUE REFERENCES jobs(id) ON DELETE CASCADE,
    queue_id    UUID        NOT NULL REFERENCES queues(id) ON DELETE CASCADE,
    final_error TEXT,
    attempts    INT         NOT NULL DEFAULT 0,
    moved_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,                        -- set when retried/discarded
    resolved_by UUID        REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX idx_dlq_queue   ON dead_letter_queue (queue_id, moved_at DESC);
CREATE INDEX idx_dlq_pending ON dead_letter_queue (moved_at DESC) WHERE resolved_at IS NULL;

-- ──────────────────────────────────────────────────────────────────
-- REFRESH TOKENS
-- ──────────────────────────────────────────────────────────────────
CREATE TABLE refresh_tokens (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT        NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

CREATE INDEX idx_tokens_user ON refresh_tokens (user_id, expires_at);

-- ──────────────────────────────────────────────────────────────────
-- updated_at trigger helper
-- ──────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN NEW.updated_at = now(); RETURN NEW; END; $$;

CREATE TRIGGER trg_users_updated_at
    BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_orgs_updated_at
    BEFORE UPDATE ON organizations FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_projects_updated_at
    BEFORE UPDATE ON projects FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_queues_updated_at
    BEFORE UPDATE ON queues FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_jobs_updated_at
    BEFORE UPDATE ON jobs FOR EACH ROW EXECUTE FUNCTION set_updated_at();

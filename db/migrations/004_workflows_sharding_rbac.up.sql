ALTER TABLE organization_members DROP CONSTRAINT IF EXISTS organization_members_role_check;
ALTER TABLE organization_members ADD CONSTRAINT organization_members_role_check
    CHECK (role IN ('owner','admin','member','viewer'));

ALTER TABLE queues ADD COLUMN IF NOT EXISTS shard_count INT NOT NULL DEFAULT 1
    CHECK (shard_count BETWEEN 1 AND 64);

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS shard INT NOT NULL DEFAULT 0
    CHECK (shard BETWEEN 0 AND 63);
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS partition_key TEXT;

CREATE TABLE IF NOT EXISTS job_dependencies (
    job_id            UUID        NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    depends_on_job_id UUID        NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (job_id, depends_on_job_id),
    CONSTRAINT job_dependencies_no_self_edge CHECK (job_id <> depends_on_job_id)
);

CREATE INDEX IF NOT EXISTS idx_job_deps_dependent ON job_dependencies (depends_on_job_id, job_id);

CREATE TABLE IF NOT EXISTS job_failure_summaries (
    job_id           UUID        PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
    summary          TEXT        NOT NULL,
    likely_cause     TEXT        NOT NULL,
    suggested_action TEXT        NOT NULL,
    category         TEXT        NOT NULL,
    confidence       TEXT        NOT NULL CHECK (confidence IN ('low','medium','high')),
    is_transient     BOOLEAN     NOT NULL DEFAULT false,
    model            TEXT        NOT NULL,
    input_hash       TEXT        NOT NULL,
    input_tokens     INT         NOT NULL DEFAULT 0,
    output_tokens    INT         NOT NULL DEFAULT 0,
    generated_by     UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_failure_summaries_created ON job_failure_summaries (created_at DESC);

DROP INDEX IF EXISTS idx_jobs_poll;
CREATE INDEX idx_jobs_poll ON jobs (queue_id, shard, run_at, priority DESC)
    WHERE status = 'queued';

CREATE INDEX IF NOT EXISTS idx_jobs_scheduled ON jobs (run_at)
    WHERE status = 'scheduled';

CREATE INDEX IF NOT EXISTS idx_jobs_blocked ON jobs (queue_id, created_at)
    WHERE status = 'blocked';

CREATE INDEX IF NOT EXISTS idx_jobs_reclaim ON jobs (claimed_at)
    WHERE claimed_at IS NOT NULL AND status IN ('claimed','running');

DROP TRIGGER IF EXISTS trg_failure_summaries_updated_at ON job_failure_summaries;
CREATE TRIGGER trg_failure_summaries_updated_at
    BEFORE UPDATE ON job_failure_summaries FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_failure_summaries_updated_at ON job_failure_summaries;

DROP TABLE IF EXISTS job_failure_summaries;
DROP TABLE IF EXISTS job_dependencies;

DROP INDEX IF EXISTS idx_jobs_reclaim;
DROP INDEX IF EXISTS idx_jobs_blocked;
DROP INDEX IF EXISTS idx_jobs_scheduled;
DROP INDEX IF EXISTS idx_jobs_poll;

CREATE INDEX idx_jobs_poll ON jobs (queue_id, status, run_at, priority DESC)
    WHERE status IN ('queued','scheduled');

ALTER TABLE jobs DROP COLUMN IF EXISTS partition_key;
ALTER TABLE jobs DROP COLUMN IF EXISTS shard;
ALTER TABLE queues DROP COLUMN IF EXISTS shard_count;

UPDATE organization_members SET role = 'member' WHERE role = 'viewer';
ALTER TABLE organization_members DROP CONSTRAINT IF EXISTS organization_members_role_check;
ALTER TABLE organization_members ADD CONSTRAINT organization_members_role_check
    CHECK (role IN ('owner','admin','member'));

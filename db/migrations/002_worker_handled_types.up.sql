ALTER TABLE workers ADD COLUMN handled_types TEXT[] NOT NULL DEFAULT '{}';

CREATE INDEX idx_workers_live ON workers (project_id, last_heartbeat_at DESC) WHERE status = 'active';

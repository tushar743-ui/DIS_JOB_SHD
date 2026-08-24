DROP INDEX IF EXISTS idx_workers_live;

ALTER TABLE workers DROP COLUMN IF EXISTS handled_types;

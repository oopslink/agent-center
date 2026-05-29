-- 0044_environment_worker.down.sql

DROP INDEX IF EXISTS idx_wce_worker_offset;
DROP TABLE IF EXISTS worker_control_events;

DROP INDEX IF EXISTS idx_env_workers_org;
DROP TABLE IF EXISTS env_workers;

-- 0004_observability_projections.down.sql — Phase 4 rollback

DROP INDEX IF EXISTS idx_proj_last_push;
DROP TABLE IF EXISTS task_execution_projections;

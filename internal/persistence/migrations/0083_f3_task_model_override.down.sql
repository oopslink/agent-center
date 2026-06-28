-- 0083_f3_task_model_override.down.sql — reverse of the up migration: drop the
-- per-task model-override column.
ALTER TABLE pm_tasks DROP COLUMN model;

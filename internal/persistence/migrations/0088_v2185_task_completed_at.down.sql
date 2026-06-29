-- 0088_v2185_task_completed_at.down.sql — reverse of the up migration: drop the
-- pm_tasks.completed_at column. Completion timestamps are lost on rollback; the plan
-- DONE-row time falls back to status_changed_at (the pre-0088 behavior).
ALTER TABLE pm_tasks DROP COLUMN completed_at;

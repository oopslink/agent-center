-- 0097_v228_task_recovery_reset_count.down.sql — reverse of the up migration: drop the
-- pm_tasks.recovery_reset_count column. The per-task recovery reset tally is lost on
-- rollback; reset_task falls back to an always-fresh budget (the pre-0097 behavior).
ALTER TABLE pm_tasks DROP COLUMN recovery_reset_count;

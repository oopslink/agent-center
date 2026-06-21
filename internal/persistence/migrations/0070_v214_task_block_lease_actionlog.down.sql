-- 0070_v214_task_block_lease_actionlog.down.sql — revert v2.14.0 I14/F2 migration A.
-- Reverse order: drop the action-log table (auto-drops its index) and the two
-- pm_tasks indexes, then drop the three added columns. Leaves pm_tasks exactly as
-- 0069 left it, so a Down→Up round-trip lands on an identical schema.
DROP TABLE pm_task_action_logs;
DROP INDEX idx_pm_tasks_assignee_running_blocked;
DROP INDEX idx_pm_tasks_assignee_status;
ALTER TABLE pm_tasks DROP COLUMN execution_lease_expires_at;
ALTER TABLE pm_tasks DROP COLUMN blocked_comment;
ALTER TABLE pm_tasks DROP COLUMN blocked_reason_type;

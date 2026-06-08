-- 0053 down — revert the Edit-Task tags + status_changed_at columns.
ALTER TABLE pm_issues DROP COLUMN status_changed_at;
ALTER TABLE pm_issues DROP COLUMN tags;
ALTER TABLE pm_tasks  DROP COLUMN status_changed_at;
ALTER TABLE pm_tasks  DROP COLUMN tags;

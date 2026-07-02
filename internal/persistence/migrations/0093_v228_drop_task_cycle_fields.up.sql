-- 0093_v228_drop_task_cycle_fields.up.sql — remove cycle-specific fields from tasks
-- These are now handled by the orchestration engine's node metadata.
ALTER TABLE pm_tasks DROP COLUMN branch;
ALTER TABLE pm_tasks DROP COLUMN base;
ALTER TABLE pm_tasks DROP COLUMN skip_merge_check;
ALTER TABLE pm_tasks DROP COLUMN role;

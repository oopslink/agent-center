-- 0066_v213_task_cycle_meta.down.sql — revert v2.13.0 I18/F2: drop the cycle-node
-- git metadata columns from pm_tasks.
ALTER TABLE pm_tasks DROP COLUMN skip_merge_check;
ALTER TABLE pm_tasks DROP COLUMN base;
ALTER TABLE pm_tasks DROP COLUMN branch;

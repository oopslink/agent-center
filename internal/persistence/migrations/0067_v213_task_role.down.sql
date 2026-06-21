-- 0067_v213_task_role.down.sql — revert v2.13.0 I18/F3: drop the cycle-node role
-- column from pm_tasks.
ALTER TABLE pm_tasks DROP COLUMN role;

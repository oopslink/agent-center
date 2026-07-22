-- 0114_remove_task_delivered_status.down.sql
--
-- Data-only, intentionally irreversible. After status='delivered' rows are
-- collapsed into completed there is no reliable way to distinguish them from
-- legitimate completed tasks.

SELECT 1;

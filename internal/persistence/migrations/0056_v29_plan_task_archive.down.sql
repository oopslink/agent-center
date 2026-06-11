-- 0056 down — revert the v2.9 P3 Stage B task archived columns. (No pm_plans DDL
-- was added on up — the 'archived' status value needs no schema change.)
ALTER TABLE pm_tasks DROP COLUMN archived_by;
ALTER TABLE pm_tasks DROP COLUMN archived_at;

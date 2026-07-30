DROP INDEX IF EXISTS idx_pm_plans_archived_at;
UPDATE pm_plans
SET status = CASE
    WHEN legacy_status != '' THEN legacy_status
    WHEN status = 'pending' THEN 'draft'
    WHEN status = 'paused' THEN 'draft'
    WHEN status = 'discarded' THEN 'draft'
    ELSE status
END;
ALTER TABLE pm_plans DROP COLUMN legacy_status;
ALTER TABLE pm_plans DROP COLUMN archived_by;
ALTER TABLE pm_plans DROP COLUMN archived_at;

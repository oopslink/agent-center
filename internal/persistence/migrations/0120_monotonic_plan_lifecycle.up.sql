-- ADR-0055: Plan status becomes monotonic; archive is an orthogonal marker.
ALTER TABLE pm_plans ADD COLUMN archived_at TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_plans ADD COLUMN archived_by TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_plans ADD COLUMN legacy_status TEXT NOT NULL DEFAULT '';

UPDATE pm_plans
SET legacy_status = status,
    archived_at = CASE WHEN status = 'archived' THEN updated_at ELSE archived_at END,
    archived_by = CASE WHEN status = 'archived' THEN 'system:migration' ELSE archived_by END,
    status = CASE
        WHEN status = 'draft' THEN 'pending'
        WHEN status = 'archived' AND NOT EXISTS (
            SELECT 1 FROM pm_tasks t
            WHERE t.plan_id = pm_plans.id
              AND t.status NOT IN ('completed', 'discarded')
        ) THEN 'done'
        WHEN status = 'archived' THEN 'discarded'
        ELSE status
    END;

CREATE INDEX IF NOT EXISTS idx_pm_plans_archived_at ON pm_plans(archived_at);

-- `reopened` was a mutable rewrite of completed history. Existing non-terminal
-- legacy rows are normalized to open; future retries append a follow-up task or
-- remediation stage instead of changing the old task.
UPDATE pm_tasks SET status = 'open' WHERE status = 'reopened';

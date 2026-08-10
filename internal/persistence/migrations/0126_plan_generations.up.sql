-- 0126_plan_generations.up.sql — immutable Plan Generation snapshots + active pointer.
ALTER TABLE pm_plans ADD COLUMN active_generation_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_pm_plans_active_generation
    ON pm_plans(active_generation_id)
    WHERE active_generation_id != '';

CREATE TABLE IF NOT EXISTS pm_plan_generations (
    id                       TEXT PRIMARY KEY,
    plan_id                  TEXT NOT NULL,
    parent_generation_id     TEXT NOT NULL DEFAULT '',
    reason                   TEXT NOT NULL,
    evidence                 TEXT NOT NULL,
    creator_ref              TEXT NOT NULL,
    diff_json                TEXT NOT NULL,
    snapshot_json            TEXT NOT NULL,
    idempotency_key          TEXT NOT NULL,
    request_fingerprint      TEXT NOT NULL,
    dispatched_task_ids_json TEXT NOT NULL DEFAULT '[]',
    created_at               TEXT NOT NULL,
    UNIQUE(plan_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_pm_plan_generations_plan_created
    ON pm_plan_generations(plan_id, created_at, id);

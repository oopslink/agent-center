-- 0150: durable hierarchical wake buckets and aggregated retry intents.
CREATE TABLE IF NOT EXISTS pm_progress_wake_bucket_states (
    scope_key         TEXT PRIMARY KEY,
    tokens            INTEGER NOT NULL,
    capacity          INTEGER NOT NULL,
    refill_per_minute INTEGER NOT NULL,
    last_refill_at    TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pm_progress_suppressed_wakes (
    id                TEXT PRIMARY KEY,
    organization_id   TEXT NOT NULL DEFAULT '',
    owner_ref         TEXT NOT NULL,
    severity          TEXT NOT NULL,
    channel           TEXT NOT NULL DEFAULT '',
    plan_ids_json     TEXT NOT NULL DEFAULT '[]',
    attempt_count     INTEGER NOT NULL DEFAULT 0,
    next_attempt_at   TEXT NOT NULL,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_pm_progress_suppressed_wakes_due
    ON pm_progress_suppressed_wakes(next_attempt_at, id);

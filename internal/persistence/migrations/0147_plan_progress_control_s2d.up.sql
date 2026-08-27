-- 0147_plan_progress_control_s2d.up.sql — S2D active-active lease/fencing,
-- watchdog heartbeats, and wake token-bucket diagnostics.
CREATE TABLE IF NOT EXISTS pm_progress_leases (
    scope          TEXT NOT NULL,
    plan_id        TEXT NOT NULL,
    holder_id      TEXT NOT NULL,
    fencing_token  INTEGER NOT NULL,
    acquired_at    TEXT NOT NULL,
    renewed_at     TEXT NOT NULL,
    expires_at     TEXT NOT NULL,
    PRIMARY KEY(scope, plan_id)
);
CREATE INDEX IF NOT EXISTS idx_pm_progress_leases_expires
    ON pm_progress_leases(expires_at);

CREATE TABLE IF NOT EXISTS pm_progress_watchdog_heartbeats (
    plan_id       TEXT NOT NULL,
    component     TEXT NOT NULL,
    last_seen_at  TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    PRIMARY KEY(plan_id, component)
);
CREATE INDEX IF NOT EXISTS idx_pm_progress_watchdog_heartbeats_last_seen
    ON pm_progress_watchdog_heartbeats(last_seen_at);

CREATE TABLE IF NOT EXISTS pm_progress_wake_bucket_diagnostics (
    id                TEXT PRIMARY KEY,
    plan_id           TEXT NOT NULL,
    organization_id   TEXT NOT NULL DEFAULT '',
    owner_ref         TEXT NOT NULL,
    severity          TEXT NOT NULL,
    allowed           INTEGER NOT NULL,
    reason            TEXT NOT NULL DEFAULT '',
    tokens_before     INTEGER NOT NULL,
    tokens_after      INTEGER NOT NULL,
    capacity          INTEGER NOT NULL,
    reserved_p0       INTEGER NOT NULL,
    refill_per_minute INTEGER NOT NULL,
    attempted_at      TEXT NOT NULL,
    next_refill_at    TEXT NOT NULL,
    evidence_json     TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_pm_progress_wake_bucket_diag_plan
    ON pm_progress_wake_bucket_diagnostics(plan_id, attempted_at DESC);

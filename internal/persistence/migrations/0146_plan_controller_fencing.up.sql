-- 0146_plan_controller_fencing.up.sql — S2D active-active Plan controller control plane.
--
-- Per-Plan controller leases carry a monotonic fencing_token. All controller
-- writes must validate the token before mutating graph/node state or appending
-- side effects. Inbox and outbox ids are stable idempotency keys.

CREATE TABLE IF NOT EXISTS pm_plan_controller_leases (
    plan_id           TEXT PRIMARY KEY,
    owner_instance_id TEXT NOT NULL,
    fencing_token     INTEGER NOT NULL,
    expires_at        TEXT NOT NULL,
    last_renewed_at   TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pm_plan_controller_leases_expires
    ON pm_plan_controller_leases(expires_at);

CREATE TABLE IF NOT EXISTS pm_plan_controller_inbox (
    event_id       TEXT PRIMARY KEY,
    plan_id        TEXT NOT NULL,
    fencing_token  INTEGER NOT NULL,
    processed_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pm_plan_controller_inbox_plan
    ON pm_plan_controller_inbox(plan_id);

CREATE TABLE IF NOT EXISTS pm_watchdog_observations (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL,
    severity    TEXT NOT NULL,
    plan_id     TEXT NOT NULL DEFAULT '',
    detail      TEXT NOT NULL DEFAULT '{}',
    observed_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pm_watchdog_observations_kind_time
    ON pm_watchdog_observations(kind, observed_at);

CREATE TABLE IF NOT EXISTS pm_wake_queue (
    id             TEXT PRIMARY KEY,
    incident_id    TEXT NOT NULL,
    org_id         TEXT NOT NULL,
    severity       TEXT NOT NULL,
    channel        TEXT NOT NULL,
    plan_id        TEXT NOT NULL DEFAULT '',
    payload        TEXT NOT NULL DEFAULT '{}',
    status         TEXT NOT NULL DEFAULT 'pending',
    created_at     TEXT NOT NULL,
    delivered_at   TEXT,
    overflowed_at  TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_pm_wake_queue_incident
    ON pm_wake_queue(incident_id);

CREATE INDEX IF NOT EXISTS idx_pm_wake_queue_pending
    ON pm_wake_queue(status, org_id, severity, channel, created_at);

CREATE TABLE IF NOT EXISTS pm_wake_overflows (
    org_id         TEXT NOT NULL,
    channel        TEXT NOT NULL,
    count          INTEGER NOT NULL,
    max_severity   TEXT NOT NULL,
    oldest_at      TEXT NOT NULL,
    affected_plans TEXT NOT NULL DEFAULT '[]',
    updated_at     TEXT NOT NULL,
    PRIMARY KEY (org_id, channel)
);

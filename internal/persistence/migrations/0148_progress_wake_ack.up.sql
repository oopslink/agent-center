CREATE TABLE IF NOT EXISTS pm_progress_wakes (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL,
    task_id TEXT NOT NULL DEFAULT '',
    node_id TEXT NOT NULL DEFAULT '',
    owner_ref TEXT NOT NULL,
    owner_display TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    requested_at TEXT NOT NULL,
    delivered_at TEXT NOT NULL DEFAULT '',
    acknowledged_at TEXT NOT NULL DEFAULT '',
    ack_fact_ref TEXT NOT NULL DEFAULT '',
    ack_deadline TEXT NOT NULL,
    max_hold_duration_ms INTEGER NOT NULL DEFAULT 0,
    escalation_level INTEGER NOT NULL DEFAULT 0,
    next_escalation_at TEXT NOT NULL DEFAULT '',
    organization_owner_ref TEXT NOT NULL DEFAULT '',
    UNIQUE(idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_pm_progress_wakes_unacked ON pm_progress_wakes (ack_deadline, acknowledged_at);

CREATE TABLE IF NOT EXISTS pm_progress_obligations (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL,
    task_id TEXT NOT NULL DEFAULT '',
    node_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    owner_ref TEXT NOT NULL,
    owner_display TEXT NOT NULL,
    deadline_at TEXT NOT NULL,
    ack_required INTEGER NOT NULL DEFAULT 1,
    acked_at TEXT NOT NULL DEFAULT '',
    escalate_to_ref TEXT NOT NULL,
    escalation_deadline_at TEXT NOT NULL,
    source_fact_refs TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    UNIQUE(plan_id, task_id, kind, owner_ref, deadline_at)
);
CREATE INDEX IF NOT EXISTS idx_pm_progress_obligations_open ON pm_progress_obligations (plan_id, status, deadline_at);

CREATE TABLE IF NOT EXISTS pm_progress_incidents (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL,
    task_id TEXT NOT NULL DEFAULT '',
    node_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    severity TEXT NOT NULL,
    owner_ref TEXT NOT NULL,
    owner_display TEXT NOT NULL,
    summary TEXT NOT NULL,
    source_ref TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(plan_id, task_id, kind, source_ref)
);
CREATE INDEX IF NOT EXISTS idx_pm_progress_incidents_open ON pm_progress_incidents (plan_id, status, severity);

CREATE TABLE IF NOT EXISTS pm_progress_holds (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL,
    task_id TEXT NOT NULL DEFAULT '',
    node_id TEXT NOT NULL DEFAULT '',
    reason_kind TEXT NOT NULL,
    reason_id TEXT NOT NULL,
    owner_ref TEXT NOT NULL,
    owner_display TEXT NOT NULL,
    entered_at TEXT NOT NULL,
    hold_ack_deadline TEXT NOT NULL,
    max_hold_duration_ms INTEGER NOT NULL DEFAULT 0,
    escalation_level INTEGER NOT NULL DEFAULT 0,
    next_escalation_at TEXT NOT NULL,
    blocks_dispatch INTEGER NOT NULL DEFAULT 1,
    blocks_acceptance INTEGER NOT NULL DEFAULT 1,
    blocks_completion INTEGER NOT NULL DEFAULT 1,
    released_at TEXT NOT NULL DEFAULT '',
    release_fact_ref TEXT NOT NULL DEFAULT '',
    UNIQUE(plan_id, task_id, reason_kind, reason_id)
);
CREATE INDEX IF NOT EXISTS idx_pm_progress_holds_open_plan ON pm_progress_holds (plan_id, released_at);
CREATE INDEX IF NOT EXISTS idx_pm_progress_holds_due ON pm_progress_holds (released_at, next_escalation_at, entered_at);

CREATE TABLE IF NOT EXISTS pm_progress_escalations (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL,
    task_id TEXT NOT NULL DEFAULT '',
    node_id TEXT NOT NULL DEFAULT '',
    obligation_id TEXT NOT NULL DEFAULT '',
    hold_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    severity TEXT NOT NULL,
    escalate_to_ref TEXT NOT NULL,
    deadline_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(obligation_id, hold_id, deadline_at, escalate_to_ref, kind)
);

CREATE TABLE IF NOT EXISTS pm_progress_leases (
    lease_scope TEXT PRIMARY KEY,
    fencing_token INTEGER NOT NULL,
    holder_id TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pm_progress_checkpoints (
    name TEXT PRIMARY KEY,
    cursor TEXT NOT NULL DEFAULT '',
    heartbeat_at TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'ok'
);

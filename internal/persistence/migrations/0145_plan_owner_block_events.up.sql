ALTER TABLE pm_plans ADD COLUMN owner_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_plans ADD COLUMN backup_owner_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_plans ADD COLUMN attention_status TEXT NOT NULL DEFAULT 'none';
ALTER TABLE pm_plans ADD COLUMN attention_since TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_plans ADD COLUMN last_attention_event_id TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_plans ADD COLUMN recovery_notify_after_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE pm_plans ADD COLUMN recovery_remind_after_seconds INTEGER NOT NULL DEFAULT 900;
ALTER TABLE pm_plans ADD COLUMN recovery_escalate_after_seconds INTEGER NOT NULL DEFAULT 3600;

UPDATE pm_plans
SET owner_ref = creator_ref
WHERE owner_ref = '' AND status IN ('pending','running','paused');

CREATE TABLE IF NOT EXISTS pm_plan_block_events (
    event_id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    plan_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    node_id TEXT NOT NULL DEFAULT '',
    execution_id TEXT NOT NULL DEFAULT '',
    block_version INTEGER NOT NULL,
    blocked_reason TEXT NOT NULL,
    reason_type TEXT NOT NULL,
    blocked_by TEXT NOT NULL,
    blocked_at TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1,
    effective INTEGER NOT NULL DEFAULT 1,
    impacted_downstream_json TEXT NOT NULL DEFAULT '[]',
    owner_ref TEXT NOT NULL,
    next_actions_json TEXT NOT NULL,
    acknowledged_at TEXT NOT NULL DEFAULT '',
    acknowledged_by TEXT NOT NULL DEFAULT '',
    resolved_at TEXT NOT NULL DEFAULT '',
    resolved_by TEXT NOT NULL DEFAULT '',
    resolution_kind TEXT NOT NULL DEFAULT '',
    resolution_note TEXT NOT NULL DEFAULT '',
    notification_state TEXT NOT NULL DEFAULT 'pending',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(plan_id, generation_id, task_id, block_version)
);

CREATE INDEX IF NOT EXISTS idx_pm_plan_block_events_plan_active
    ON pm_plan_block_events(plan_id, active, effective, resolved_at);
CREATE INDEX IF NOT EXISTS idx_pm_plan_block_events_notify_retry
    ON pm_plan_block_events(notification_state, updated_at);

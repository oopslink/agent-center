ALTER TABLE pm_plan_block_events ADD COLUMN notification_claimed_at TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_pm_plan_block_events_notify_claim
    ON pm_plan_block_events(notification_state, notification_claimed_at);

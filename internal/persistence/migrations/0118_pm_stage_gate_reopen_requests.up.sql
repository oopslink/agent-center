CREATE TABLE IF NOT EXISTS pm_stage_gate_reopen_requests (
    plan_id TEXT NOT NULL,
    stage_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    actor_ref TEXT NOT NULL,
    reason TEXT NOT NULL,
    prior_gate_task_id TEXT NOT NULL,
    prior_round INTEGER NOT NULL,
    new_round INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (plan_id, stage_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_pm_stage_gate_reopen_requests_stage
    ON pm_stage_gate_reopen_requests (plan_id, stage_id, created_at);

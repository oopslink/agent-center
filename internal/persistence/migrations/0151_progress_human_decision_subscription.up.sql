-- 0151: durable automatic re-evaluation for human decisions waiting on facts.
CREATE TABLE IF NOT EXISTS pm_progress_prerequisite_subscriptions (
    id                   TEXT PRIMARY KEY,
    plan_id              TEXT NOT NULL,
    decision_task_id     TEXT NOT NULL,
    prerequisite_task_id TEXT NOT NULL,
    owner_ref            TEXT NOT NULL,
    next_deadline_at     TEXT NOT NULL,
    action               TEXT NOT NULL,
    reason_fact_ref      TEXT NOT NULL,
    status               TEXT NOT NULL,
    created_at           TEXT NOT NULL,
    resolved_at          TEXT NOT NULL DEFAULT '',
    decision_fact_ref    TEXT NOT NULL DEFAULT '',
    UNIQUE(plan_id, decision_task_id, prerequisite_task_id, reason_fact_ref)
);
CREATE INDEX IF NOT EXISTS idx_pm_progress_prerequisite_open
    ON pm_progress_prerequisite_subscriptions(status, next_deadline_at);

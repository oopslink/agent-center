CREATE TABLE IF NOT EXISTS pm_plan_supersessions (
    plan_id            TEXT NOT NULL,
    superseded_task_id TEXT NOT NULL,
    successor_task_id  TEXT NOT NULL,
    reason             TEXT NOT NULL DEFAULT '',
    actor_ref          TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL,
    PRIMARY KEY (plan_id, superseded_task_id),
    CHECK (superseded_task_id <> successor_task_id)
);

CREATE INDEX IF NOT EXISTS idx_pm_plan_supersessions_successor
    ON pm_plan_supersessions(plan_id, successor_task_id);

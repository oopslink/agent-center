CREATE TABLE agent_execution_mirrors (
    agent_id             TEXT NOT NULL,
    task_id              TEXT NOT NULL,
    worker_id            TEXT NOT NULL,
    execution_mode       TEXT NOT NULL,
    executor_id          TEXT NOT NULL DEFAULT '',
    executor_state       TEXT NOT NULL DEFAULT '',
    delivery_state       TEXT NOT NULL DEFAULT '',
    required_next_action TEXT NOT NULL DEFAULT '',
    branch               TEXT NOT NULL DEFAULT '',
    head_sha             TEXT NOT NULL DEFAULT '',
    worktree             TEXT NOT NULL DEFAULT '',
    row_json             TEXT NOT NULL,
    snapshot_json        TEXT NOT NULL,
    observed_at          TEXT NOT NULL,
    updated_at           TEXT NOT NULL,
    PRIMARY KEY (agent_id, task_id)
);

CREATE INDEX idx_agent_execution_mirrors_task
    ON agent_execution_mirrors (task_id);

CREATE INDEX idx_agent_execution_mirrors_executor
    ON agent_execution_mirrors (executor_id)
    WHERE executor_id <> '';

CREATE INDEX idx_agent_execution_mirrors_observed
    ON agent_execution_mirrors (observed_at);

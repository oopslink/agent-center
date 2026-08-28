-- 0145_insight_concurrency_observations.up.sql — S2A durable heartbeat observations.
--
-- DuckDB Insight is a disposable read model. Heartbeat slot snapshots must first
-- land in SQLite so the projector can replay them after restart/rebuild.
CREATE TABLE agent_concurrency_observations (
    id          TEXT PRIMARY KEY,
    worker_id   TEXT NOT NULL,
    agent_id    TEXT NOT NULL,
    snapshot    TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

CREATE INDEX idx_aco_observed_at ON agent_concurrency_observations (observed_at);
CREATE INDEX idx_aco_agent_time ON agent_concurrency_observations (agent_id, observed_at);

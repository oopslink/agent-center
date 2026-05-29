-- 0042_v27_agents.up.sql — v2.7 C1 (ADR-0049, task #99)
--
-- Agent bounded context: the long-running Agent product entity. There is NO
-- AgentRun table by design (ADR-0049 §1) — observability is the lifecycle
-- state + the AgentActivityEvent stream (C2). AgentWorkItem + activity tables
-- land in C2 (migration tbd).
--
-- Worker binding (worker_id) is immutable in v2.7 (changing worker = new
-- agent). profile env_vars + skills are JSON; lifecycle is the reconciled
-- intent. App-layer referential integrity per conventions § 9.w.

CREATE TABLE agents (
    id               TEXT PRIMARY KEY,
    organization_id  TEXT NOT NULL,
    name             TEXT NOT NULL,
    description      TEXT,
    model            TEXT,
    cli              TEXT,
    env_vars         TEXT NOT NULL DEFAULT '{}',   -- JSON object
    skills           TEXT NOT NULL DEFAULT '[]',   -- JSON array
    worker_id        TEXT NOT NULL,                -- immutable runtime binding
    lifecycle        TEXT NOT NULL,                -- stopped|running|stopping|resetting|error
    lifecycle_error  TEXT,
    created_by       TEXT NOT NULL,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    version          INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_agents_org    ON agents (organization_id);
CREATE INDEX idx_agents_worker ON agents (worker_id);

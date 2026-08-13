CREATE TABLE IF NOT EXISTS team_rule_load_audits (
    execution_id TEXT NOT NULL DEFAULT '',
    planning_session_id TEXT NOT NULL DEFAULT '',
    team_id TEXT NOT NULL,
    team_memory_commit TEXT NOT NULL,
    rule_slug TEXT NOT NULL,
    phase TEXT NOT NULL,
    agent_id TEXT NOT NULL DEFAULT '',
    loaded_at TEXT NOT NULL,
    PRIMARY KEY (execution_id, planning_session_id, team_id, team_memory_commit, rule_slug)
);

CREATE INDEX IF NOT EXISTS idx_team_rule_load_audits_execution
    ON team_rule_load_audits (execution_id)
    WHERE execution_id <> '';

CREATE INDEX IF NOT EXISTS idx_team_rule_load_audits_planning_session
    ON team_rule_load_audits (planning_session_id)
    WHERE planning_session_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS uq_team_rule_load_audits_execution_rule
    ON team_rule_load_audits (execution_id, team_id, team_memory_commit, rule_slug)
    WHERE execution_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS uq_team_rule_load_audits_planning_rule
    ON team_rule_load_audits (planning_session_id, team_id, team_memory_commit, rule_slug)
    WHERE execution_id = '' AND planning_session_id <> '';

-- 0123_team_memory_policy.up.sql — ADR-0057 controlled Team Memory writes.
--
-- The Team aggregate owns the write policy: default proposal_only, with an
-- explicit set of agent member refs that may review as curator when mode is
-- curator_auto. Grants are stored separately so the existing teams table does
-- not need a dialect-specific rebuild.
CREATE TABLE IF NOT EXISTS team_memory_policies (
    team_id    TEXT PRIMARY KEY,
    mode       TEXT NOT NULL DEFAULT 'proposal_only',
    updated_at TEXT NOT NULL,
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    CHECK (mode IN ('proposal_only', 'curator_auto'))
);

CREATE TABLE IF NOT EXISTS team_memory_policy_curators (
    team_id   TEXT NOT NULL,
    agent_ref TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (team_id, agent_ref),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_team_memory_policy_curators_team
    ON team_memory_policy_curators(team_id);

-- Team Memory controlled writes: Team-owned curator policy and durable
-- projection checkpoint. Canonical proposal/review content remains in the team
-- Git repo; these tables only hold authorization policy and projector progress.
CREATE TABLE IF NOT EXISTS team_memory_policies (
    team_id    TEXT PRIMARY KEY,
    mode       TEXT NOT NULL DEFAULT 'proposal_only',
    updated_at TEXT NOT NULL,
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS team_memory_curators (
    team_id    TEXT NOT NULL,
    agent_ref  TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (team_id, agent_ref),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS team_memory_observability_checkpoints (
    team_id               TEXT PRIMARY KEY,
    last_projected_commit TEXT NOT NULL DEFAULT '',
    updated_at            TEXT NOT NULL
);

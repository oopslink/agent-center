-- 0125_team_memory_observability_checkpoint.up.sql — track Team Memory event projection.
CREATE TABLE IF NOT EXISTS team_memory_observability_checkpoints (
    team_id               TEXT PRIMARY KEY,
    last_projected_commit TEXT NOT NULL DEFAULT '',
    updated_at            TEXT NOT NULL,
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
);

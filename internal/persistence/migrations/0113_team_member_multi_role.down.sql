DROP TRIGGER IF EXISTS trg_team_members_agent_one_team_insert;
DROP INDEX IF EXISTS idx_team_members_agent_ref;
DROP INDEX IF EXISTS idx_team_members_team;

CREATE TABLE IF NOT EXISTS team_members_old (
    team_id TEXT NOT NULL,
    member_ref TEXT NOT NULL,
    member_kind TEXT NOT NULL,
    role TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (team_id, member_ref),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (team_id, role) REFERENCES team_roles(team_id, role)
);

INSERT OR IGNORE INTO team_members_old (team_id, member_ref, member_kind, role, created_at)
SELECT team_id, member_ref, member_kind, role, MIN(created_at)
FROM team_members
GROUP BY team_id, member_ref;

DROP TABLE team_members;
ALTER TABLE team_members_old RENAME TO team_members;

CREATE UNIQUE INDEX IF NOT EXISTS idx_team_members_agent_exclusive
    ON team_members(member_ref) WHERE member_kind = 'agent';
CREATE INDEX IF NOT EXISTS idx_team_members_team ON team_members(team_id);

-- Allow one member to carry multiple declared roles within the same team while
-- keeping the product invariant that an agent belongs to at most one team.

DROP INDEX IF EXISTS idx_team_members_agent_exclusive;
DROP INDEX IF EXISTS idx_team_members_team;

CREATE TABLE IF NOT EXISTS team_members_new (
    team_id TEXT NOT NULL,
    member_ref TEXT NOT NULL,
    member_kind TEXT NOT NULL,
    role TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (team_id, member_ref, role),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (team_id, role) REFERENCES team_roles(team_id, role)
);

INSERT OR IGNORE INTO team_members_new (team_id, member_ref, member_kind, role, created_at)
SELECT team_id, member_ref, member_kind, role, created_at
FROM team_members;

DROP TABLE team_members;
ALTER TABLE team_members_new RENAME TO team_members;

CREATE INDEX IF NOT EXISTS idx_team_members_team ON team_members(team_id);
CREATE INDEX IF NOT EXISTS idx_team_members_agent_ref
    ON team_members(member_ref) WHERE member_kind = 'agent';

CREATE TRIGGER IF NOT EXISTS trg_team_members_agent_one_team_insert
BEFORE INSERT ON team_members
WHEN NEW.member_kind = 'agent'
 AND EXISTS (
    SELECT 1
    FROM team_members
    WHERE member_ref = NEW.member_ref
      AND member_kind = 'agent'
      AND team_id <> NEW.team_id
 )
BEGIN
    SELECT RAISE(ABORT, 'agent already in another team');
END;

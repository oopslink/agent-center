-- 0096_v229_teams.up.sql — Team BC data layer (Team S1, design §2/§4/§9).
--
-- Four tables:
--   teams          — the Team aggregate (id / org / name).
--   team_roles     — per-team role declarations. Roles are NOT hardcoded; each
--                    team template declares its own roles (design §9), each
--                    carrying its cli / model / capability_tags / concurrency.
--   team_members   — membership rows. member_kind drives the agent-exclusivity
--                    index below.
--   team_projects  — Team ↔ Project association (many-to-many).
CREATE TABLE IF NOT EXISTS teams (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1
);

-- A team name is unique within its org (mirrors pm_templates org+name).
CREATE UNIQUE INDEX IF NOT EXISTS idx_teams_org_name ON teams(org_id, name);

-- Per-team role config. Roles are declared by the team template (design §9),
-- never hardcoded — so the (cli, model, capability_tags, max_concurrency) tuple
-- lives per (team, role). capability_tags is a JSON array.
CREATE TABLE IF NOT EXISTS team_roles (
    team_id         TEXT NOT NULL,
    role            TEXT NOT NULL,
    cli             TEXT NOT NULL DEFAULT '',
    model           TEXT NOT NULL DEFAULT '',
    capability_tags TEXT NOT NULL DEFAULT '[]',
    max_concurrency INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL,
    PRIMARY KEY (team_id, role),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
);

-- Team membership. member_ref is an identity ref ("agent:<id>" / "user:<id>");
-- member_kind ('agent' | 'human') is derived from the ref at write time so the
-- partial unique index below can key off it. The composite FK to team_roles
-- enforces that a member's role has been declared for that team (design §9).
CREATE TABLE IF NOT EXISTS team_members (
    team_id     TEXT NOT NULL,
    member_ref  TEXT NOT NULL,
    member_kind TEXT NOT NULL,
    role        TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    PRIMARY KEY (team_id, member_ref),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (team_id, role) REFERENCES team_roles(team_id, role)
);

-- Agent exclusivity (design §2/§4): an agent belongs to AT MOST ONE team. The
-- partial index spans ALL teams but only agent rows, so a second team trying to
-- add the same agent fails with a UNIQUE violation. Humans are intentionally
-- excluded from this index → a human may join many teams.
CREATE UNIQUE INDEX IF NOT EXISTS idx_team_members_agent_exclusive
    ON team_members(member_ref) WHERE member_kind = 'agent';

CREATE INDEX IF NOT EXISTS idx_team_members_team ON team_members(team_id);

-- Team ↔ Project association.
CREATE TABLE IF NOT EXISTS team_projects (
    team_id    TEXT NOT NULL,
    project_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (team_id, project_id),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_team_projects_project ON team_projects(project_id);

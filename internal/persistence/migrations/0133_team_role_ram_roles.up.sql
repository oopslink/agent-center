-- 0133_team_role_ram_roles.up.sql -- Team functional roles map to RAM roles.

CREATE TABLE IF NOT EXISTS team_role_ram_role_mappings (
    team_id      TEXT NOT NULL,
    team_role    TEXT NOT NULL,
    ram_role_id  TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    created_by   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (team_id, team_role, ram_role_id),
    FOREIGN KEY (team_id, team_role) REFERENCES team_roles(team_id, role) ON DELETE CASCADE,
    FOREIGN KEY (ram_role_id) REFERENCES authorization_roles(id)
);
CREATE INDEX IF NOT EXISTS idx_team_role_ram_role_reverse ON team_role_ram_role_mappings(ram_role_id, team_id, team_role);

CREATE TABLE IF NOT EXISTS team_role_ram_role_versions (
    team_id TEXT NOT NULL, team_role TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TEXT NOT NULL, updated_by TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (team_id, team_role),
    FOREIGN KEY (team_id, team_role) REFERENCES team_roles(team_id, role) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS team_role_ram_role_audit_events (
    id TEXT PRIMARY KEY, org_id TEXT NOT NULL, team_id TEXT NOT NULL, team_role TEXT NOT NULL,
    actor_ref TEXT NOT NULL, previous_role_ids TEXT NOT NULL DEFAULT '[]', next_role_ids TEXT NOT NULL DEFAULT '[]',
    previous_version INTEGER NOT NULL, next_version INTEGER NOT NULL, created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_team_role_ram_role_audit_lookup ON team_role_ram_role_audit_events(team_id, team_role, created_at DESC);

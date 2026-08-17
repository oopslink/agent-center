-- 0132_access_profile_versions.up.sql -- persistent versioned access profiles.

CREATE TABLE IF NOT EXISTS access_profiles (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    disabled_at TEXT,
    created_by  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_access_profiles_org_name
    ON access_profiles(org_id, name)
    WHERE disabled_at IS NULL;

CREATE TABLE IF NOT EXISTS access_profile_versions (
    profile_id       TEXT NOT NULL,
    version          INTEGER NOT NULL,
    permissions_json TEXT NOT NULL,
    risk             TEXT NOT NULL CHECK (risk IN ('low', 'medium', 'high')),
    created_by       TEXT NOT NULL,
    created_at       TEXT NOT NULL,
    PRIMARY KEY (profile_id, version)
);

CREATE INDEX IF NOT EXISTS idx_access_profile_versions_profile
    ON access_profile_versions(profile_id, version DESC);

INSERT OR IGNORE INTO access_profiles
    (id, org_id, name, description, created_by, created_at, updated_at)
VALUES
    ('team-basic', '', 'Team basic', 'Read team metadata and memory.', 'system', datetime('now'), datetime('now')),
    ('team-contributor', '', 'Team contributor', 'Read/write team work and propose memory.', 'system', datetime('now'), datetime('now')),
    ('team-curator', '', 'Team curator', 'Review team memory in addition to contributor access.', 'system', datetime('now'), datetime('now'));

INSERT OR IGNORE INTO access_profile_versions
    (profile_id, version, permissions_json, risk, created_by, created_at)
VALUES
    ('team-basic', 1, '["team.read","team.memory.read"]', 'low', 'system', datetime('now')),
    ('team-contributor', 1, '["team.read","team.write","team.memory.read","team.memory.propose"]', 'medium', 'system', datetime('now')),
    ('team-curator', 1, '["team.read","team.write","team.memory.read","team.memory.propose"]', 'medium', 'system', datetime('now')),
    ('team-curator', 2, '["team.read","team.write","team.memory.read","team.memory.propose","team.memory.review"]', 'high', 'system', datetime('now'));

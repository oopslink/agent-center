-- 0143_ram_role_classification_contract.up.sql
--
-- Freeze the RAM Role classification contract:
--   * reusable roles are kind=system/custom and visibility=reusable
--   * managed direct-grant roles are kind=managed and visibility=internal
--   * system roles are global seed/migration data and remain immutable in APIs

DROP INDEX IF EXISTS idx_authorization_roles_custom_org_stable_key;
DROP INDEX IF EXISTS idx_authorization_roles_system_stable_key;
DROP INDEX IF EXISTS idx_authorization_roles_org;
DROP INDEX IF EXISTS idx_authorization_roles_custom_org_name;
DROP INDEX IF EXISTS idx_authorization_roles_system_name;

CREATE TEMP TABLE authorization_role_versions_0143 AS
SELECT * FROM authorization_role_versions;

CREATE TABLE authorization_roles_new (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL CHECK (kind IN ('system', 'custom', 'managed')),
    visibility  TEXT NOT NULL DEFAULT 'reusable' CHECK (visibility IN ('reusable', 'internal')),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    revoked_at  TEXT,
    version     INTEGER NOT NULL DEFAULT 1,
    stable_key  TEXT DEFAULT NULL,
    scope_kind  TEXT DEFAULT NULL,
    CHECK (
        (kind = 'system' AND org_id = '' AND visibility = 'reusable')
        OR (kind = 'custom' AND org_id <> '' AND visibility = 'reusable')
        OR (kind = 'managed' AND org_id <> '' AND visibility = 'internal')
    ),
    CHECK (NOT (visibility = 'reusable' AND name LIKE 'Access grant%'))
);

INSERT INTO authorization_roles_new
    (id, org_id, kind, visibility, name, description, created_by, created_at, updated_at, revoked_at, version, stable_key, scope_kind)
SELECT
    id,
    org_id,
    CASE WHEN kind = 'custom' AND name LIKE 'Access grant%' THEN 'managed' ELSE kind END,
    CASE WHEN kind = 'custom' AND name LIKE 'Access grant%' THEN 'internal' ELSE 'reusable' END,
    CASE WHEN kind = 'custom' AND name LIKE 'Access grant%' THEN 'Managed direct grant' || substr(name, length('Access grant') + 1) ELSE name END,
    description,
    created_by,
    created_at,
    updated_at,
    revoked_at,
    version,
    stable_key,
    scope_kind
FROM authorization_roles;

DROP TABLE authorization_roles;
ALTER TABLE authorization_roles_new RENAME TO authorization_roles;

INSERT OR IGNORE INTO authorization_role_versions
SELECT * FROM authorization_role_versions_0143;

DROP TABLE authorization_role_versions_0143;

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_system_name
    ON authorization_roles(name)
    WHERE kind = 'system' AND visibility = 'reusable' AND revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_custom_org_name
    ON authorization_roles(org_id, name)
    WHERE kind = 'custom' AND visibility = 'reusable' AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_authorization_roles_org
    ON authorization_roles(org_id, kind, visibility)
    WHERE revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_system_stable_key
    ON authorization_roles(COALESCE(NULLIF(stable_key, ''), id))
    WHERE kind = 'system' AND visibility = 'reusable' AND revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_custom_org_stable_key
    ON authorization_roles(org_id, COALESCE(NULLIF(stable_key, ''), id))
    WHERE kind = 'custom' AND visibility = 'reusable' AND revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS authorization_system_role_contracts (
    role_id                         TEXT PRIMARY KEY,
    stable_key                      TEXT NOT NULL,
    version                         INTEGER NOT NULL,
    name                            TEXT NOT NULL,
    description                     TEXT NOT NULL,
    scope_kind                      TEXT NOT NULL,
    risk                            TEXT NOT NULL CHECK (risk IN ('low', 'medium', 'high')),
    responsibility_scenario         TEXT NOT NULL,
    least_privilege_permissions_json TEXT NOT NULL,
    reuse_entrypoints_json          TEXT NOT NULL,
    maintained_by                   TEXT NOT NULL CHECK (maintained_by = 'release_seed_migration'),
    updated_at                      TEXT NOT NULL
);

INSERT OR REPLACE INTO authorization_system_role_contracts
    (role_id, stable_key, version, name, description, scope_kind, risk, responsibility_scenario, least_privilege_permissions_json, reuse_entrypoints_json, maintained_by, updated_at)
VALUES
    ('team-basic', 'team-basic', 1, 'Team basic', 'Read team metadata and memory.', 'team', 'low',
     'Default read-only RAM Role for Team Roles that need to inspect team configuration and team memory without write authority.',
     '["team.memory.read","team.read"]',
     '["team.role.ram_role_keys","team.role.ram_roles.mapping"]',
     'release_seed_migration', datetime('now')),
    ('team-contributor', 'team-contributor', 1, 'Team contributor', 'Read/write team work and propose memory.', 'team', 'medium',
     'Reusable RAM Role for Team Roles that participate in team work and may propose, but not approve, team memory changes.',
     '["team.memory.propose","team.memory.read","team.read","team.write"]',
     '["team.role.ram_role_keys","team.role.ram_roles.mapping"]',
     'release_seed_migration', datetime('now')),
    ('team-curator', 'team-curator', 1, 'Team curator', 'Review team memory in addition to contributor access.', 'team', 'high',
     'Reusable RAM Role for Team Roles that curate promoted team memory while retaining contributor capabilities.',
     '["team.memory.propose","team.memory.read","team.memory.review","team.read","team.write"]',
     '["team.role.ram_role_keys","team.role.ram_roles.mapping"]',
     'release_seed_migration', datetime('now'));

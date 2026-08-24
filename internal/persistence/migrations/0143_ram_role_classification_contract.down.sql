DROP TABLE IF EXISTS authorization_system_role_contracts;

DROP INDEX IF EXISTS idx_authorization_roles_custom_org_stable_key;
DROP INDEX IF EXISTS idx_authorization_roles_system_stable_key;
DROP INDEX IF EXISTS idx_authorization_roles_org;
DROP INDEX IF EXISTS idx_authorization_roles_custom_org_name;
DROP INDEX IF EXISTS idx_authorization_roles_system_name;

CREATE TEMP TABLE authorization_role_versions_0143 AS
SELECT * FROM authorization_role_versions;

CREATE TABLE authorization_roles_old (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL CHECK (kind IN ('system', 'custom')),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    revoked_at  TEXT,
    version     INTEGER NOT NULL DEFAULT 1,
    stable_key  TEXT DEFAULT NULL,
    scope_kind  TEXT DEFAULT NULL
);

INSERT INTO authorization_roles_old
    (id, org_id, kind, name, description, created_by, created_at, updated_at, revoked_at, version, stable_key, scope_kind)
SELECT
    id,
    org_id,
    kind,
    name,
    description,
    created_by,
    created_at,
    updated_at,
    revoked_at,
    version,
    stable_key,
    scope_kind
FROM authorization_roles
WHERE kind IN ('system', 'custom');

DROP TABLE authorization_roles;
ALTER TABLE authorization_roles_old RENAME TO authorization_roles;

INSERT OR IGNORE INTO authorization_role_versions
SELECT * FROM authorization_role_versions_0143;

DROP TABLE authorization_role_versions_0143;

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_system_name
    ON authorization_roles(name)
    WHERE kind = 'system' AND revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_custom_org_name
    ON authorization_roles(org_id, name)
    WHERE kind = 'custom' AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_authorization_roles_org
    ON authorization_roles(org_id, kind)
    WHERE revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_system_stable_key
    ON authorization_roles(COALESCE(NULLIF(stable_key, ''), id))
    WHERE kind = 'system' AND revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_custom_org_stable_key
    ON authorization_roles(org_id, COALESCE(NULLIF(stable_key, ''), id))
    WHERE kind = 'custom' AND revoked_at IS NULL;

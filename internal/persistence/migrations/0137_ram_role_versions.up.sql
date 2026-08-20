-- 0137_ram_role_versions.up.sql -- RAM Role history and access-profile retirement.

CREATE TABLE IF NOT EXISTS authorization_role_versions (
    role_id          TEXT NOT NULL,
    version          INTEGER NOT NULL CHECK (version > 0),
    permissions_json TEXT NOT NULL,
    risk             TEXT NOT NULL CHECK (risk IN ('low', 'medium', 'high')),
    created_by       TEXT NOT NULL,
    created_at       TEXT NOT NULL,
    PRIMARY KEY (role_id, version),
    FOREIGN KEY (role_id) REFERENCES authorization_roles(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_authorization_role_versions_role
    ON authorization_role_versions(role_id, version DESC);

INSERT OR IGNORE INTO authorization_roles
    (id, org_id, kind, name, description, created_by, created_at, updated_at, revoked_at, version)
SELECT
    p.id,
    p.org_id,
    CASE WHEN p.org_id = '' THEN 'system' ELSE 'custom' END,
    p.name,
    p.description,
    p.created_by,
    p.created_at,
    p.updated_at,
    p.disabled_at,
    COALESCE((SELECT MAX(v.version) FROM access_profile_versions v WHERE v.profile_id = p.id), 1)
FROM access_profiles p;

UPDATE authorization_roles
SET version = (
    SELECT COALESCE(MAX(v.version), authorization_roles.version)
    FROM access_profile_versions v
    WHERE v.profile_id = authorization_roles.id
)
WHERE id IN (SELECT profile_id FROM access_profile_versions);

INSERT OR IGNORE INTO authorization_role_versions
    (role_id, version, permissions_json, risk, created_by, created_at)
SELECT profile_id, version, permissions_json, risk, created_by, created_at
FROM access_profile_versions;

DELETE FROM authorization_role_permissions
WHERE role_id IN (SELECT profile_id FROM access_profile_versions);

INSERT OR IGNORE INTO authorization_role_permissions
    (role_id, permission_key, resource_kind, delegatable, created_at)
SELECT latest.profile_id, json_each.value, 'team', 0, datetime('now')
FROM (
    SELECT profile_id, permissions_json
    FROM access_profile_versions v
    WHERE version = (SELECT MAX(version) FROM access_profile_versions mx WHERE mx.profile_id = v.profile_id)
) latest, json_each(latest.permissions_json);

DROP INDEX IF EXISTS idx_access_profile_versions_profile;
DROP INDEX IF EXISTS idx_access_profiles_org_name;
DROP TABLE IF EXISTS access_profile_versions;
DROP TABLE IF EXISTS access_profiles;

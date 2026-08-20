-- Restore the v136 access-profile storage before removing the v137 history
-- table. Every role with version rows came from an access profile (or was
-- created through the replacement RAM-role API after v137); retaining both
-- forms makes a downgrade data-preserving.
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

INSERT OR REPLACE INTO access_profiles
    (id, org_id, name, description, disabled_at, created_by, created_at, updated_at)
SELECT
    r.id, r.org_id, r.name, r.description, r.revoked_at,
    r.created_by, r.created_at, r.updated_at
FROM authorization_roles r
WHERE EXISTS (
    SELECT 1 FROM authorization_role_versions v WHERE v.role_id = r.id
);

INSERT OR REPLACE INTO access_profile_versions
    (profile_id, version, permissions_json, risk, created_by, created_at)
SELECT role_id, version, permissions_json, risk, created_by, created_at
FROM authorization_role_versions;

DROP TABLE IF EXISTS authorization_role_versions;

ALTER TABLE team_roles ADD COLUMN access_requirements_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE team_roles ADD COLUMN access_profiles_json TEXT NOT NULL DEFAULT '[]';

CREATE TABLE IF NOT EXISTS access_profiles (
    id          TEXT NOT NULL,
    org_id      TEXT NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (org_id, id)
);

CREATE TABLE IF NOT EXISTS access_profile_versions (
    org_id           TEXT NOT NULL,
    profile_id       TEXT NOT NULL,
    version          INTEGER NOT NULL,
    role_id          TEXT NOT NULL,
    permissions_json TEXT NOT NULL DEFAULT '[]',
    created_at       TEXT NOT NULL,
    PRIMARY KEY (org_id, profile_id, version),
    FOREIGN KEY (org_id, profile_id) REFERENCES access_profiles(org_id, id) ON DELETE CASCADE
);

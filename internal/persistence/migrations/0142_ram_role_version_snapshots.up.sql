-- 0142_ram_role_version_snapshots.up.sql -- immutable RAM Role display snapshots.

ALTER TABLE authorization_role_versions ADD COLUMN stable_key TEXT;
ALTER TABLE authorization_role_versions ADD COLUMN name TEXT;
ALTER TABLE authorization_role_versions ADD COLUMN description TEXT;
ALTER TABLE authorization_role_versions ADD COLUMN scope_kind TEXT;

-- Older versions predate immutable display snapshots. Backfill the best
-- available value once; every version written after this migration records its
-- own complete state and no longer projects mutable authorization_roles data.
UPDATE authorization_role_versions
SET stable_key = (SELECT COALESCE(NULLIF(r.stable_key, ''), r.id) FROM authorization_roles r WHERE r.id = role_id),
    name = (SELECT r.name FROM authorization_roles r WHERE r.id = role_id),
    description = (SELECT COALESCE(r.description, '') FROM authorization_roles r WHERE r.id = role_id),
    scope_kind = (SELECT COALESCE(NULLIF(r.scope_kind, ''), 'org') FROM authorization_roles r WHERE r.id = role_id)
WHERE stable_key IS NULL OR name IS NULL OR description IS NULL OR scope_kind IS NULL;

-- 0142_ram_role_version_metadata.up.sql -- immutable RAM Role display snapshots.

ALTER TABLE authorization_role_versions ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE authorization_role_versions ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE authorization_role_versions ADD COLUMN scope_kind TEXT NOT NULL DEFAULT 'org';
ALTER TABLE authorization_role_versions ADD COLUMN stable_key TEXT NOT NULL DEFAULT '';

-- Historical schemas did not retain display metadata per version. Backfill the
-- only authoritative values available so every existing row has a complete
-- snapshot; subsequent versions write their own immutable values.
UPDATE authorization_role_versions
SET name = COALESCE((
        SELECT ar.name FROM authorization_roles ar
        WHERE ar.id = authorization_role_versions.role_id
    ), ''),
    description = COALESCE((
        SELECT ar.description FROM authorization_roles ar
        WHERE ar.id = authorization_role_versions.role_id
    ), ''),
    scope_kind = COALESCE((
        SELECT NULLIF(ar.scope_kind, '') FROM authorization_roles ar
        WHERE ar.id = authorization_role_versions.role_id
    ), 'org'),
    stable_key = COALESCE((
        SELECT NULLIF(ar.stable_key, '') FROM authorization_roles ar
        WHERE ar.id = authorization_role_versions.role_id
    ), role_id);

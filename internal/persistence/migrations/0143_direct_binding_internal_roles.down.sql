-- 0143_direct_binding_internal_roles.down.sql -- restore legacy visible role-access carriers.

DROP INDEX IF EXISTS idx_authorization_roles_managed_internal;
DROP INDEX IF EXISTS idx_authorization_roles_custom_org_stable_key;
DROP INDEX IF EXISTS idx_authorization_roles_custom_org_name;

UPDATE authorization_roles
SET kind = 'custom',
    managed = 0,
    visibility = 'visible',
    updated_at = datetime('now'),
    version = version + 1
WHERE id LIKE 'role-access-%'
  AND kind = 'managed'
  AND managed = 1
  AND visibility = 'internal';

PRAGMA writable_schema = ON;

UPDATE sqlite_schema
SET sql = replace(sql, "kind        TEXT NOT NULL CHECK (kind IN ('system', 'custom', 'managed'))", "kind        TEXT NOT NULL CHECK (kind IN ('system', 'custom'))")
WHERE type = 'table'
  AND name = 'authorization_roles'
  AND sql LIKE '%kind        TEXT NOT NULL CHECK (kind IN (''system'', ''custom'', ''managed''))%';

PRAGMA writable_schema = OFF;
PRAGMA schema_version = 142;

ALTER TABLE authorization_roles DROP COLUMN visibility;
ALTER TABLE authorization_roles DROP COLUMN managed;

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_custom_org_name
    ON authorization_roles(org_id, name)
    WHERE kind = 'custom' AND revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_custom_org_stable_key
    ON authorization_roles(org_id, COALESCE(NULLIF(stable_key, ''), id))
    WHERE kind = 'custom' AND revoked_at IS NULL;

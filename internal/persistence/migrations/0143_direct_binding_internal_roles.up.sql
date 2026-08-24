-- 0143_direct_binding_internal_roles.up.sql -- direct grants use internal managed carriers.
--
-- Legacy Access batch grants created visible custom roles with ids like
-- role-access-*. Those rows are implementation carriers for one-permission
-- direct assignments, not user-managed RAM Roles. If a Team Role references one
-- of those rows, stop immediately so an operator can resolve the ambiguous
-- Team Role mapping explicitly instead of silently hiding it.

CREATE TEMP TABLE IF NOT EXISTS _t1499_role_access_guard (
    blocker_count INTEGER NOT NULL CHECK (blocker_count = 0)
);

DELETE FROM _t1499_role_access_guard;

INSERT INTO _t1499_role_access_guard(blocker_count)
SELECT COUNT(*)
FROM team_role_ram_role_mappings m
JOIN authorization_roles r ON r.id = m.ram_role_id
WHERE r.id LIKE 'role-access-%'
  AND r.kind = 'custom'
  AND r.revoked_at IS NULL;

ALTER TABLE authorization_roles ADD COLUMN managed INTEGER NOT NULL DEFAULT 0 CHECK (managed IN (0, 1));
ALTER TABLE authorization_roles ADD COLUMN visibility TEXT NOT NULL DEFAULT 'visible' CHECK (visibility IN ('visible', 'internal'));

PRAGMA writable_schema = ON;

UPDATE sqlite_schema
SET sql = replace(sql, "kind        TEXT NOT NULL CHECK (kind IN ('system', 'custom'))", "kind        TEXT NOT NULL CHECK (kind IN ('system', 'custom', 'managed'))")
WHERE type = 'table'
  AND name = 'authorization_roles'
  AND sql LIKE '%kind        TEXT NOT NULL CHECK (kind IN (''system'', ''custom''))%';

PRAGMA writable_schema = OFF;
PRAGMA schema_version = 143;

UPDATE authorization_roles
SET kind = 'managed',
    managed = 1,
    visibility = 'internal',
    updated_at = datetime('now'),
    version = version + 1
WHERE id LIKE 'role-access-%'
  AND kind = 'custom'
  AND revoked_at IS NULL;

DROP INDEX IF EXISTS idx_authorization_roles_custom_org_name;
DROP INDEX IF EXISTS idx_authorization_roles_custom_org_stable_key;

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_custom_org_name
    ON authorization_roles(org_id, name)
    WHERE kind = 'custom' AND visibility = 'visible' AND revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_custom_org_stable_key
    ON authorization_roles(org_id, COALESCE(NULLIF(stable_key, ''), id))
    WHERE kind = 'custom' AND visibility = 'visible' AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_authorization_roles_managed_internal
    ON authorization_roles(org_id, id)
    WHERE kind = 'managed' AND managed = 1 AND visibility = 'internal' AND revoked_at IS NULL;

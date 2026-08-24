-- 0143_direct_binding_internal_roles.up.sql -- direct grants use internal managed carriers.
--
-- Legacy Access batch grants created visible custom roles with ids like
-- role-access-*. Only genuine one-permission direct carriers may be hidden.
-- The Go migrator preflight fails closed before this SQL when a role-access row
-- is ambiguous, including Team Role references and non-single-permission rows.

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

WITH direct_carriers AS (
    SELECT r.id
    FROM authorization_roles r
    JOIN authorization_role_permissions arp ON arp.role_id = r.id
    LEFT JOIN team_role_ram_role_mappings trm ON trm.ram_role_id = r.id
    WHERE r.id LIKE 'role-access-%'
      AND r.kind = 'custom'
      AND r.visibility = 'visible'
      AND r.revoked_at IS NULL
    GROUP BY r.id
    HAVING COUNT(arp.permission_key) = 1
       AND COUNT(trm.ram_role_id) = 0
)
UPDATE authorization_roles
SET kind = 'managed',
    managed = 1,
    visibility = 'internal',
    updated_at = datetime('now'),
    version = version + 1
WHERE id IN (SELECT id FROM direct_carriers)
  AND kind = 'custom'
  AND visibility = 'visible'
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

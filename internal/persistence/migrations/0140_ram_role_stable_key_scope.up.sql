-- 0140_ram_role_stable_key_scope.up.sql -- v4 RAM Role CRUD display fields.

ALTER TABLE authorization_roles ADD COLUMN stable_key TEXT DEFAULT NULL;
ALTER TABLE authorization_roles ADD COLUMN scope_kind TEXT DEFAULT NULL;

UPDATE authorization_roles
SET stable_key = id
WHERE stable_key IS NULL OR stable_key = '';

UPDATE authorization_roles
SET scope_kind = COALESCE((
    SELECT arp.resource_kind
    FROM authorization_role_permissions arp
    WHERE arp.role_id = authorization_roles.id
    ORDER BY arp.resource_kind
    LIMIT 1
), 'org')
WHERE scope_kind IS NULL OR scope_kind = '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_system_stable_key
    ON authorization_roles(COALESCE(NULLIF(stable_key, ''), id))
    WHERE kind = 'system' AND revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_custom_org_stable_key
    ON authorization_roles(org_id, COALESCE(NULLIF(stable_key, ''), id))
    WHERE kind = 'custom' AND revoked_at IS NULL;

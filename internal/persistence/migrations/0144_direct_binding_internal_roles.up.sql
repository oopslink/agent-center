-- 0144_direct_binding_internal_roles.up.sql -- migrate legacy direct carriers.
--
-- role-access-* rows were historically visible custom RAM Roles. Only
-- fail-closed preflight-approved single-permission carriers are internalized;
-- assignments and permission rows are preserved verbatim.

UPDATE authorization_roles
SET kind = 'managed',
    visibility = 'internal',
    name = CASE
        WHEN name LIKE 'Access grant%' THEN 'Managed direct grant' || substr(name, length('Access grant') + 1)
        ELSE name
    END,
    updated_at = datetime('now'),
    version = version + 1
WHERE id IN (
    SELECT r.id
    FROM authorization_roles r
    JOIN authorization_role_permissions arp ON arp.role_id = r.id
    LEFT JOIN team_role_ram_role_mappings trm ON trm.ram_role_id = r.id
    WHERE r.id LIKE 'role-access-%'
      AND r.kind = 'custom'
      AND COALESCE(NULLIF(r.visibility, ''), 'reusable') = 'reusable'
      AND r.revoked_at IS NULL
    GROUP BY r.id
    HAVING COUNT(arp.permission_key) = 1
       AND COUNT(trm.ram_role_id) = 0
)
  AND kind = 'custom'
  AND COALESCE(NULLIF(visibility, ''), 'reusable') = 'reusable'
  AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_authorization_roles_managed_internal
    ON authorization_roles(org_id, id)
    WHERE kind = 'managed' AND visibility = 'internal' AND revoked_at IS NULL;

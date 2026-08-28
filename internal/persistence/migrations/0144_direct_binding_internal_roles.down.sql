-- 0144_direct_binding_internal_roles.down.sql -- restore migrated legacy carriers.

DROP INDEX IF EXISTS idx_authorization_roles_managed_internal;

UPDATE authorization_roles
SET kind = 'custom',
    visibility = 'reusable',
    updated_at = datetime('now'),
    version = version + 1
WHERE id LIKE 'role-access-%'
  AND kind = 'managed'
  AND visibility = 'internal'
  AND revoked_at IS NULL;

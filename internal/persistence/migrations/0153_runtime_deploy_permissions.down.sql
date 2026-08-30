DELETE FROM authorization_role_permissions
WHERE role_id = 'sys-admin-token'
  AND permission_key IN ('runtime.status.read', 'runtime.deploy')
  AND resource_kind = 'worker';

DELETE FROM permission_definitions
WHERE key IN ('runtime.status.read', 'runtime.deploy');

DELETE FROM authorization_role_permissions
WHERE role_id IN ('sys-org-owner', 'sys-org-admin', 'sys-org-member')
  AND permission_key = 'template.read';

DELETE FROM permission_definitions
WHERE key = 'template.read';

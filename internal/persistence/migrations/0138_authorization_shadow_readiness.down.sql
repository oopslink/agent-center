DELETE FROM authorization_role_permissions
WHERE permission_key = 'template.read'
  AND role_id IN ('sys-org-owner', 'sys-org-admin', 'sys-org-member');

DELETE FROM permission_definitions
WHERE key IN ('task.start.self', 'task.heartbeat.self', 'template.read');

DROP TABLE IF EXISTS authorization_shadow_readiness;

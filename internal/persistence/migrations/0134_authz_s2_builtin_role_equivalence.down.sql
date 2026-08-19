DELETE FROM authorization_role_permissions
WHERE role_id IN ('sys-team-web-owner', 'sys-team-web-admin', 'sys-team-web-member')
   OR (role_id IN ('sys-org-owner', 'sys-org-admin', 'sys-org-member')
       AND permission_key IN ('coderepo.workspace.read', 'ai_runtime.catalog.read', 'ai_runtime.catalog.export', 'team.create', 'template.write'));

DELETE FROM authorization_roles
WHERE id IN ('sys-team-web-owner', 'sys-team-web-admin', 'sys-team-web-member');

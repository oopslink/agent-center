DELETE FROM authorization_role_permissions
WHERE role_id IN ('team-basic', 'team-contributor', 'team-curator');

DELETE FROM authorization_roles
WHERE id IN ('team-basic', 'team-contributor', 'team-curator')
  AND kind = 'system'
  AND org_id = '';

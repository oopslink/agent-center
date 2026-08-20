DROP INDEX IF EXISTS idx_authorization_readiness_gates_observed;
DROP TABLE IF EXISTS authorization_readiness_gates;
DELETE FROM authorization_role_assignments WHERE role_id = 'sys-background-project-writer';
DELETE FROM authorization_role_permissions WHERE role_id = 'sys-background-project-writer';
DELETE FROM authorization_roles WHERE id = 'sys-background-project-writer';

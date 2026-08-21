DROP INDEX IF EXISTS idx_authorization_roles_custom_org_stable_key;
DROP INDEX IF EXISTS idx_authorization_roles_system_stable_key;
ALTER TABLE authorization_roles DROP COLUMN scope_kind;
ALTER TABLE authorization_roles DROP COLUMN stable_key;

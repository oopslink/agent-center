-- 0129_unified_authorization.down.sql -- remove unified authorization data core.

DROP TABLE IF EXISTS authorization_audit_events;
DROP TABLE IF EXISTS authorization_idempotency_keys;
DROP TABLE IF EXISTS authorization_role_assignments;
DROP TABLE IF EXISTS authorization_role_permissions;
DROP TABLE IF EXISTS authorization_roles;
DROP TABLE IF EXISTS permission_definitions;

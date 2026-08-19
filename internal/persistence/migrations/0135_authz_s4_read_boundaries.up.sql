-- 0135_authz_s4_read_boundaries.up.sql
--
-- Adds the org-scoped template read permission used by S4 read-side RAM gates.

INSERT OR IGNORE INTO permission_definitions
    (key, category, resource_kinds_json, actions_json, legacy_sources_json, created_at)
VALUES
    ('template.read', 'access', '["org"]', '["read"]', '["members"]', datetime('now'));

INSERT OR IGNORE INTO authorization_role_permissions
    (role_id, permission_key, resource_kind, delegatable, created_at)
VALUES
    ('sys-org-owner', 'template.read', 'org', 1, datetime('now')),
    ('sys-org-admin', 'template.read', 'org', 0, datetime('now')),
    ('sys-org-member', 'template.read', 'org', 0, datetime('now'));

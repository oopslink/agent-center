INSERT OR IGNORE INTO permission_definitions
    (key, category, resource_kinds_json, actions_json, legacy_sources_json, created_at)
VALUES
    ('runtime.status.read', 'access', '["worker"]', '["read"]', '["admin_tokens.owner"]', datetime('now')),
    ('runtime.deploy', 'access', '["worker"]', '["deploy"]', '["admin_tokens.owner"]', datetime('now'));

INSERT OR IGNORE INTO authorization_role_permissions
    (role_id, permission_key, resource_kind, delegatable, created_at)
VALUES
    ('sys-admin-token', 'runtime.status.read', 'worker', 0, datetime('now')),
    ('sys-admin-token', 'runtime.deploy', 'worker', 0, datetime('now'));

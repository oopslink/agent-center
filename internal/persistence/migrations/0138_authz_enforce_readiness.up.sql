-- 0138_authz_enforce_readiness.up.sql -- close registry gaps required before enforce cutover.

INSERT OR IGNORE INTO permission_definitions
    (key, category, resource_kinds_json, actions_json, legacy_sources_json, created_at)
VALUES
    ('task.start.self', 'access', '["task"]', '["start"]', '["pm_tasks.assignee"]', datetime('now')),
    ('task.heartbeat.self', 'access', '["task"]', '["heartbeat"]', '["pm_tasks.assignee"]', datetime('now')),
    ('template.read', 'access', '["org"]', '["read"]', '["members"]', datetime('now'));

INSERT OR IGNORE INTO authorization_role_permissions
    (role_id, permission_key, resource_kind, delegatable, created_at)
VALUES
    ('sys-org-owner', 'template.read', 'org', 1, datetime('now')),
    ('sys-org-admin', 'template.read', 'org', 0, datetime('now')),
    ('sys-org-member', 'template.read', 'org', 0, datetime('now'));

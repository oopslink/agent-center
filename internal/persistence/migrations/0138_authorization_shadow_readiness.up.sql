CREATE TABLE IF NOT EXISTS authorization_shadow_readiness (
  id TEXT PRIMARY KEY,
  mode TEXT NOT NULL,
  window_started_at TEXT NOT NULL,
  window_ended_at TEXT NOT NULL,
  transports_json TEXT NOT NULL,
  checks INTEGER NOT NULL DEFAULT 0,
  mismatches INTEGER NOT NULL DEFAULT 0,
  legacy_only INTEGER NOT NULL DEFAULT 0,
  equivalent_only INTEGER NOT NULL DEFAULT 0,
  ready INTEGER NOT NULL DEFAULT 0,
  reason TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);

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

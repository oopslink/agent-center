-- 0094_v228_templates.up.sql — Template management (workspace-level reusable workflow templates)
CREATE TABLE IF NOT EXISTS pm_templates (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL DEFAULT '',
    name       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    content    TEXT NOT NULL,
    is_builtin INTEGER NOT NULL DEFAULT 0,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_pm_templates_org_id ON pm_templates(org_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pm_templates_org_name ON pm_templates(org_id, name) WHERE is_builtin = 0;

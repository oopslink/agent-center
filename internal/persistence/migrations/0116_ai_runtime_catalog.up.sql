CREATE TABLE IF NOT EXISTS ai_runtime_catalogs (
    org_id TEXT PRIMARY KEY,
    revision INTEGER NOT NULL DEFAULT 0,
    default_profile_id TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS ai_runtime_clis (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    key TEXT NOT NULL,
    display_name TEXT NOT NULL,
    executable TEXT NOT NULL,
    version_constraint TEXT NOT NULL DEFAULT '',
    required_features_json TEXT NOT NULL DEFAULT '[]',
    parameter_schema_json TEXT NOT NULL DEFAULT '{}',
    enabled INTEGER NOT NULL DEFAULT 1,
    system INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(org_id, key)
);
CREATE INDEX IF NOT EXISTS idx_ai_runtime_clis_org ON ai_runtime_clis(org_id);

ALTER TABLE pm_model_catalog ADD COLUMN runtime_key TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_model_catalog ADD COLUMN compatible_cli_keys_json TEXT NOT NULL DEFAULT '["codex"]';
ALTER TABLE pm_model_catalog ADD COLUMN default_parameters_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE pm_model_catalog ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1;
UPDATE pm_model_catalog SET runtime_key = model_id WHERE runtime_key = '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_pm_model_catalog_org_runtime_key ON pm_model_catalog(org_id, runtime_key);

CREATE TABLE IF NOT EXISTS ai_runtime_profiles (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    key TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    cli_key TEXT NOT NULL,
    model_key TEXT NOT NULL,
    parameters_json TEXT NOT NULL DEFAULT '{}',
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(org_id, key)
);
CREATE INDEX IF NOT EXISTS idx_ai_runtime_profiles_org ON ai_runtime_profiles(org_id);

CREATE TABLE IF NOT EXISTS ai_runtime_audit_log (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    actor TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_key TEXT NOT NULL,
    action TEXT NOT NULL,
    before_json TEXT NOT NULL DEFAULT 'null',
    after_json TEXT NOT NULL DEFAULT 'null',
    revision INTEGER NOT NULL,
    occurred_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_runtime_audit_org_revision ON ai_runtime_audit_log(org_id, revision);

-- Definitions are catalog data, not Worker capabilities. Seed per existing org,
-- including orgs that only have legacy model rows.
INSERT OR IGNORE INTO ai_runtime_catalogs(org_id)
SELECT id FROM organizations WHERE deleted_at IS NULL;
INSERT OR IGNORE INTO ai_runtime_catalogs(org_id)
SELECT DISTINCT org_id FROM pm_model_catalog;
INSERT OR IGNORE INTO ai_runtime_clis
    (id, org_id, key, display_name, executable, parameter_schema_json, enabled, system, created_at, updated_at)
SELECT 'cli-codex-' || org_id, org_id, 'codex', 'Codex', 'codex', '{"type":"object"}', 1, 1,
       strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM ai_runtime_catalogs;
INSERT OR IGNORE INTO ai_runtime_clis
    (id, org_id, key, display_name, executable, parameter_schema_json, enabled, system, created_at, updated_at)
SELECT 'cli-claude-code-' || org_id, org_id, 'claude-code', 'Claude Code', 'claude', '{"type":"object"}', 1, 1,
       strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM ai_runtime_catalogs;

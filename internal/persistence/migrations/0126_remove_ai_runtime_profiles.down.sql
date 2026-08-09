ALTER TABLE ai_runtime_catalogs ADD COLUMN default_profile_id TEXT NOT NULL DEFAULT '';

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

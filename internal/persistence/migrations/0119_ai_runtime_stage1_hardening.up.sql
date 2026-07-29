ALTER TABLE ai_runtime_profiles ADD COLUMN version INTEGER NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS ai_runtime_execution_snapshots (
    execution_id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    snapshot_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_runtime_execution_snapshots_org
    ON ai_runtime_execution_snapshots(org_id);

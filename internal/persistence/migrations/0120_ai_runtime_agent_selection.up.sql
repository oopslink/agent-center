CREATE TABLE ai_runtime_agent_selections (
    agent_id TEXT PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    org_id TEXT NOT NULL,
    selection_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_ai_runtime_agent_selections_org ON ai_runtime_agent_selections(org_id);

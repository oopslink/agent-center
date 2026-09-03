CREATE TABLE collaboration_effects (
  effect_id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  target_task_id TEXT NOT NULL,
  source_agent_ref TEXT NOT NULL DEFAULT '',
  target_agent_ref TEXT NOT NULL DEFAULT '',
  relation_type TEXT NOT NULL,
  polarity TEXT NOT NULL,
  magnitude INTEGER NOT NULL CHECK (magnitude BETWEEN 1 AND 3),
  confidence TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  rule_version TEXT NOT NULL,
  evidence_event_ids TEXT NOT NULL,
  before_state TEXT NOT NULL,
  after_state TEXT NOT NULL,
  explanation_key TEXT NOT NULL,
  UNIQUE(rule_version, project_id, target_task_id, source_agent_ref, target_agent_ref, relation_type, evidence_event_ids)
);
CREATE INDEX idx_collaboration_effect_query ON collaboration_effects(rule_version, project_id, occurred_at, effect_id);
CREATE TABLE collaboration_effect_dependencies (
  rule_version TEXT NOT NULL, project_id TEXT NOT NULL, plan_id TEXT NOT NULL DEFAULT '',
  upstream_task_id TEXT NOT NULL, downstream_task_id TEXT NOT NULL, source_event_id TEXT NOT NULL,
  occurred_at TEXT NOT NULL, PRIMARY KEY(rule_version, source_event_id, upstream_task_id, downstream_task_id)
);
CREATE INDEX idx_collaboration_effect_dep_upstream ON collaboration_effect_dependencies(rule_version,project_id,upstream_task_id);
CREATE TABLE collaboration_effect_diagnostics (
  source_event_id TEXT NOT NULL, rule_version TEXT NOT NULL, reason TEXT NOT NULL, occurred_at TEXT NOT NULL,
  PRIMARY KEY(source_event_id,rule_version,reason)
);
CREATE TABLE collaboration_effect_checkpoints (
  rule_version TEXT PRIMARY KEY, last_event_id TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE collaboration_effect_active (
  id INTEGER PRIMARY KEY CHECK(id=1), rule_version TEXT NOT NULL, updated_at TEXT NOT NULL
);
INSERT INTO collaboration_effect_active(id,rule_version,updated_at) VALUES(1,'collaboration-effect.mvp.v1',datetime('now'));

-- 0123_ai_runtime_stage5_cutover.up.sql — AI Runtime Stage 5 migration/cutover evidence.
--
-- Additive only: legacy Agent runtime fields remain the live read path until a
-- cutover flag selects the new resolver. Object selections store per-object
-- profile/override choices without manufacturing one-off Runtime Profiles.

CREATE TABLE IF NOT EXISTS ai_runtime_object_selections (
    org_id TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL,
    selection_json TEXT NOT NULL,
    selection_source TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    migrated_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (org_id, object_type, object_id)
);
CREATE INDEX IF NOT EXISTS idx_ai_runtime_object_selections_org_type
    ON ai_runtime_object_selections(org_id, object_type);

CREATE TABLE IF NOT EXISTS ai_runtime_shadow_diffs (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    org_id TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL,
    matched INTEGER NOT NULL DEFAULT 0,
    diff_type TEXT NOT NULL,
    legacy_json TEXT NOT NULL DEFAULT '{}',
    new_json TEXT NOT NULL DEFAULT '{}',
    details_json TEXT NOT NULL DEFAULT '{}',
    compared_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_runtime_shadow_diffs_org_run
    ON ai_runtime_shadow_diffs(org_id, run_id);

CREATE TABLE IF NOT EXISTS ai_runtime_cutover_events (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    actor TEXT NOT NULL,
    stage TEXT NOT NULL,
    action TEXT NOT NULL,
    flags_json TEXT NOT NULL DEFAULT '[]',
    rollback_json TEXT NOT NULL DEFAULT '{}',
    before_json TEXT NOT NULL DEFAULT '{}',
    after_json TEXT NOT NULL DEFAULT '{}',
    occurred_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_runtime_cutover_events_org_time
    ON ai_runtime_cutover_events(org_id, occurred_at);

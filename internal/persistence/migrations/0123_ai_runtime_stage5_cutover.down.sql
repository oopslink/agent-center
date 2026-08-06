-- 0123_ai_runtime_stage5_cutover.down.sql

DROP INDEX IF EXISTS idx_ai_runtime_cutover_events_org_time;
DROP TABLE IF EXISTS ai_runtime_cutover_events;
DROP INDEX IF EXISTS idx_ai_runtime_shadow_diffs_org_run;
DROP TABLE IF EXISTS ai_runtime_shadow_diffs;
DROP INDEX IF EXISTS idx_ai_runtime_object_selections_org_type;
DROP TABLE IF EXISTS ai_runtime_object_selections;

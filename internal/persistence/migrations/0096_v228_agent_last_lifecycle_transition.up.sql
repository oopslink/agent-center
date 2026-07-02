-- 0096_v228_agent_last_lifecycle_transition.up.sql — record the time of an agent's
-- most recent lifecycle STATE transition (start / stop / restart / reset / archive /
-- the Mark* feedbacks), distinct from updated_at which also bumps on config edits.
--
--   agents.last_lifecycle_transition_at — the UI renders it as the "started/restarted"
--       time while the agent is running and the "stopped" time while it is stopped.
--
-- UPGRADE SAFETY: one additive ADD COLUMN (nullable) + a one-shot backfill. Existing
-- rows inherit last_lifecycle_transition_at = updated_at (the closest known "last
-- changed" moment — the same value the reader falls back to when the column is NULL);
-- no row is otherwise rewritten.
--
-- DIALECT NOTE: ADD COLUMN (nullable) + a blanket UPDATE are in the SQLite (>=3.35) +
-- PG common subset (mirrors 0088 completed_at).

ALTER TABLE agents ADD COLUMN last_lifecycle_transition_at TEXT;
UPDATE agents SET last_lifecycle_transition_at = updated_at WHERE last_lifecycle_transition_at IS NULL;

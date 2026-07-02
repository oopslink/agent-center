-- 0096_v228_agent_last_lifecycle_transition.down.sql — reverse of the up migration:
-- drop the agents.last_lifecycle_transition_at column. Lifecycle-transition times are
-- lost on rollback; the UI falls back to updated_at (the pre-0096 behavior).
ALTER TABLE agents DROP COLUMN last_lifecycle_transition_at;

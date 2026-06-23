-- 0074_v214_rename_activity_work_item_ref.down.sql — reverse of the F-naming rename.
--
-- Restore the agent_activity_events column to its pre-0074 name (work_item_ref)
-- and the partial index back to idx_aae_work_item, so the migrator round-trip
-- stays reversible. Pure rename; no data is reconstructed.

DROP INDEX IF EXISTS idx_aae_task;
ALTER TABLE agent_activity_events RENAME COLUMN task_ref TO work_item_ref;
CREATE INDEX idx_aae_work_item ON agent_activity_events (work_item_ref) WHERE work_item_ref IS NOT NULL;

-- 0074_v214_rename_activity_work_item_ref.up.sql — v2.14.0 I14 (AgentWorkItem retirement, naming cleanup).
--
-- Final naming pass after the AgentWorkItem model was retired (issue I14 "去掉
-- AgentWorkItem, 收敛到 PM 域的 Task"): an agent's unit of work is now the pm Task,
-- so the leftover AgentWorkItem-lineage "work_item_ref" column on the
-- agent_activity_events observation stream (created 0043) is renamed to "task_ref"
-- to match the live identifiers (agent.AgentActivityEvent.TaskRef). This is a pure
-- rename — agent_activity_events is the INDEPENDENT append-only activity feed and
-- is NOT one of the legacy tables dropped in 0073; only the column/index naming
-- changes here, the data is preserved in place.
--
-- The partial index idx_aae_work_item (created 0043 over work_item_ref) is dropped
-- and recreated as idx_aae_task over the renamed column with the same predicate.

ALTER TABLE agent_activity_events RENAME COLUMN work_item_ref TO task_ref;

DROP INDEX IF EXISTS idx_aae_work_item;
CREATE INDEX idx_aae_task ON agent_activity_events (task_ref) WHERE task_ref IS NOT NULL;

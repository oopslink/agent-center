-- 0090_v227_agent_desc_in_prompt.down.sql — reverse of the up migration: drop the
-- per-agent include_description_in_system_prompt column.
ALTER TABLE agents DROP COLUMN include_description_in_system_prompt;

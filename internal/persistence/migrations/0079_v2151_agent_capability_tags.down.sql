-- 0079_v2151_agent_capability_tags.down.sql — reverse of the up migration.
ALTER TABLE agents DROP COLUMN capability_tags;

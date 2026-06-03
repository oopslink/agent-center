-- 0043_v27_agent_work_items.down.sql

DROP INDEX IF EXISTS idx_aae_work_item;
DROP INDEX IF EXISTS idx_aae_agent;
DROP TABLE IF EXISTS agent_activity_events;

DROP INDEX IF EXISTS idx_awi_agent_active;
DROP INDEX IF EXISTS idx_awi_task;
DROP INDEX IF EXISTS idx_awi_agent;
DROP TABLE IF EXISTS agent_work_items;

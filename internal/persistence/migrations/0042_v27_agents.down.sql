-- 0042_v27_agents.down.sql

DROP INDEX IF EXISTS idx_agents_worker;
DROP INDEX IF EXISTS idx_agents_org;
DROP TABLE IF EXISTS agents;

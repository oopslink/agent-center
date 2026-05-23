-- 0009_v2_agent_instances.down.sql — revert AgentInstance AR

DROP INDEX IF EXISTS idx_agent_instances_builtin;
DROP INDEX IF EXISTS idx_agent_instances_worker_state;
DROP INDEX IF EXISTS uniq_agent_instances_name;
DROP TABLE IF EXISTS agent_instances;

-- 0064_agent_llm_config.down.sql — revert T236: drop the agent LLM tuning columns.
ALTER TABLE agents DROP COLUMN provider;
ALTER TABLE agents DROP COLUMN mode;
ALTER TABLE agents DROP COLUMN reasoning;

-- 0082_f3_agent_model_routing.down.sql — reverse of the up migration: drop the
-- F3 agent model-routing columns.
ALTER TABLE agents DROP COLUMN allowed_models;
ALTER TABLE agents DROP COLUMN max_concurrent_tasks;
ALTER TABLE agents DROP COLUMN default_executor_model;
ALTER TABLE agents DROP COLUMN orchestrator_model;

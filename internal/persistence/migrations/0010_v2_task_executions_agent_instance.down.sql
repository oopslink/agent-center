-- 0010_v2_task_executions_agent_instance.down.sql — revert agent_instance_id 字段

DROP INDEX IF EXISTS idx_task_executions_agent_instance;
ALTER TABLE task_executions DROP COLUMN agent_instance_id;

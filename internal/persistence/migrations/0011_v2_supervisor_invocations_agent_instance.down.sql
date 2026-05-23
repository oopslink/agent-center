-- 0011_v2_supervisor_invocations_agent_instance.down.sql — revert agent_instance_id 字段

DROP INDEX IF EXISTS idx_supervisor_invocations_agent_instance;
ALTER TABLE supervisor_invocations DROP COLUMN agent_instance_id;

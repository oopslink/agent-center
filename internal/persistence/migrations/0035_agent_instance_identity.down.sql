-- 0035_agent_instance_identity.down.sql

DROP INDEX IF EXISTS idx_agent_instances_identity;
DROP INDEX IF EXISTS idx_agent_instances_org;
ALTER TABLE agent_instances DROP COLUMN kind;
ALTER TABLE agent_instances DROP COLUMN organization_id;
ALTER TABLE agent_instances DROP COLUMN identity_id;

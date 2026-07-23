DROP INDEX IF EXISTS idx_pm_stages_gate_task_id;
ALTER TABLE pm_stages DROP COLUMN gate_spec;
ALTER TABLE pm_stages DROP COLUMN gate_task_id;

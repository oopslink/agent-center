ALTER TABLE pm_stages ADD COLUMN gate_task_id TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_stages ADD COLUMN gate_spec TEXT NOT NULL DEFAULT '{}';

CREATE UNIQUE INDEX IF NOT EXISTS idx_pm_stages_gate_task_id
    ON pm_stages(gate_task_id)
    WHERE gate_task_id <> '';

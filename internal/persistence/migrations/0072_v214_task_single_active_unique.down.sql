-- 0072_v214_task_single_active_unique.down.sql — revert v2.14.0 I14/F3 single-active
-- UNIQUE index. Schema-only (index drop); fully reversible. Down->Up lands on an
-- identical schema shape.
DROP INDEX IF EXISTS idx_pm_tasks_one_active_per_agent;

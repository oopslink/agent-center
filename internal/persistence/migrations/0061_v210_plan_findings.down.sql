-- 0061_v210_plan_findings.down.sql — reverse of the v2.10 Plan Shared Findings table.
DROP INDEX IF EXISTS idx_pm_plan_findings_task;
DROP INDEX IF EXISTS idx_pm_plan_findings_plan;
DROP TABLE IF EXISTS pm_plan_findings;

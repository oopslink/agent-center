-- 0054 down — revert the v2.9 plan orchestration foundation (#283).
DROP INDEX idx_pm_task_dependencies_plan;
DROP TABLE pm_task_dependencies;
DROP INDEX idx_pm_tasks_plan;
ALTER TABLE pm_tasks DROP COLUMN plan_id;
DROP INDEX idx_pm_plans_project;
DROP TABLE pm_plans;

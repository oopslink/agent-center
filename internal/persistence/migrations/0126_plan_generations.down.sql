DROP INDEX IF EXISTS idx_pm_plan_generations_plan_created;
DROP TABLE IF EXISTS pm_plan_generations;
DROP INDEX IF EXISTS idx_pm_plans_active_generation;
ALTER TABLE pm_plans DROP COLUMN active_generation_id;

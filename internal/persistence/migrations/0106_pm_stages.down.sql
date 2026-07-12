-- 0106_pm_stages.down.sql — reverse the Plan Stage model additions. Drop the
-- pm_stages table and the pm_tasks.stage_id column. stage_id defaulted to '' (no
-- stage) and Stage is a pure-additive feature (§8), so dropping both simply reverts
-- to the pre-Stage pure-node DAG. No data restore is needed.
DROP INDEX IF EXISTS idx_pm_stages_plan_id;
DROP TABLE IF EXISTS pm_stages;
ALTER TABLE pm_tasks DROP COLUMN stage_id;

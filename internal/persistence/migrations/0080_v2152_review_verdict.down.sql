-- 0080_v2152_review_verdict.down.sql — reverse of the up migration.
DROP INDEX IF EXISTS idx_pm_plan_review_verdicts_plan;
DROP TABLE IF EXISTS pm_plan_review_verdicts;

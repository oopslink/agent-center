-- 0055 down — revert the v2.9 plan dispatch records (#285).
DROP INDEX idx_pm_plan_dispatch_records_plan;
DROP TABLE pm_plan_dispatch_records;

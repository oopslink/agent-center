-- 0104_model_catalog.down.sql — drop the org model catalog table + its indexes.
DROP INDEX IF EXISTS idx_pm_model_catalog_org_model;
DROP INDEX IF EXISTS idx_pm_model_catalog_org;
DROP TABLE IF EXISTS pm_model_catalog;

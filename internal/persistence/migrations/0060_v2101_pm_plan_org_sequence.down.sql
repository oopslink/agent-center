-- 0060_v2101_pm_plan_org_sequence.down.sql — revert v2.10.1 [T99].
DELETE FROM pm_org_sequence WHERE entity_type = 'plan';
ALTER TABLE pm_plans DROP COLUMN org_number;

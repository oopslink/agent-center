-- 0049_v271_pm_org_sequence.down.sql — revert v2.7.1 #245.
DROP TABLE IF EXISTS pm_org_sequence;
ALTER TABLE pm_tasks  DROP COLUMN org_number;
ALTER TABLE pm_issues DROP COLUMN org_number;

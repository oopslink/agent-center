-- 0100_v229_pm_audit_log.down.sql — reverse of the up: drop the audit ledger table
-- and its two lookup indexes. Purely additive migration, so the down is a clean drop.
DROP INDEX IF EXISTS idx_pm_audit_log_project;
DROP INDEX IF EXISTS idx_pm_audit_log_object;
DROP TABLE IF EXISTS pm_audit_log;

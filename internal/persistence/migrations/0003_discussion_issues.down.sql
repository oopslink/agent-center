-- 0003_discussion_issues.down.sql — Phase 3 Discussion Core rollback
--
-- Migrator runs idempotent DROPs; DROP TABLE IF EXISTS keeps re-runs safe.

DROP INDEX IF EXISTS idx_issues_conv;
DROP INDEX IF EXISTS idx_issues_opener;
DROP INDEX IF EXISTS idx_issues_status;
DROP INDEX IF EXISTS idx_issues_project_status;
DROP TABLE IF EXISTS issues;

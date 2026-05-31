-- 0034_organization_scope.down.sql
-- SQLite does not support DROP COLUMN before 3.35.0; and the project
-- targets the bundled mattn/go-sqlite3 which supports DROP COLUMN.

DROP INDEX IF EXISTS idx_user_secrets_org;
ALTER TABLE user_secrets DROP COLUMN organization_id;

DROP INDEX IF EXISTS idx_workers_org;
ALTER TABLE workers DROP COLUMN organization_id;

-- v2.7 #131: projects table retired (org-scope column never added in up).

DROP INDEX IF EXISTS idx_conversations_org;
ALTER TABLE conversations DROP COLUMN organization_id;

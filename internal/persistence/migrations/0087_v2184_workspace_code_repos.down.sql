-- 0087_v2184_workspace_code_repos.down.sql — reverse of the up migration: drop the
-- workspace code_repos table and the pm_code_repo_refs reference columns. Any ref
-- that pointed at a workspace Repo loses that binding (repo_id) on rollback; url-only
-- refs are unaffected. Demote/clear workspace references before rolling back if the
-- project side must keep a usable url (the inherent loss of reverting the workspace
-- model).
ALTER TABLE pm_code_repo_refs DROP COLUMN is_primary;
ALTER TABLE pm_code_repo_refs DROP COLUMN repo_id;
DROP INDEX IF EXISTS idx_code_repos_org;
DROP TABLE IF EXISTS code_repos;

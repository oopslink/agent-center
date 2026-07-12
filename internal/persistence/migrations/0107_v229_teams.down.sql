-- 0107_v229_teams.down.sql — reverse 0107. Drop children before parents so the
-- FK graph unwinds cleanly (indexes drop with their tables, but the explicit
-- DROP INDEX keeps the down idempotent even if a table drop is a no-op).
DROP INDEX IF EXISTS idx_team_projects_project;
DROP TABLE IF EXISTS team_projects;

DROP INDEX IF EXISTS idx_team_members_team;
DROP INDEX IF EXISTS idx_team_members_agent_exclusive;
DROP TABLE IF EXISTS team_members;

DROP TABLE IF EXISTS team_roles;

DROP INDEX IF EXISTS idx_teams_org_name;
DROP TABLE IF EXISTS teams;

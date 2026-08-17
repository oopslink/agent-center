-- 0131_team_role_access_requirements.up.sql -- declared role access requirements.

ALTER TABLE team_roles ADD COLUMN access_requirements TEXT NOT NULL DEFAULT '[]';

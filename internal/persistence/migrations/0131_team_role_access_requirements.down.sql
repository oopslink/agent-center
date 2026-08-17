-- 0131_team_role_access_requirements.down.sql -- remove declared role access requirements.

ALTER TABLE team_roles DROP COLUMN access_requirements;

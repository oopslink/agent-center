DROP TABLE IF EXISTS access_profile_versions;
DROP TABLE IF EXISTS access_profiles;
ALTER TABLE team_roles DROP COLUMN access_profiles_json;
ALTER TABLE team_roles DROP COLUMN access_requirements_json;

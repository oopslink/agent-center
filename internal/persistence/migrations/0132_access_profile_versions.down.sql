-- 0132_access_profile_versions.down.sql -- remove persistent versioned access profiles.

DROP TABLE IF EXISTS access_profile_versions;
DROP TABLE IF EXISTS access_profiles;

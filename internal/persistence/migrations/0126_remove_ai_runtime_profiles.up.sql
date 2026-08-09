CREATE TEMP TABLE _ai_runtime_profile_delete_guard (
    n INTEGER NOT NULL CHECK (n = 0)
);

INSERT INTO _ai_runtime_profile_delete_guard(n)
SELECT COUNT(*) FROM ai_runtime_profiles;

INSERT INTO _ai_runtime_profile_delete_guard(n)
SELECT COUNT(*) FROM ai_runtime_catalogs WHERE default_profile_id <> '';

DROP TABLE _ai_runtime_profile_delete_guard;

DROP INDEX IF EXISTS idx_ai_runtime_profiles_org;
DROP TABLE IF EXISTS ai_runtime_profiles;

ALTER TABLE ai_runtime_catalogs DROP COLUMN default_profile_id;

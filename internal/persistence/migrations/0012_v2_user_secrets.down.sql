-- 0012_v2_user_secrets.down.sql — revert SecretManagement BC8 — UserSecret AR

DROP INDEX IF EXISTS idx_user_secrets_kind_state;
DROP INDEX IF EXISTS uniq_user_secrets_name;
DROP TABLE IF EXISTS user_secrets;

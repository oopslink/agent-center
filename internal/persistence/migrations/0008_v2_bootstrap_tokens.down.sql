-- 0008_v2_bootstrap_tokens.down.sql — revert BootstrapToken Entity

DROP INDEX IF EXISTS idx_bootstrap_tokens_expires;
DROP INDEX IF EXISTS idx_bootstrap_tokens_worker;
DROP INDEX IF EXISTS uniq_bootstrap_tokens_value_hash;
DROP INDEX IF EXISTS uniq_bootstrap_tokens_active_per_worker;
DROP TABLE IF EXISTS bootstrap_tokens;

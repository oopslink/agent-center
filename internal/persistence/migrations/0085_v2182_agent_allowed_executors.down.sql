-- 0085_v2182_agent_allowed_executors.down.sql — drop the authoritative
-- allowed_executors column. allowed_models was retained through the up migration
-- (kept in sync as the derived mirror), so it is still present and authoritative
-- again after this drop — no data restore needed. Any executor whose cli was NOT
-- claude-code loses its CLI binding on rollback (allowed_models is model-only); that
-- is the inherent, expected loss of rolling back the {cli, model} upgrade.
ALTER TABLE agents DROP COLUMN allowed_executors;

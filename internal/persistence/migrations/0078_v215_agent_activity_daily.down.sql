-- 0078_v215_agent_activity_daily.down.sql — reverse of 0078 up. Drops the rollup
-- table and its cursor (and the index, which SQLite drops with the table). Purely
-- additive migration; the rollup is fully recomputable from the raw sources, so
-- the down is a clean drop with no data to restore.
DROP TABLE IF EXISTS agent_activity_rollup_cursor;
DROP TABLE IF EXISTS agent_activity_daily;

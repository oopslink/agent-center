-- 0077_v215_usage_collection.down.sql — reverse of 0077 up. Drops the two
-- collection tables (and their indexes, which SQLite drops with the table). Purely
-- additive migration, so the down is a clean drop with no data to restore.
DROP TABLE IF EXISTS usage_events;
DROP TABLE IF EXISTS model_prices;

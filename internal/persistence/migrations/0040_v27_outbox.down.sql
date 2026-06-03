-- 0040_v27_outbox.down.sql

DROP TABLE IF EXISTS outbox_applied;
DROP INDEX IF EXISTS idx_outbox_unprocessed;
DROP TABLE IF EXISTS outbox_events;

-- 0066_dm_dedup.down.sql — drop the DM dedup index + column (T288).
-- The duplicate MERGE (data) is irreversible; this only reverts the schema.
DROP INDEX IF EXISTS uniq_conversations_dm_key;
ALTER TABLE conversations DROP COLUMN dm_key;

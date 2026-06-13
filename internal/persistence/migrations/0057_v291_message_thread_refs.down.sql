-- 0057_v291_message_thread_refs.down.sql

DROP INDEX IF EXISTS idx_messages_root;
ALTER TABLE messages DROP COLUMN root_message_id;
ALTER TABLE messages DROP COLUMN parent_message_id;

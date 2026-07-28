DROP INDEX IF EXISTS idx_messages_reply_to_message_id;
ALTER TABLE messages DROP COLUMN reply_to_message_id;

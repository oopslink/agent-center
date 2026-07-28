-- Primary reply linkage: records which source message this message closes over.
-- Orthogonal to thread refs: a primary reply may be top-level or posted in the
-- source message's thread.
ALTER TABLE messages ADD COLUMN reply_to_message_id TEXT;

CREATE INDEX IF NOT EXISTS idx_messages_reply_to_message_id
    ON messages (reply_to_message_id)
    WHERE reply_to_message_id IS NOT NULL;

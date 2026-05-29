-- 0037_v27_conversation_owner_ref_kind.down.sql

UPDATE conversations SET kind = 'channel' WHERE kind = 'project_channel';

DROP INDEX IF EXISTS uniq_conversations_channel_name;
CREATE UNIQUE INDEX uniq_conversations_channel_name
    ON conversations (name)
    WHERE kind = 'channel' AND name IS NOT NULL;

DROP INDEX IF EXISTS idx_conversations_owner_ref;
ALTER TABLE conversations DROP COLUMN owner_ref;

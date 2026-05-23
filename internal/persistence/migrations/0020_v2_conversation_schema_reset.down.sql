-- 0020_v2_conversation_schema_reset.down.sql — P10 § 3.0
-- 还原 v1 conversation schema：drop v2 字段，恢复 v1 vendor 字段 + index.

DROP INDEX IF EXISTS idx_conversations_parent;
DROP INDEX IF EXISTS uniq_conversations_channel_name;

ALTER TABLE conversations DROP COLUMN archived_by;
ALTER TABLE conversations DROP COLUMN archived_at;
ALTER TABLE conversations DROP COLUMN created_by;
ALTER TABLE conversations DROP COLUMN participants;
ALTER TABLE conversations DROP COLUMN parent_conversation_id;
ALTER TABLE conversations DROP COLUMN description;
ALTER TABLE conversations DROP COLUMN name;

ALTER TABLE conversations ADD COLUMN title TEXT;
ALTER TABLE conversations ADD COLUMN primary_channel_hint TEXT;
ALTER TABLE conversations ADD COLUMN primary_channel_thread_key TEXT;

CREATE UNIQUE INDEX uniq_conversations_channel_thread
    ON conversations (primary_channel_hint, primary_channel_thread_key)
    WHERE primary_channel_hint       IS NOT NULL
      AND primary_channel_thread_key IS NOT NULL;

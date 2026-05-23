-- 0020_v2_conversation_schema_reset.up.sql — P10 § 3.0
--
-- Conversation v2 schema reset per ADR-0032 (channel first-class) +
-- ADR-0031 (vendor撤回) + ADR-0034 (participants 字段).
--
-- 删字段（vendor 撤回）：
--   - primary_channel_hint / primary_channel_thread_key (uniq index 先 drop)
--   - title (改名为 name + description)
--
-- 加字段（v2 universal）：
--   - name              TEXT     — channel kind 必填且全局唯一；其他 kind 可空
--   - description       TEXT     — universal nullable
--   - parent_conversation_id TEXT — universal 父子链（CV1 + CV3 carry-over 铺基础）
--   - participants      TEXT NOT NULL DEFAULT '[]'  — JSON array of ParticipantElement
--   - created_by        TEXT NOT NULL DEFAULT 'system' — 创建者 actor
--   - archived_at       TEXT     — status='archived' 时填
--   - archived_by       TEXT     — 同上
--
-- 加索引：
--   - uniq_conversations_channel_name on (name) WHERE kind='channel' AND name IS NOT NULL
--   - idx_conversations_parent on (parent_conversation_id) WHERE NOT NULL

DROP INDEX IF EXISTS uniq_conversations_channel_thread;

ALTER TABLE conversations DROP COLUMN primary_channel_hint;
ALTER TABLE conversations DROP COLUMN primary_channel_thread_key;
ALTER TABLE conversations DROP COLUMN title;

ALTER TABLE conversations ADD COLUMN name TEXT;
ALTER TABLE conversations ADD COLUMN description TEXT;
ALTER TABLE conversations ADD COLUMN parent_conversation_id TEXT;
ALTER TABLE conversations ADD COLUMN participants TEXT NOT NULL DEFAULT '[]';
ALTER TABLE conversations ADD COLUMN created_by TEXT NOT NULL DEFAULT 'system';
ALTER TABLE conversations ADD COLUMN archived_at TEXT;
ALTER TABLE conversations ADD COLUMN archived_by TEXT;

CREATE UNIQUE INDEX uniq_conversations_channel_name
    ON conversations (name)
    WHERE kind = 'channel' AND name IS NOT NULL;

CREATE INDEX idx_conversations_parent
    ON conversations (parent_conversation_id)
    WHERE parent_conversation_id IS NOT NULL;

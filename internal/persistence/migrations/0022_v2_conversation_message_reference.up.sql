-- 0022_v2_conversation_message_reference.up.sql — P10 § 3.0
--
-- CV3 跨 Conversation message carry-over reference table per ADR-0035.
-- 应用层 invariants（Repository / Service 层校验，conventions § 9.w）：
--   - source_message 必须存在于 source_conversation
--   - child_conversation 必须存在 + active
--   - (child_conversation_id, source_message_id) 唯一 — append-only carry-over

CREATE TABLE conversation_message_reference (
    id                       TEXT PRIMARY KEY,           -- ULID
    child_conversation_id    TEXT NOT NULL,              -- 新 conv 引用 prior messages
    source_conversation_id   TEXT NOT NULL,              -- 源 conv
    source_message_id        TEXT NOT NULL,              -- 被引 message
    created_by               TEXT NOT NULL,              -- 物化 carry-over 的 actor
    created_at               TEXT NOT NULL
);
CREATE INDEX idx_cmr_child  ON conversation_message_reference (child_conversation_id);
CREATE INDEX idx_cmr_source ON conversation_message_reference (source_message_id);
CREATE UNIQUE INDEX uniq_cmr_child_source_msg
    ON conversation_message_reference (child_conversation_id, source_message_id);

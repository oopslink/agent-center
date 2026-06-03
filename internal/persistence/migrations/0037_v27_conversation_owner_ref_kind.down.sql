-- 0037_v27_conversation_owner_ref_kind.down.sql

DROP INDEX IF EXISTS idx_conversations_project_ref;
DROP INDEX IF EXISTS idx_conversations_owner_ref;
ALTER TABLE conversations DROP COLUMN project_ref;
ALTER TABLE conversations DROP COLUMN owner_ref;

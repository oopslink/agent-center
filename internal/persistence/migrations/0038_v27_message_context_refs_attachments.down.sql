-- 0038_v27_message_context_refs_attachments.down.sql

ALTER TABLE messages DROP COLUMN attachments;
ALTER TABLE messages DROP COLUMN context_refs;

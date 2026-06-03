-- 0038_v27_message_context_refs_attachments.up.sql — v2.7 A0 (ADR-0047, task #95)
--
-- Message foundation for the v2.7 domain refactor:
--   1. context_refs (JSON object) — carries at least work_item_ref / task_ref /
--      agent_ref so the UI can group a Task Conversation's messages by
--      AgentWorkItem segment across reassignments (ADR-0047 §3, plan §2.4).
--   2. attachments (JSON array) — unified MessageAttachment: each element is
--      { uri: ac://files/{ulid}, filename, mime_type, size }. No file/image
--      split in the domain model; display type is derived from metadata
--      (ADR-0047 §4, ADR-0048).
--
-- Both default to empty so existing messages remain valid (A0 acceptance:
-- a message can carry work_item_ref without breaking the UI; the empty
-- default is the "no refs / no attachments" case).

ALTER TABLE messages ADD COLUMN context_refs TEXT NOT NULL DEFAULT '{}';
ALTER TABLE messages ADD COLUMN attachments  TEXT NOT NULL DEFAULT '[]';

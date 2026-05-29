-- 0037_v27_conversation_owner_ref_kind.up.sql — v2.7 A0 (ADR-0047, task #95)
--
-- Conversation foundation for the v2.7 domain refactor (channel model
-- finalized in plan §10 OQ10):
--   1. owner_ref URI — a Conversation's reference back to its owner:
--        channel: id://organizations/{org_id}  (generic Org-level group chat;
--                 belongs to exactly one Org, NOT project-bound)
--        issue/task: pm://issues|tasks/{id}     (set in phase B)
--        dm: NULL
--      Existing channel rows are backfilled to their Org below.
--   2. project_ref — a nullable, constraint-free SOFT LABEL for channels
--      (pm://projects/{id}); v1 grouping/navigation only (does not limit
--      members or visibility), settable/clearable. NOT ownership.
--
-- The kind set stays {channel | issue | task | dm} — 'channel' is RETAINED
-- (no rename). Vestigial 'adhoc'/'notification' were never created in prod.
--
-- Additive ALTER preserves existing conversations/messages (A0 acceptance:
-- old channel/message mechanism still works after the rebuild).

ALTER TABLE conversations ADD COLUMN owner_ref   TEXT;
ALTER TABLE conversations ADD COLUMN project_ref TEXT;

-- Backfill: every existing channel is owned by its Org.
UPDATE conversations
   SET owner_ref = 'id://organizations/' || organization_id
 WHERE kind = 'channel' AND organization_id IS NOT NULL AND organization_id != '';

CREATE INDEX idx_conversations_owner_ref
    ON conversations (owner_ref)
    WHERE owner_ref IS NOT NULL;

CREATE INDEX idx_conversations_project_ref
    ON conversations (project_ref)
    WHERE project_ref IS NOT NULL;

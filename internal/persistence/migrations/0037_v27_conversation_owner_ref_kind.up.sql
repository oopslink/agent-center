-- 0037_v27_conversation_owner_ref_kind.up.sql — v2.7 A0 (ADR-0047, task #95)
--
-- Conversation foundation for the v2.7 domain refactor:
--   1. owner_ref URI column — a Conversation references its business object
--      (ProjectManager BC) by URI: pm://projects|issues|tasks/{id}. Nullable;
--      A0 only stores it (NO validation — deferred to phase B per ADR-0047 §负面).
--      dm and (for now) project_channel rows carry NULL.
--   2. kind convergence to the v2.7 four-value set
--      (project_channel | issue | task | dm; ADR-0047 §1). The legacy 'channel'
--      kind is org-level group chat; v2.7 renames it project_channel and will
--      decide org-level-channel placement in phase B (owner_ref stays NULL
--      until then). Vestigial 'adhoc'/'notification' are never created in
--      production paths, so no data migration is needed for them.
--
-- v2.7 permits drop+recreate, but this additive ALTER preserves existing
-- conversations/messages (cleaner against the A0 acceptance "old channel/
-- message still work after rebuild"), matching the 0034 column-add pattern.

ALTER TABLE conversations ADD COLUMN owner_ref TEXT;

-- Rename the live 'channel' kind to the v2.7 'project_channel'.
UPDATE conversations SET kind = 'project_channel' WHERE kind = 'channel';

-- The unique channel-name index (0020) keys on the old kind='channel'
-- predicate; rebuild it on the new kind so name uniqueness still holds.
DROP INDEX IF EXISTS uniq_conversations_channel_name;
CREATE UNIQUE INDEX uniq_conversations_channel_name
    ON conversations (name)
    WHERE kind = 'project_channel' AND name IS NOT NULL;

CREATE INDEX idx_conversations_owner_ref
    ON conversations (owner_ref)
    WHERE owner_ref IS NOT NULL;

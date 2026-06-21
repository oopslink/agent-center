-- 0066_dm_dedup.up.sql — T288: one DM per (org, participant set).
--
-- A DM (kind='dm') between the same participants must be a SINGLE conversation,
-- reused on every open path (Start-a-DM, reminder delivery, agent open) instead of
-- duplicated. This migration:
--   1) adds dm_key — the canonical, order-independent key over a DM's ACTIVE
--      participant identity ids (sorted, US(\x1f)-joined; mirrors conversation.DMKey);
--   2) backfills dm_key for existing DMs;
--   3) MERGES pre-existing duplicate DMs into the earliest (canonical) one —
--      repointing messages / carry-over refs / read- & follow-state, then deleting
--      the now-empty duplicates (so the list shows one DM per agent again);
--   4) enforces uniqueness with a partial unique index, so concurrent/retried opens
--      can never duplicate again (the DB-level regression guard).

ALTER TABLE conversations ADD COLUMN dm_key TEXT;

-- (1)+(2) Backfill: dm_key = sorted, char(31)-joined active participant identity ids.
-- Active = participant whose left_at is absent/empty (mirrors ParticipantElement.IsActive).
-- The inner ORDER BY makes the key order-independent; an all-left DM yields NULL (no key).
UPDATE conversations
SET dm_key = (
    SELECT group_concat(idv, char(31))
    FROM (
        SELECT json_extract(p.value, '$.identity_id') AS idv
        FROM json_each(conversations.participants) AS p
        WHERE COALESCE(json_extract(p.value, '$.left_at'), '') = ''
        ORDER BY json_extract(p.value, '$.identity_id')
    )
)
WHERE kind = 'dm';

-- (3) Merge duplicates. Build dupe_id -> canonical_id for every non-archived DM that
-- shares (org, dm_key) with an EARLIER DM (earliest created_at, id as tiebreak = canon).
CREATE TEMP TABLE _dm_canon AS
SELECT d.id AS dupe_id,
       (SELECT m.id FROM conversations m
        WHERE m.kind = 'dm' AND m.archived_at IS NULL
          AND m.organization_id = d.organization_id AND m.dm_key = d.dm_key
        ORDER BY m.created_at ASC, m.id ASC
        LIMIT 1) AS canon_id
FROM conversations d
WHERE d.kind = 'dm' AND d.dm_key IS NOT NULL AND d.archived_at IS NULL;

-- keep only the actual duplicates (a row whose canonical is some EARLIER conversation).
DELETE FROM _dm_canon WHERE canon_id IS NULL OR dupe_id = canon_id;

-- Repoint child rows from each duplicate to its canonical DM.
UPDATE messages
SET conversation_id = (SELECT canon_id FROM _dm_canon WHERE dupe_id = messages.conversation_id)
WHERE conversation_id IN (SELECT dupe_id FROM _dm_canon);

UPDATE conversation_message_reference
SET child_conversation_id = (SELECT canon_id FROM _dm_canon WHERE dupe_id = conversation_message_reference.child_conversation_id)
WHERE child_conversation_id IN (SELECT dupe_id FROM _dm_canon);
UPDATE conversation_message_reference
SET source_conversation_id = (SELECT canon_id FROM _dm_canon WHERE dupe_id = conversation_message_reference.source_conversation_id)
WHERE source_conversation_id IN (SELECT dupe_id FROM _dm_canon);

-- read- & follow-state are PK (user_id, conversation_id): a user may already have a row
-- on the canonical DM, so move-or-ignore (keep canonical's cursor) then drop leftovers.
UPDATE OR IGNORE user_conversation_read_state
SET conversation_id = (SELECT canon_id FROM _dm_canon WHERE dupe_id = user_conversation_read_state.conversation_id)
WHERE conversation_id IN (SELECT dupe_id FROM _dm_canon);
DELETE FROM user_conversation_read_state WHERE conversation_id IN (SELECT dupe_id FROM _dm_canon);

UPDATE OR IGNORE user_conversation_follow_state
SET conversation_id = (SELECT canon_id FROM _dm_canon WHERE dupe_id = user_conversation_follow_state.conversation_id)
WHERE conversation_id IN (SELECT dupe_id FROM _dm_canon);
DELETE FROM user_conversation_follow_state WHERE conversation_id IN (SELECT dupe_id FROM _dm_canon);

-- Drop the now-empty duplicate DM conversations.
DELETE FROM conversations WHERE id IN (SELECT dupe_id FROM _dm_canon);

DROP TABLE _dm_canon;

-- (4) Enforce uniqueness: at most one NON-archived DM per (org, participant set).
-- Archived DMs are exempt (so a pair can re-open a fresh DM after archiving the old).
CREATE UNIQUE INDEX uniq_conversations_dm_key
    ON conversations (organization_id, dm_key)
    WHERE kind = 'dm' AND dm_key IS NOT NULL AND archived_at IS NULL;

-- 0050_user_conversation_follow_state.up.sql — v2.8 #268
--
-- Per-user / per-conversation follow marker for the v2.8 P1
-- surface-agnostic MessageList. A "followed" conversation/thread
-- contributes to the user's unread / mention badges and is delivered;
-- an unfollowed one is suppressed from the badge model. Composite PK
-- (user_id, conversation_id) — at most one row per pair, mirroring
-- user_conversation_read_state (migration 0026).
--
-- DEFAULT is computed at the app layer (FollowStateService), NOT stored:
--   * top-level conversation (channel / DM, parent_conversation_id empty)
--     → followed by default (you are a member);
--   * thread (parent_conversation_id non-null)
--     → NOT followed by default; auto-follow on participate / @mention.
-- A row therefore only exists to record an explicit OVERRIDE of the
-- default: explicit unfollow (followed=0) of a default-followed channel,
-- or auto/explicit follow (followed=1) of a default-unfollowed thread.
-- Row absence = "use the kind default". This is why migration 0050 is
-- schema-only with NO data backfill: existing top-level conversations
-- resolve to followed=true via the default, existing threads to false —
-- the §9.4 "watermark" here is that old rows map to the correct default,
-- verified by the real cross-version upgrade smoke (not an empty-DB
-- round-trip).
--
-- HUMAN-ONLY (Q-T1 / D3 directed-wake): the app layer skips writing rows
-- for agent identities — agents never accumulate follow state and are
-- zeroed in the DTO (defence in depth).
--
-- Reference integrity (user_id → identities.id, conversation_id →
-- conversations.id) enforced at app layer per conventions § 9.w; no FK
-- declarations (consistent with 0026).

CREATE TABLE user_conversation_follow_state (
    user_id          TEXT NOT NULL,
    conversation_id  TEXT NOT NULL,
    followed         INTEGER NOT NULL,
    updated_at       TEXT NOT NULL,
    version          INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (user_id, conversation_id)
);

-- Supports per-conversation fanout (delete-on-conversation-delete and
-- "who follows this conversation" lookups), mirroring idx_ucrs_conversation.
CREATE INDEX idx_ucfs_conversation
    ON user_conversation_follow_state (conversation_id);

-- 0026_user_conversation_read_state.up.sql — v2.1-C-1
--
-- Per-user / per-conversation read marker for the v2.1 unread-message
-- badge UX (channel + DM list row + sidebar count). Composite PK
-- (user_id, conversation_id) — at most one row per pair.
--
-- "never seen" = row absent (no sentinel value). Insert-or-update via
-- UPSERT in the app-layer MarkSeenService (v2.1-C-2).
--
-- Reference integrity (user_id → identities.id, conversation_id →
-- conversations.id) enforced at app layer per conventions § 9.w; no
-- FK declarations.

CREATE TABLE user_conversation_read_state (
    user_id                  TEXT NOT NULL,
    conversation_id          TEXT NOT NULL,
    last_seen_message_id     TEXT NOT NULL,
    updated_at               TEXT NOT NULL,
    version                  INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (user_id, conversation_id)
);

-- Supports per-conv user lookup (SSE fanout in v2.1-C-2 needs to know
-- "which users care about this conversation's read state").
CREATE INDEX idx_ucrs_conversation
    ON user_conversation_read_state (conversation_id);

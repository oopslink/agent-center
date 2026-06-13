-- 0057_v291_message_thread_refs.up.sql — v2.9.1 Thread P1 (BE base)
--
-- Thread foundation for the conversation BC. A message may be a reply that hangs
-- off a ROOT message (Slack-style depth-1):
--   * parent_message_id — the message this one replies to. Under depth-1 this is
--     always a root (the writer redirects replies-to-replies to the root).
--   * root_message_id    — the thread's root id (== parent_message_id for a reply).
-- Both are NULL for a top-level (root) message, so existing rows remain valid.
--
-- The partial index supports the read side (v2.9.1 read-model task): fetch a
-- thread's replies in order by (root_message_id, posted_at) and count replies.

ALTER TABLE messages ADD COLUMN parent_message_id TEXT;
ALTER TABLE messages ADD COLUMN root_message_id   TEXT;

CREATE INDEX idx_messages_root
    ON messages (root_message_id, posted_at)
    WHERE root_message_id IS NOT NULL;

-- 0027: messages (conversation_id, id) covering index.
--
-- v2.1-C-1 added user_conversation_read_state with a countUnread
-- query `WHERE conversation_id = ? AND id > ? LIMIT N` (see
-- internal/conversation/service/read_state.go). The existing
-- idx_messages_conv (conversation_id, posted_at) only helped with
-- the conversation_id seek; id > ? was a residual predicate
-- evaluated row-by-row inside the conversation — O(conversation_size)
-- regardless of unread count.
--
-- This index makes the (conversation_id, id) prefix usable as a range
-- seek, dropping the cost to O(unread_count + log conversation_size).
-- Filed as v2.1-E in v2.1-backlog; promoted now per the v1+v2
-- self-audit Bug C follow-up.

CREATE INDEX idx_messages_conv_id ON messages (conversation_id, id);

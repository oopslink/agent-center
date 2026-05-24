-- 0026_user_conversation_read_state.down.sql — v2.1-C-1
DROP INDEX IF EXISTS idx_ucrs_conversation;
DROP TABLE IF EXISTS user_conversation_read_state;

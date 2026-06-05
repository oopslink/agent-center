-- 0050_user_conversation_follow_state.down.sql — v2.8 #268
DROP INDEX IF EXISTS idx_ucfs_conversation;
DROP TABLE IF EXISTS user_conversation_follow_state;

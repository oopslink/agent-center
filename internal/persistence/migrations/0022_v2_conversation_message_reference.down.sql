-- 0022_v2_conversation_message_reference.down.sql — P10 § 3.0
DROP INDEX IF EXISTS uniq_cmr_child_source_msg;
DROP INDEX IF EXISTS idx_cmr_source;
DROP INDEX IF EXISTS idx_cmr_child;
DROP TABLE IF EXISTS conversation_message_reference;

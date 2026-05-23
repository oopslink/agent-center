-- 0023_v2_message_strip_vendor.up.sql — P10 § 3.0
-- 删 messages.vendor_msg_ref + 其 unique index per ADR-0031 (vendor 撤回).

DROP INDEX IF EXISTS uniq_messages_vendor_ref;
ALTER TABLE messages DROP COLUMN vendor_msg_ref;

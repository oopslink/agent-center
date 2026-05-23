-- 0023_v2_message_strip_vendor.down.sql — P10 § 3.0
ALTER TABLE messages ADD COLUMN vendor_msg_ref TEXT;
CREATE UNIQUE INDEX uniq_messages_vendor_ref ON messages (vendor_msg_ref) WHERE vendor_msg_ref IS NOT NULL;

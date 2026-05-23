-- 0024_v2_kind_rename.up.sql — P10 § 3.0
-- Rename conversation.kind 'group_thread' → 'channel' per ADR-0032.
-- 应用层强制 channel kind 必填 name (per uniq index 0020 partial unique);
-- v1 group_thread 行没有 name 字段会 NULL，保留 NULL — channel 业务约束
-- 由 ChannelManagementService 在创建时 enforce。

UPDATE conversations SET kind = 'channel' WHERE kind = 'group_thread';

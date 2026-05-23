-- 0021_v2_identity_simplify.up.sql — P10 § 3.0
--
-- Identity 简化 per ADR-0033：kind 4 → 3 (user / agent / system)；删
-- ChannelBinding（vendor 撤回 per ADR-0031）。
-- v2 不考虑向后兼容：直接 DELETE supervisor / bot 行 + DROP channel_bindings 表。
--
-- v2 ID 格式约定（应用层 enforce）：'kind:id' (e.g. 'user:hayang' / 'agent:s-X' / 'system')

DELETE FROM identities WHERE kind NOT IN ('user', 'agent', 'system');

DROP INDEX IF EXISTS uniq_channel_bindings_preferred;
DROP INDEX IF EXISTS uniq_channel_bindings_channel_vendor_user;
DROP INDEX IF EXISTS idx_channel_bindings_identity;
DROP TABLE IF EXISTS channel_bindings;

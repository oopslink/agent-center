-- 0005_bridge_feishu_outbound.down.sql — revert Phase 5 schema additions
DROP TABLE IF EXISTS bridge_subscription_cursors;
DROP TABLE IF EXISTS feishu_delivery_ledger;
DROP TABLE IF EXISTS channel_bindings;
DROP TABLE IF EXISTS identities;

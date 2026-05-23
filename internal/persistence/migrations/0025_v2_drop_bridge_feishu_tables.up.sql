-- 0025_v2_drop_bridge_feishu_tables.up.sql — P10 F6
--
-- Drop the two Bridge BC physical tables left behind by migration
-- 0005_bridge_feishu_outbound after P10 § 3.9 removed all Go consumers
-- (per ADR-0031, Bridge BC撤回). v2 forward-only.
--
-- Tables dropped:
--   - feishu_delivery_ledger (Bridge ACL audit)
--   - bridge_subscription_cursors (Bridge events subscriber cursor)
--
-- Identity-related tables (identities, channel_bindings) from 0005 are
-- handled separately by 0021 (channel_bindings dropped there; identities
-- kept for v2).

DROP INDEX IF EXISTS idx_feishu_ledger_status_pending;
DROP INDEX IF EXISTS idx_feishu_ledger_message;
DROP INDEX IF EXISTS uniq_feishu_ledger_message;
DROP TABLE IF EXISTS feishu_delivery_ledger;
DROP TABLE IF EXISTS bridge_subscription_cursors;

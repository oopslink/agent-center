-- 0005_bridge_feishu_outbound.up.sql — Phase 5 Bridge ACL Outbound (Feishu)
--
-- 落盘以下 BC 物理表（conventions § 9.z BC 物理隔离）：
--   - Conversation BC: identities, channel_bindings  (Identity AR + sub-VO)
--   - Bridge BC:       feishu_delivery_ledger        (ACL audit, 非业务聚合)
--   - Bridge BC:       bridge_subscription_cursors   (events 表订阅游标; 不动 events schema)
--
-- 元层规则按 02-persistence-schema § 1-7：
--   - ULID / 形式化 TEXT PK
--   - TEXT 存 ISO8601 时间戳
--   - INTEGER 0/1 boolean
--   - 不声明 FOREIGN KEY（conventions § 9.w）；引用完整性由应用层 Repository / Domain Service 负责
--   - reason / message 平铺双字段（conventions § 16）
--   - 不在 SQL 里 json_extract
--
-- 字段对位：
--   - identities / channel_bindings: conversation/02-identity.md § 1
--   - feishu_delivery_ledger:        bridge/00-overview.md § 5.1 + plan-5 § 3.3
--   - bridge_subscription_cursors:   plan-5 § 6.3

-- =========================================================================
-- Conversation BC — identities (Identity AR; conversation/02 § 1)
-- =========================================================================
CREATE TABLE identities (
    id              TEXT PRIMARY KEY,         -- 形式化 ID: 'user:hayang' / 'supervisor:inv-N' / 'agent:s-X' / 'bot'
    kind            TEXT NOT NULL,            -- user | supervisor | agent | bot (应用层 enum 校验)
    display_name    TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    version         INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_identities_kind ON identities (kind);

-- =========================================================================
-- Conversation BC — channel_bindings (ChannelBinding VO sub-从属; conversation/02 § 1)
-- =========================================================================
-- 引用完整性由应用层 IdentityRegistrationService.BindChannel 在写入前校验 identity 存在
CREATE TABLE channel_bindings (
    id              TEXT PRIMARY KEY,        -- ULID
    identity_id     TEXT NOT NULL,           -- references identities(id), enforced at app layer
    channel         TEXT NOT NULL,           -- 'feishu' / 'dingtalk' / ...
    vendor_user_id  TEXT NOT NULL,           -- vendor 侧用户 id 字符串
    preferred       INTEGER NOT NULL DEFAULT 0,  -- 0/1, identity 在该渠道是否默认推送
    bound_at        TEXT NOT NULL,
    created_at      TEXT NOT NULL
);
CREATE INDEX idx_channel_bindings_identity ON channel_bindings (identity_id);
-- (channel, vendor_user_id) 唯一 - conversation/02 § 4 invariant 6
CREATE UNIQUE INDEX uniq_channel_bindings_channel_vendor_user
    ON channel_bindings (channel, vendor_user_id);
-- preferred 唯一 per (identity_id, channel) - invariant 7 (partial unique)
CREATE UNIQUE INDEX uniq_channel_bindings_preferred
    ON channel_bindings (identity_id, channel)
    WHERE preferred = 1;

-- =========================================================================
-- Bridge BC — feishu_delivery_ledger (ACL audit; bridge/00 § 5.1)
-- =========================================================================
CREATE TABLE feishu_delivery_ledger (
    id              TEXT PRIMARY KEY,           -- ULID
    message_id      TEXT NOT NULL,              -- Conversation.Message.id (弱引用，audit)
    conversation_id TEXT NOT NULL,
    channel         TEXT NOT NULL,              -- 'feishu'
    thread_key      TEXT,                       -- 投递目标 thread；root card 发出后填
    vendor_msg_ref  TEXT,                       -- 飞书 message_id
    card_message_id TEXT,                       -- interactive card 时的 card msg id (ADR-0020 § 4 audit)
    status          TEXT NOT NULL,              -- pending | delivered | failed
    retry_count     INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT,
    delivered_at    TEXT,
    updated_at      TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    version         INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_feishu_ledger_message ON feishu_delivery_ledger (message_id);
CREATE INDEX idx_feishu_ledger_status_pending
    ON feishu_delivery_ledger (status) WHERE status = 'pending';
-- 每个 message_id 唯一 ledger 行（dispatcher 用其做幂等兜底）
CREATE UNIQUE INDEX uniq_feishu_ledger_message ON feishu_delivery_ledger (message_id);

-- =========================================================================
-- Bridge BC — bridge_subscription_cursors (events 表订阅游标; plan-5 § 6.3)
-- =========================================================================
-- 不动 events 表 schema（events 是 append-only contract，已冻结）；
-- subscriber 单 row PK，便于 Phase 6 Supervisor 复用同表。
CREATE TABLE bridge_subscription_cursors (
    subscriber      TEXT PRIMARY KEY,        -- 'feishu_outbound' / 'supervisor' / ...
    last_event_id   TEXT NOT NULL DEFAULT '',
    updated_at      TEXT NOT NULL
);

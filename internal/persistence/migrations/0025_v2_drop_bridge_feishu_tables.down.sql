-- 0025_v2_drop_bridge_feishu_tables.down.sql — P10 F6
-- Restore the bridge feishu tables (data not recoverable; v2 forward-only).

CREATE TABLE feishu_delivery_ledger (
    id              TEXT PRIMARY KEY,
    message_id      TEXT NOT NULL,
    conversation_id TEXT NOT NULL,
    channel         TEXT NOT NULL,
    thread_key      TEXT,
    vendor_msg_ref  TEXT,
    card_message_id TEXT,
    status          TEXT NOT NULL,
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
CREATE UNIQUE INDEX uniq_feishu_ledger_message ON feishu_delivery_ledger (message_id);

CREATE TABLE bridge_subscription_cursors (
    subscriber      TEXT PRIMARY KEY,
    last_event_id   TEXT NOT NULL DEFAULT '',
    updated_at      TEXT NOT NULL
);

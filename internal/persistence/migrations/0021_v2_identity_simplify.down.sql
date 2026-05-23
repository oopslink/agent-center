-- 0021_v2_identity_simplify.down.sql — P10 § 3.0
-- 还原 channel_bindings 表。supervisor / bot 行已删，无法恢复（v2 forward-only）.

CREATE TABLE channel_bindings (
    id              TEXT PRIMARY KEY,
    identity_id     TEXT NOT NULL,
    channel         TEXT NOT NULL,
    vendor_user_id  TEXT NOT NULL,
    preferred       INTEGER NOT NULL DEFAULT 0,
    bound_at        TEXT NOT NULL,
    created_at      TEXT NOT NULL
);
CREATE INDEX idx_channel_bindings_identity ON channel_bindings (identity_id);
CREATE UNIQUE INDEX uniq_channel_bindings_channel_vendor_user
    ON channel_bindings (channel, vendor_user_id);
CREATE UNIQUE INDEX uniq_channel_bindings_preferred
    ON channel_bindings (identity_id, channel)
    WHERE preferred = 1;

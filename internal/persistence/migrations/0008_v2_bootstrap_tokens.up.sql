-- 0008_v2_bootstrap_tokens.up.sql — Phase 8 (v2) BootstrapToken Entity
--
-- 覆盖 ADR-0023 § 2：BootstrapToken 升 Entity (state machine: active/used/expired/revoked)
--
-- 不变量（应用层 / DB 双层保证）：
--   - 同一 worker_id 同时至多 1 个 active token （UNIQUE INDEX WHERE status='active'）
--   - 明文不入 DB（仅 value_hash）
--   - status=used 是终态，无 reissue 路径（service 层拒）

CREATE TABLE bootstrap_tokens (
    id              TEXT PRIMARY KEY,           -- ULID
    worker_id       TEXT NOT NULL,              -- 绑定 Worker（不可变）
    value_hash      TEXT NOT NULL,              -- token 字面值 SHA-256 hex（明文仅签发返回）
    status          TEXT NOT NULL,              -- active | used | expired | revoked
    created_at      TEXT NOT NULL,
    expires_at      TEXT NOT NULL,              -- TTL 默认 30 min
    used_at         TEXT,                       -- 兑换为 session 的时刻；非 NULL 当 status=used
    revoked_at      TEXT,                       -- 撤销时刻；非 NULL 当 status=revoked
    revoked_reason  TEXT,                       -- closed enum: manual / reissue_superseded
    revoked_message TEXT,                       -- companion to revoked_reason (conv § 16)
    created_by      TEXT NOT NULL               -- 签发 actor（user:<n> / system / 等）
);

-- 同 worker_id 同时至多 1 active token （核心不变量；DB 兜底）
CREATE UNIQUE INDEX uniq_bootstrap_tokens_active_per_worker
    ON bootstrap_tokens (worker_id)
    WHERE status = 'active';

-- 反查 value 用（exchange 时）
CREATE UNIQUE INDEX uniq_bootstrap_tokens_value_hash
    ON bootstrap_tokens (value_hash);

-- 查 worker 的全部 token（含历史）
CREATE INDEX idx_bootstrap_tokens_worker
    ON bootstrap_tokens (worker_id);

-- 扫过期 token
CREATE INDEX idx_bootstrap_tokens_expires
    ON bootstrap_tokens (expires_at)
    WHERE status = 'active';

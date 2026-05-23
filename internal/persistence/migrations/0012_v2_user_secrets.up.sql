-- 0012_v2_user_secrets.up.sql — Phase 8 (v2) SecretManagement BC8 — UserSecret AR
--
-- 覆盖 ADR-0026 § 2：UserSecret AR + AES-GCM ciphertext + state machine (active/revoked)
--
-- 不变量（应用层 / DB 双层保证）：
--   - name 全局唯一
--   - 明文绝不入此表（仅 value_ciphertext + value_nonce）
--   - revoked 是终态（应用层）
--   - rotate 是更新 value，不改 state

CREATE TABLE user_secrets (
    id                  TEXT PRIMARY KEY,           -- ULID
    name                TEXT NOT NULL,              -- 全局唯一
    kind                TEXT NOT NULL,              -- mcp | cloud_credential | repo_deploy_key | other
    value_ciphertext    BLOB NOT NULL,              -- AES-GCM 256-bit ciphertext
    value_nonce         BLOB NOT NULL,              -- AES-GCM nonce (12 bytes)
    state               TEXT NOT NULL,              -- active | revoked
    created_at          TEXT NOT NULL,
    created_by          TEXT NOT NULL,              -- 创建者 actor (user:<n> / system / etc.)
    last_used_at        TEXT,                       -- 最后被 SecretResolutionService.resolve 的时间
    rotated_at          TEXT,                       -- 最后一次 rotate 的时间
    revoked_at          TEXT,                       -- 非 NULL 当 state=revoked
    revoked_by          TEXT,                       -- companion to revoked_at
    revoked_reason      TEXT,                       -- closed enum
    revoked_message     TEXT,                       -- companion to revoked_reason (conv § 16)
    version             INTEGER NOT NULL DEFAULT 1
);

-- name 全局唯一 per ADR-0026 § 2
CREATE UNIQUE INDEX uniq_user_secrets_name ON user_secrets (name);

-- 按 kind / state 列 (CLI list)
CREATE INDEX idx_user_secrets_kind_state ON user_secrets (kind, state);

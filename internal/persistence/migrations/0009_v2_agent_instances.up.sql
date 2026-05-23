-- 0009_v2_agent_instances.up.sql — Phase 8 (v2) AgentInstance 一等公民化
--
-- 覆盖 ADR-0024 § 1 + ADR-0029 § 1：
--   - AgentInstance AR with state machine (idle/active/sleeping/archived)
--   - is_builtin field 区分 Supervisor (built-in) vs worker agent
--   - CHECK constraint: (is_builtin=false AND worker_id NOT NULL) XOR (is_builtin=true AND worker_id NULL)
--
-- 不变量（应用层 / DB 双层保证）：
--   - name 全局唯一（v2 单租户）
--   - is_builtin / name / agent_cli 创建后不可变（应用层）
--   - archived 终态不可逆（应用层）
--   - built-in archive 拒绝（应用层）

CREATE TABLE agent_instances (
    id                  TEXT PRIMARY KEY,           -- ULID
    name                TEXT NOT NULL,              -- 全局唯一
    agent_cli           TEXT NOT NULL,              -- claude-code | codex | opencode | ...
    worker_id           TEXT,                       -- FK → workers; NULLABLE iff is_builtin=true
    config              TEXT NOT NULL DEFAULT '{}', -- JSON { instructions_ref?, mcp_config?, skills?, ... }
    max_concurrent      INTEGER,                    -- null = 无 cap，受 Worker.concurrency 兜底
    state               TEXT NOT NULL,              -- idle | active | sleeping | archived
    is_builtin          INTEGER NOT NULL DEFAULT 0, -- 0 = false (worker agent), 1 = true (system-provisioned, e.g. supervisor)
    created_at          TEXT NOT NULL,
    archived_at         TEXT,                       -- 非 NULL 当 state=archived
    archived_reason     TEXT,                       -- closed enum
    archived_message    TEXT,                       -- companion to archived_reason (conv § 16)
    version             INTEGER NOT NULL DEFAULT 1,

    -- CHECK: built-in 必须 worker_id=NULL；非 built-in 必须 worker_id NOT NULL
    -- per ADR-0029 § 1
    CHECK (
        (is_builtin = 0 AND worker_id IS NOT NULL) OR
        (is_builtin = 1 AND worker_id IS NULL)
    )
);

-- name 全局唯一 per ADR-0024 § 9.1
CREATE UNIQUE INDEX uniq_agent_instances_name ON agent_instances (name);

-- 查 worker 上的 agent (state filter common)
CREATE INDEX idx_agent_instances_worker_state ON agent_instances (worker_id, state)
    WHERE worker_id IS NOT NULL;

-- 查 built-in 用 (主要 supervisor 1 行)
CREATE INDEX idx_agent_instances_builtin ON agent_instances (name)
    WHERE is_builtin = 1;

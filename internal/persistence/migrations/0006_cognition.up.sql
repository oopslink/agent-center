-- 0006_cognition.up.sql — Phase 6 Cognition (Supervisor + DecisionRecord)
--
-- 落盘以下 BC 物理表（conventions § 9.z BC 物理隔离）：
--   - Cognition: supervisor_invocations (AR; SupervisorInvocation)
--   - Cognition: decision_records      (Entity; append-only)
--
-- 元层规则按 02-persistence-schema § 1-7：
--   - ULID 字符串 PK (TEXT)
--   - TEXT 存 ISO8601 时间戳
--   - TEXT 存 JSON（不在 SQL 里 json_extract）
--   - version INTEGER 默认 1（乐观锁 CAS）
--   - reason / message 平铺双字段（conventions § 16）
--   - 不声明 FOREIGN KEY（conventions § 9.w）；引用完整性由应用层 Repository / Domain Service 负责
--
-- 字段对位：
--   - supervisor_invocations: cognition/01-supervisor-invocation.md § 3
--   - decision_records:       cognition/01-supervisor-invocation.md § 4 (append-only)

-- =========================================================================
-- Cognition — supervisor_invocations (AR; 4-state lifecycle)
-- =========================================================================
CREATE TABLE supervisor_invocations (
    id                      TEXT PRIMARY KEY,           -- ULID
    scope_kind              TEXT NOT NULL,              -- task | issue | conversation | worker | global
    scope_key               TEXT NOT NULL,              -- task_id / issue_id / ... / '_global_'
    trigger_event_ids       TEXT NOT NULL,              -- JSON array of event_id ULIDs (≥1)
    status                  TEXT NOT NULL,              -- running | succeeded | failed | timed_out
    hard_timeout_seconds    INTEGER NOT NULL,           -- 180 | 600 per scope_kind
    started_at              TEXT NOT NULL,
    ended_at                TEXT,                       -- non-NULL when status terminal
    failed_reason           TEXT,                       -- closed enum: claude_nonzero | cli_command_error | oom | center_restart_orphan | killed_by_admin | unknown
    failed_message          TEXT,                       -- companion to failed_reason (conv § 16)
    timed_out_at            TEXT,                       -- non-NULL when status=timed_out
    token_usage             TEXT NOT NULL DEFAULT '{}', -- JSON {input,output,cache_read,cache_create}
    decisions_made          INTEGER NOT NULL DEFAULT 0,
    prompt_blob_ref         TEXT,                       -- BlobStore ref when prompt > threshold
    created_at              TEXT NOT NULL,
    updated_at              TEXT NOT NULL,
    version                 INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_invocations_scope ON supervisor_invocations (scope_kind, scope_key);
CREATE INDEX idx_invocations_status ON supervisor_invocations (status);
CREATE INDEX idx_invocations_started ON supervisor_invocations (started_at);
-- partial unique: at most one running invocation per (scope_kind, scope_key)
-- (ADR-0013 § 1: per-scope serialised execution)
CREATE UNIQUE INDEX uniq_invocations_running_per_scope
    ON supervisor_invocations (scope_kind, scope_key)
    WHERE status = 'running';

-- =========================================================================
-- Cognition — decision_records (Entity; append-only)
-- =========================================================================
-- 引用完整性由应用层 DecisionRecorder 在 INSERT 前校验 invocation_id 存在
CREATE TABLE decision_records (
    id                  TEXT PRIMARY KEY,           -- ULID
    invocation_id       TEXT NOT NULL,              -- references supervisor_invocations(id), enforced at app layer
    kind                TEXT NOT NULL,              -- closed enum: 12 DecisionKind values
    target_refs         TEXT NOT NULL DEFAULT '{}', -- JSON; refs to TaskID / IssueID / ExecutionID / ...
    rationale           TEXT NOT NULL,              -- non-empty by AR invariant
    outcome             TEXT NOT NULL,              -- succeeded | failed
    outcome_message     TEXT,                       -- required when outcome=failed (conv § 16)
    created_at          TEXT NOT NULL
);
CREATE INDEX idx_decisions_invocation ON decision_records (invocation_id);
CREATE INDEX idx_decisions_kind ON decision_records (kind);
CREATE INDEX idx_decisions_created ON decision_records (created_at);

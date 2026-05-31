-- 0001_init.up.sql — Phase 1 Shared Kernel + Events 总线
--
-- 落盘以下 BC 物理表（conventions § 9.z BC 物理隔离）：
--   - Observability:    events
--   - Workforce:        workers, worker_project_mappings, worker_project_proposals, projects
--   - Conversation:     conversations, messages
--
-- 元层规则按 02-persistence-schema § 1-7：
--   - ULID 字符串 PK (TEXT)
--   - TEXT 存 ISO8601 时间戳
--   - INTEGER 0/1 boolean
--   - TEXT 存 JSON（不在 SQL 里 json_extract）
--   - version INTEGER 默认 1（乐观锁 CAS）
--   - reason / message 平铺双字段（conventions § 16）
--   - 不声明 FOREIGN KEY（conventions § 9.w）；引用完整性由应用层 Repository / Domain Service 负责

-- =========================================================================
-- Observability — events 表（append-only；02-persistence § 8.2.1）
-- =========================================================================
CREATE TABLE events (
    id              TEXT PRIMARY KEY,
    occurred_at     TEXT NOT NULL,
    seq             INTEGER NOT NULL,
    event_type      TEXT NOT NULL,
    refs            TEXT NOT NULL DEFAULT '{}',
    actor           TEXT NOT NULL,
    payload         TEXT NOT NULL DEFAULT '{}',
    correlation_id  TEXT,
    decision_id     TEXT,
    created_at      TEXT NOT NULL
);
-- append-only：无 version 列、不暴露 UPDATE / DELETE 路径
CREATE INDEX idx_events_occurred_at ON events (occurred_at);
CREATE INDEX idx_events_type        ON events (event_type);
CREATE INDEX idx_events_correlation ON events (correlation_id) WHERE correlation_id IS NOT NULL;
CREATE INDEX idx_events_decision    ON events (decision_id)    WHERE decision_id    IS NOT NULL;
CREATE UNIQUE INDEX uniq_events_seq ON events (seq);

-- =========================================================================
-- Workforce — workers
-- v2.7 #131: the legacy `projects` / `worker_project_mappings` /
-- `worker_project_proposals` tables are RETIRED (project mgmt → pm_projects;
-- worker↔project mapping/dispatch removed). Fresh install is new-model only —
-- these tables are never created (no drop-migration; nothing to drop).
-- =========================================================================

-- workers
CREATE TABLE workers (
    id                    TEXT PRIMARY KEY,                 -- worker_id（用户配置，全局唯一）
    status                TEXT NOT NULL,                    -- online | offline
    capabilities          TEXT NOT NULL DEFAULT '[]',       -- JSON：list of agent_cli kinds
    last_heartbeat_at     TEXT,
    working_seconds       INTEGER NOT NULL DEFAULT 0,
    enrolled_at           TEXT NOT NULL,
    online_at             TEXT,
    offline_at            TEXT,
    offline_reason        TEXT,
    offline_message       TEXT,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    version               INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_workers_status ON workers (status);

-- =========================================================================
-- Conversation — conversations, messages
-- =========================================================================

-- conversations: AR（conversation/01 § 3）
CREATE TABLE conversations (
    id                              TEXT PRIMARY KEY,
    kind                            TEXT NOT NULL,         -- dm | group_thread | adhoc | notification | task | issue
    title                           TEXT,
    primary_channel_hint            TEXT,
    primary_channel_thread_key      TEXT,
    status                          TEXT NOT NULL,         -- open | closed
    opened_at                       TEXT NOT NULL,
    closed_at                       TEXT,
    closed_reason                   TEXT,
    closed_message                  TEXT,
    created_at                      TEXT NOT NULL,
    updated_at                      TEXT NOT NULL,
    version                         INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_conversations_kind   ON conversations (kind);
CREATE INDEX idx_conversations_status ON conversations (status);
CREATE UNIQUE INDEX uniq_conversations_channel_thread
    ON conversations (primary_channel_hint, primary_channel_thread_key)
    WHERE primary_channel_hint       IS NOT NULL
      AND primary_channel_thread_key IS NOT NULL;

-- messages: Conversation 子从属（conversation/01 § 4）
-- 引用完整性由应用层 Repository / Domain Service 负责（conventions § 9.w）
CREATE TABLE messages (
    id                      TEXT PRIMARY KEY,
    conversation_id         TEXT NOT NULL,                 -- references conversations(id), enforced at app layer
    sender_identity_id      TEXT NOT NULL,
    content_kind            TEXT NOT NULL,                 -- text/system/agent_finding/supervisor_summary/conclusion_draft/task_proposal
    content                 TEXT NOT NULL,
    direction               TEXT NOT NULL,                 -- inbound | outbound | internal
    vendor_msg_ref          TEXT,
    input_request_ref       TEXT,
    posted_at               TEXT NOT NULL,
    created_at              TEXT NOT NULL
);
CREATE INDEX idx_messages_conv      ON messages (conversation_id, posted_at);
CREATE UNIQUE INDEX uniq_messages_vendor_ref
    ON messages (vendor_msg_ref)
    WHERE vendor_msg_ref IS NOT NULL;

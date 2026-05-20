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
-- Workforce — projects, workers, worker_project_mappings, worker_project_proposals
-- =========================================================================

-- projects: slug PK（workforce/02-project § 2）
CREATE TABLE projects (
    id                          TEXT PRIMARY KEY,           -- slug
    name                        TEXT NOT NULL,
    kind                        TEXT,                       -- coding | writing | investing | NULL
    default_agent_cli           TEXT,
    description                 TEXT,
    created_at                  TEXT NOT NULL,
    updated_at                  TEXT NOT NULL,
    created_by_identity_id      TEXT NOT NULL,
    version                     INTEGER NOT NULL DEFAULT 1
);

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

-- worker_project_mappings: Worker 子从属 entity（workforce/01 § 4）
CREATE TABLE worker_project_mappings (
    id                    TEXT PRIMARY KEY,
    worker_id             TEXT NOT NULL,
    project_id            TEXT NOT NULL,
    base_path             TEXT NOT NULL,
    source_proposal_id    TEXT,
    status                TEXT NOT NULL,                    -- active | invalidated
    invalidate_reason     TEXT,                             -- path_missing | not_git_repo | manual_remove
    invalidate_message    TEXT,
    added_at              TEXT NOT NULL,
    invalidated_at        TEXT,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    version               INTEGER NOT NULL DEFAULT 1,
    FOREIGN KEY (worker_id)  REFERENCES workers(id),
    FOREIGN KEY (project_id) REFERENCES projects(id)
);
CREATE INDEX idx_mappings_worker  ON worker_project_mappings (worker_id);
CREATE INDEX idx_mappings_project ON worker_project_mappings (project_id);
-- 单对 (worker_id, project_id) 至多 1 条 active（workforce/01 § 4.5）
CREATE UNIQUE INDEX uniq_mappings_active
    ON worker_project_mappings (worker_id, project_id)
    WHERE status = 'active';

-- worker_project_proposals: 独立 AR（workforce/03 § 2）
CREATE TABLE worker_project_proposals (
    id                          TEXT PRIMARY KEY,
    worker_id                   TEXT NOT NULL,
    candidate_path              TEXT NOT NULL,
    suggested_project_id        TEXT NOT NULL,
    suggested_kind              TEXT,
    candidate_metadata          TEXT NOT NULL DEFAULT '{}', -- JSON
    status                      TEXT NOT NULL,              -- pending | accepted | ignored | superseded
    proposed_at                 TEXT NOT NULL,
    reviewed_at                 TEXT,
    reviewed_by_identity_id     TEXT,
    resulting_mapping_id        TEXT,
    created_at                  TEXT NOT NULL,
    updated_at                  TEXT NOT NULL,
    version                     INTEGER NOT NULL DEFAULT 1,
    FOREIGN KEY (worker_id) REFERENCES workers(id)
);
CREATE INDEX idx_proposals_worker        ON worker_project_proposals (worker_id);
CREATE INDEX idx_proposals_status        ON worker_project_proposals (status);
-- 同 (worker_id, candidate_path) 至多 1 条非终态 proposal（workforce/03 § 6.1）
CREATE UNIQUE INDEX uniq_proposals_active_path
    ON worker_project_proposals (worker_id, candidate_path)
    WHERE status IN ('pending');

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
CREATE TABLE messages (
    id                      TEXT PRIMARY KEY,
    conversation_id         TEXT NOT NULL,
    sender_identity_id      TEXT NOT NULL,
    content_kind            TEXT NOT NULL,                 -- text/system/agent_finding/supervisor_summary/conclusion_draft/task_proposal
    content                 TEXT NOT NULL,
    direction               TEXT NOT NULL,                 -- inbound | outbound | internal
    vendor_msg_ref          TEXT,
    input_request_ref       TEXT,
    posted_at               TEXT NOT NULL,
    created_at              TEXT NOT NULL,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id)
);
CREATE INDEX idx_messages_conv      ON messages (conversation_id, posted_at);
CREATE UNIQUE INDEX uniq_messages_vendor_ref
    ON messages (vendor_msg_ref)
    WHERE vendor_msg_ref IS NOT NULL;

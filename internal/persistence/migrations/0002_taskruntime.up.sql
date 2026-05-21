-- 0002_taskruntime.up.sql — Phase 2 TaskRuntime Core
--
-- 落盘以下 BC 物理表（conventions § 9.z BC 物理隔离）：
--   - TaskRuntime: tasks, task_executions, input_requests, artifacts
--
-- 元层规则按 02-persistence-schema § 1-7：
--   - ULID 字符串 PK (TEXT)
--   - TEXT 存 ISO8601 时间戳
--   - INTEGER 0/1 boolean
--   - TEXT 存 JSON
--   - version INTEGER 默认 1（乐观锁 CAS）
--   - reason / message 平铺双字段（conventions § 16）
--   - 不声明 FOREIGN KEY（conventions § 9.w）；引用完整性由应用层 Repository / Domain Service 负责

-- =========================================================================
-- tasks — AR (01-task § 5)
-- 引用完整性由应用层 Repository / Domain Service 负责（conventions § 9.w）
-- =========================================================================
CREATE TABLE tasks (
    id                       TEXT PRIMARY KEY,
    project_id               TEXT NOT NULL,                 -- references projects(id), enforced at app layer
    parent_task_id           TEXT,                          -- references tasks(id), enforced at app layer
    from_issue_id            TEXT,
    title                    TEXT NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    description_blob_ref     TEXT,
    status                   TEXT NOT NULL,                 -- open | suspended | done | abandoned
    priority                 TEXT NOT NULL DEFAULT 'medium', -- high | medium | low
    eta_at                   TEXT,
    requires_worktree        INTEGER NOT NULL DEFAULT 1,    -- 0/1 boolean
    depends_on_task_ids      TEXT NOT NULL DEFAULT '[]',    -- JSON array of task ids
    abandoned_reason         TEXT,
    abandoned_message        TEXT,
    conversation_id          TEXT,                          -- 1:1 强引用（ADR-0017），enforced at app layer
    current_execution_id     TEXT,
    created_by               TEXT NOT NULL,
    created_at               TEXT NOT NULL,
    updated_at               TEXT NOT NULL,
    version                  INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_tasks_project_status ON tasks (project_id, status);
CREATE INDEX idx_tasks_parent ON tasks (parent_task_id) WHERE parent_task_id IS NOT NULL;
CREATE INDEX idx_tasks_issue  ON tasks (from_issue_id)  WHERE from_issue_id  IS NOT NULL;
CREATE INDEX idx_tasks_conv   ON tasks (conversation_id) WHERE conversation_id IS NOT NULL;

-- =========================================================================
-- task_executions — AR (02-task-execution § 5.1)
-- =========================================================================
CREATE TABLE task_executions (
    id                          TEXT PRIMARY KEY,
    task_id                     TEXT NOT NULL,
    worker_id                   TEXT NOT NULL,
    agent_cli                   TEXT NOT NULL,             -- claude-code | codex | opencode
    workspace_mode              TEXT NOT NULL,             -- worktree | direct
    cwd                         TEXT,
    branch_name                 TEXT,
    base_branch                 TEXT,
    priority                    TEXT NOT NULL DEFAULT 'medium',
    eta_at                      TEXT,
    execution_timeout_override  INTEGER,                   -- seconds
    working_seconds_accumulated INTEGER NOT NULL DEFAULT 0,
    status                      TEXT NOT NULL,             -- submitted | working | input_required | completed | failed | killed
    dispatch_state              TEXT NOT NULL DEFAULT '',  -- pending_ack | acked | '' (after acked / terminal)
    pending_input_request_id    TEXT,
    started_at                  TEXT NOT NULL,
    working_started_at          TEXT,
    cancel_requested_at         TEXT,
    cancel_reason               TEXT,
    cancel_message              TEXT,
    ended_at                    TEXT,
    completed_reason            TEXT,
    completed_message           TEXT,
    failed_reason               TEXT,
    failed_message              TEXT,
    killed_reason               TEXT,
    killed_message              TEXT,
    created_at                  TEXT NOT NULL,
    updated_at                  TEXT NOT NULL,
    version                     INTEGER NOT NULL DEFAULT 1
    -- task_id references tasks(id); worker_id references workers(id) — enforced at app layer
);
CREATE INDEX idx_task_executions_task          ON task_executions (task_id);
CREATE INDEX idx_task_executions_worker_status ON task_executions (worker_id, status);
CREATE INDEX idx_task_executions_active        ON task_executions (status)
    WHERE status IN ('submitted', 'working', 'input_required');
CREATE INDEX idx_task_executions_dispatch
    ON task_executions (dispatch_state, created_at)
    WHERE dispatch_state = 'pending_ack';

-- =========================================================================
-- input_requests — AR (03-input-request § 3)
-- =========================================================================
CREATE TABLE input_requests (
    id                  TEXT PRIMARY KEY,
    task_execution_id   TEXT NOT NULL,
    status              TEXT NOT NULL,                 -- pending | responded | timed_out | canceled
    question            TEXT NOT NULL,
    options             TEXT,                          -- JSON array | NULL
    urgency             TEXT NOT NULL DEFAULT 'normal',
    requested_at        TEXT NOT NULL,
    responded_at        TEXT,
    responded_by        TEXT,
    response_text       TEXT,
    ended_reason        TEXT,
    ended_message       TEXT,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    version             INTEGER NOT NULL DEFAULT 1
    -- task_execution_id references task_executions(id) — enforced at app layer
);
CREATE INDEX idx_input_requests_execution ON input_requests (task_execution_id);
CREATE INDEX idx_input_requests_status    ON input_requests (status);

-- =========================================================================
-- artifacts — TaskExecution 子实体（append-only；02-task-execution § 12）
-- =========================================================================
CREATE TABLE artifacts (
    id                TEXT PRIMARY KEY,
    task_id           TEXT NOT NULL,                  -- references tasks(id), enforced at app layer
    execution_id      TEXT NOT NULL,                  -- references task_executions(id), enforced at app layer
    kind              TEXT NOT NULL,                  -- pr_url | file | report | diff | test_run | note | ...
    title             TEXT NOT NULL,
    blob_ref          TEXT,
    url               TEXT,
    metadata_json     TEXT NOT NULL DEFAULT '{}',
    created_at        TEXT NOT NULL,
    created_by        TEXT NOT NULL
);
CREATE INDEX idx_artifacts_task      ON artifacts (task_id);
CREATE INDEX idx_artifacts_execution ON artifacts (execution_id, created_at);

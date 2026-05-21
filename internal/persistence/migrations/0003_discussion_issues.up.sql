-- 0003_discussion_issues.up.sql — Phase 3 Discussion Core
--
-- 落盘 Discussion BC 物理表（conventions § 9.z BC 物理隔离）：
--   - Discussion: issues
--
-- 元层规则按 02-persistence-schema § 1-7：
--   - ULID 字符串 PK (TEXT)
--   - TEXT 存 ISO8601 时间戳
--   - INTEGER 0/1 boolean
--   - TEXT 存 JSON
--   - version INTEGER 默认 1（乐观锁 CAS）
--   - reason / message 平铺双字段（conventions § 16）
--   - 不声明 FOREIGN KEY（conventions § 9.w）；引用完整性由应用层 Repository / Domain Service 负责
--
-- 字段对位 discussion/00-overview § 1.1。description 长文走 BlobStore；本表只
-- 存 description（短文 ≤ 10KB）或 description_blob_ref（长文外存）；二者不
-- 同时非空。

CREATE TABLE issues (
    id                          TEXT PRIMARY KEY,
    project_id                  TEXT NOT NULL,                 -- references projects(id), enforced at app layer
    title                       TEXT NOT NULL,
    description                 TEXT NOT NULL DEFAULT '',
    description_blob_ref        TEXT,
    opened_by_identity_id       TEXT NOT NULL,                 -- 'user:<id>' | 'supervisor:<id>' | 'agent:<id>'
    origin                      TEXT NOT NULL,                 -- cli | web_console | feishu_at | supervisor | agent_open_issue
    opened_at                   TEXT NOT NULL,
    status                      TEXT NOT NULL,                 -- open | under_discussion | concluded | closed_no_action | closed_with_tasks | withdrawn
    concluded_at                TEXT,
    conclusion_summary          TEXT,
    concluded_by_identity_id    TEXT,
    withdraw_reason             TEXT,                           -- conventions § 16 reason+message 平铺
    withdraw_message            TEXT,
    conversation_id             TEXT,                           -- 1:1 强引用 conversations(id) kind=issue；enforced at app layer
    related_conversation_ids    TEXT NOT NULL DEFAULT '[]',     -- JSON array, weak lineage refs
    created_at                  TEXT NOT NULL,
    updated_at                  TEXT NOT NULL,
    version                     INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_issues_project_status ON issues (project_id, status);
CREATE INDEX idx_issues_status         ON issues (status);
CREATE INDEX idx_issues_opener         ON issues (opened_by_identity_id);
CREATE INDEX idx_issues_conv           ON issues (conversation_id) WHERE conversation_id IS NOT NULL;

-- 0100_v229_pm_audit_log.up.sql — the通用变更记录 / 审计账本 (change-log / audit-trail),
-- design docs/design/features/2026-07-03-change-log-audit-design.md.
--
-- Object-level SEMANTIC change ledger for issue / task / plan: "谁、什么时候、把什么
-- 从 X 改成了 Y". One generic table (object_type ∈ {issue,task,plan}) + one write path +
-- one read API. Distinct from and orthogonal to the three existing streams:
--   - outbox_events (0040)        — transactional infra outbox, cleared after relay.
--   - agent_activity_events (0043)— agent RUNTIME trace (thinking/tool_use/lifecycle), GC'd.
--   - pm_task_action_logs (0070)  — task-only action log feeding the overdue reminder.
-- This ledger is object-keyed, low-frequency, human-readable, and PERMANENT.
--
-- Shape mirrors the append-only成例 pm_task_action_logs (0070): ULID PK, occurred_at
-- TEXT (RFC3339), NO version / NO updated_at / NO FK REFERENCES (the whole schema is
-- uniformly FK-free / app-managed; object_id is a logical reference). Generalized
-- fields: object_type/object_id, change_type (enum §4.2), field/from_value/to_value
-- for the "X→Y" diff, actor_ref (谁改的; 'system:<reconciler>' for系统驱动), and a JSON
-- detail blob for structured extras (dep kind/when, gate round, note).
--
-- UPGRADE SAFETY: purely additive — a new empty table + two lookup indexes, nothing
-- else touched. Empty table + no write point exercised = 现状零影响 (纯加法、零回归).
--
-- DIALECT NOTE: CREATE TABLE + CREATE INDEX in the SQLite + PG common subset (mirrors
-- 0070). NOT NULL DEFAULT '' on detail matches the branch convention (repo binds it as
-- a plain string, never NULL).
CREATE TABLE pm_audit_log (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id   TEXT NOT NULL,
    change_type TEXT NOT NULL,
    field       TEXT NOT NULL DEFAULT '',
    from_value  TEXT NOT NULL DEFAULT '',
    to_value    TEXT NOT NULL DEFAULT '',
    actor_ref   TEXT NOT NULL,
    detail      TEXT NOT NULL DEFAULT '{}',
    occurred_at TEXT NOT NULL
);

-- Primary read path: an object's ledger, time-ordered (the §6 read API pages on this).
CREATE INDEX idx_pm_audit_log_object ON pm_audit_log (object_type, object_id, occurred_at);
-- Project-scoped scan (per-project GC boundary / future project-wide audit view).
CREATE INDEX idx_pm_audit_log_project ON pm_audit_log (project_id, occurred_at);

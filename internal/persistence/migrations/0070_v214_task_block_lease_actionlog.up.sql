-- 0070_v214_task_block_lease_actionlog.up.sql — v2.14.0 I14/F2 (issue §7 + §13.F-①).
-- Migration A (schema-ADD only) of the "remove AgentWorkItem, fold work-item state
-- into pm_tasks" refactor. EVERYTHING here is additive: three new pm_tasks columns
-- (existing rows backfill to '' / '' / NULL = "not blocked, no live lease"), two
-- non-unique lookup indexes, and the empty pm_task_action_logs table. It does NOT
-- touch the status set / check, and drops nothing.
--
-- Migration ordering (PD ruling 2026-06-21): A=0070 (this) → B=0071 (F4 data
-- backfill) → F3 single-active UNIQUE index=0072 → C=0073 (F7 drop legacy tables).
-- NOTE: the §13.B/§13.F-① single-active UNIQUE index (idx_pm_tasks_one_active_per_agent)
-- is DELIBERATELY NOT here — it only holds once F3 reworks ClaimPoolTask to claim→open
-- and adds the start-time single-active gate; landing it in F2 would break the running
-- flow (pool cap=3 still sets running). It moves to F3's 0072, and F4's 0071 backfill
-- must first demote any agent's surplus running tasks to open so 0072 builds clean.
--
--   blocked_reason_type        — classifies the existing blocked_reason annotation:
--                                'input_required' | 'obstacle' | '' (not blocked).
--   blocked_comment            — Unblock payload: the user's reply (input_required)
--                                or the owner/PM's note (obstacle); '' until unblocked.
--   execution_lease_expires_at — the agent execution-lease deadline; NULL == no live
--                                lease (lease is cleared on block). Stored as RFC3339
--                                TEXT, matching every other timestamp column.
-- blocked_reason_type / blocked_comment are NOT NULL DEFAULT '' to mirror the
-- branch/base convention (repo binds them as plain strings, not nullString).
ALTER TABLE pm_tasks ADD COLUMN blocked_reason_type TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_tasks ADD COLUMN blocked_comment     TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_tasks ADD COLUMN execution_lease_expires_at TEXT;

-- Lookup index for "an agent's actionable tasks" (list_my_tasks / the §13.A runnable
-- gate). Non-unique — the single-active HARD constraint is F3's separate UNIQUE index
-- (0072), not this one.
CREATE INDEX idx_pm_tasks_assignee_status
    ON pm_tasks (assignee, status)
    WHERE status IN ('open', 'running');

-- Lookup index for an agent's blocked-but-running tasks. NOTE: blocked_reason is
-- stored NULL (not '') when empty — the repo binds it via nullString — so "is
-- blocked" must be written `blocked_reason IS NOT NULL AND blocked_reason <> ''`.
CREATE INDEX idx_pm_tasks_assignee_running_blocked
    ON pm_tasks (assignee)
    WHERE status = 'running' AND blocked_reason IS NOT NULL AND blocked_reason <> '';

-- §7.3 task action logs: immutable, append-only Task lifecycle record (assigned /
-- reassigned / agent_started / blocked / unblocked / lease_expired / completed),
-- replacing the deleted agent_work_item_transitions. pm_-prefixed to match every
-- other PM table. No FK REFERENCES: this schema is uniformly FK-free / app-managed
-- (zero migrations use REFERENCES, even with foreign_keys=ON), so task_id is a
-- logical reference to pm_tasks(id).
CREATE TABLE pm_task_action_logs (
    id          TEXT PRIMARY KEY,
    task_id     TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    action      TEXT NOT NULL,
    actor_ref   TEXT NOT NULL,
    agent_ref   TEXT NOT NULL DEFAULT '',
    note        TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_pm_task_action_logs_task_id ON pm_task_action_logs (task_id, occurred_at);

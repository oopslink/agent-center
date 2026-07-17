-- 0111_adr54_parked_task_indexes.up.sql — ADR-0054 (I107): widen the two 0070 PARTIAL
-- indexes on pm_tasks so they still cover the task-status set after `delivered` and
-- `blocked` were added as real non-terminal states.
--
-- There is NO data migration here, and none is possible or wanted:
--
--   * pm_tasks.status is a free TEXT column with NO CHECK constraint (see
--     0052_v281_task_issue_state_model_fix.up.sql), so the two new status VALUES need no
--     DDL at all — they just start being written.
--   * Existing rows are LEFT ALONE ON PURPOSE. A task parked the ADR-0046 way (status =
--     'running' + a non-empty blocked_reason) keeps that shape, and every ADR-0054
--     consumer is written to accept BOTH shapes — it lists {blocked, running} and then
--     filters on blocked_reason, so a legacy row is still found, still reminded, still
--     unblockable. Rewriting live rows' status would be a far riskier change than
--     tolerating two shapes, and 0058 already proved the direction is lossy: it did
--     `UPDATE pm_tasks SET status='running' WHERE status='blocked'`, destroying the
--     distinction it collapsed. Nothing can recover those pre-0058 rows, and this
--     migration does not pretend to.
--
-- The indexes below are the ONLY thing that actually needs changing: both are PARTIAL
-- (WHERE-clauses naming statuses literally), so a parked task silently drops out of them.
-- That is a performance-and-correctness-of-plan issue, not of results — SQLite just falls
-- back to a scan — except for the second one, which exists SOLELY to find blocked tasks
-- and would otherwise index nothing at all now that a blocked task is not 'running'.
-- Partial indexes cannot be ALTERed, so each is dropped and recreated.

-- "An agent's actionable/active tasks" (the list_my_tasks + §13.A runnable-gate lookup,
-- and the CountActiveByAssignee / ListActiveByAssignee backlog pair). The predicate must
-- mirror those queries' status IN (...) list: a parked task is still the agent's ASSIGNED
-- work and is counted in their backlog, so it belongs in this index.
-- Non-unique — the single-active HARD constraint was F3's separate UNIQUE index (0072,
-- since dropped by 0084), never this one.
DROP INDEX IF EXISTS idx_pm_tasks_assignee_status;
CREATE INDEX idx_pm_tasks_assignee_status
    ON pm_tasks (assignee, status)
    WHERE status IN ('open', 'running', 'delivered', 'blocked');

-- "An agent's blocked tasks" (the overdue-block reminder sweep + the Alerts rail). Under
-- ADR-0054 a blocked task has status='blocked'; 'running' stays in the predicate so the
-- LEGACY annotation-shaped rows described above remain indexed by the same lookup.
-- NOTE (unchanged from 0070): blocked_reason is stored NULL (not '') when empty — the
-- repo binds it via nullString — so "is blocked" must be written
-- `blocked_reason IS NOT NULL AND blocked_reason <> ''`.
DROP INDEX IF EXISTS idx_pm_tasks_assignee_running_blocked;
CREATE INDEX idx_pm_tasks_assignee_running_blocked
    ON pm_tasks (assignee)
    WHERE status IN ('running', 'blocked') AND blocked_reason IS NOT NULL AND blocked_reason <> '';

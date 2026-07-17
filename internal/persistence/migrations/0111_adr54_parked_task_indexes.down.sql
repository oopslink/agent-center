-- 0111_adr54_parked_task_indexes.down.sql — restore the two 0070 partial indexes to their
-- pre-ADR-0054 predicates verbatim.
--
-- Index-only, so the down is lossless: 0111's up migrated no data (see its header — the
-- new `delivered`/`blocked` statuses need no DDL because pm_tasks.status is free TEXT),
-- and narrowing a partial index back only removes rows from the index, never from the
-- table. Any task already parked as status='delivered'/'blocked' keeps its row and simply
-- stops being covered by these indexes — which is exactly the pre-0111 state.
DROP INDEX IF EXISTS idx_pm_tasks_assignee_status;
CREATE INDEX idx_pm_tasks_assignee_status
    ON pm_tasks (assignee, status)
    WHERE status IN ('open', 'running');

DROP INDEX IF EXISTS idx_pm_tasks_assignee_running_blocked;
CREATE INDEX idx_pm_tasks_assignee_running_blocked
    ON pm_tasks (assignee)
    WHERE status = 'running' AND blocked_reason IS NOT NULL AND blocked_reason <> '';

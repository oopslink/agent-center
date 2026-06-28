-- 0084_v218_drop_task_single_active_unique.down.sql — recreate the v2.14.0
-- single-active partial UNIQUE index (the exact 0072 definition).
--
-- KNOWN DOWN LIMITATION: recreating a UNIQUE index requires the current data to
-- already satisfy it — i.e. each agent must have ≤1 running, non-blocked task at
-- down time. After running under the ≤N cap an agent may legitimately have
-- several running tasks, in which case this down migration FAILS to build the
-- index. That is expected: down is a dev/rollback convenience, not a production
-- path, and rolling back the concurrency relaxation onto data that used it is
-- inherently lossy. Demote surplus running tasks to 'open' before rolling back if
-- needed (mirrors how 0071 demoted surplus before 0072 first built this index).

CREATE UNIQUE INDEX idx_pm_tasks_one_active_per_agent
    ON pm_tasks (assignee)
    WHERE status = 'running' AND (blocked_reason IS NULL OR blocked_reason = '');

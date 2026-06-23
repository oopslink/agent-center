-- 0072_v214_task_single_active_unique.up.sql — v2.14.0 I14/F3 (issue §13.B + §13.F-①).
-- The single-active HARD constraint: an agent may have at most ONE running,
-- non-blocked Task at a time. This restores, on the pm_tasks model, the invariant
-- the deleted 0051_agent_work_item_single_active_unique UNIQUE partial index used
-- to guarantee on agent_work_items (issue §13.B: "单活硬约束不能降级为软策略").
--
-- Migration ordering (PD ruling 2026-06-21): A=0070 (schema add) → B=0071 (F4 data
-- backfill + surplus-running demotion) → this 0072 (F3 single-active UNIQUE) →
-- C=0073 (F7 drop legacy tables). 0071 MUST run first: it demotes each agent's
-- surplus running tasks to 'open' so this UNIQUE index builds clean (the legacy
-- pool cap=3 allowed several concurrent running tasks per agent).
--
-- NULL-aware predicate (issue §13.B adapted to this repo's storage): the repo binds
-- an EMPTY blocked_reason as NULL (nullString), not '', so the literal
-- `blocked_reason = ''` from the spec would miss every NULL row and silently leak
-- the single-active guarantee. The correct predicate matches BOTH forms:
-- `blocked_reason IS NULL OR blocked_reason = ''`. A BLOCKED running task does NOT
-- occupy the active slot (§13.F-①: "blocked 不占活槽") — it is excluded here, so an
-- agent can hold a blocked task plus one live running task.
--
-- Uniqueness is on assignee alone (within the partial predicate). SQLite treats
-- NULLs as distinct in a UNIQUE index, so unassigned running rows (assignee NULL)
-- never collide — but a running task always has an assignee in practice, so this
-- guards the real case: two non-blocked running tasks for the SAME agent.
CREATE UNIQUE INDEX idx_pm_tasks_one_active_per_agent
    ON pm_tasks (assignee)
    WHERE status = 'running' AND (blocked_reason IS NULL OR blocked_reason = '');

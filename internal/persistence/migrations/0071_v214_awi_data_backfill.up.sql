-- 0071_v214_awi_data_backfill.up.sql — v2.14.0 I14/F4 (issue §13.E + §六 Step6).
-- Migration B (DATA-only) of the "remove AgentWorkItem, fold work-item state into
-- pm_tasks" refactor. Reads the legacy agent_work_items rows and folds their live
-- state into the pm_tasks columns added by 0070 (migration A), then rebuilds a
-- best-effort pm_task_action_logs history. Runs automatically on boot via
-- Migrator.Up.
--
-- Migration ordering (PD ruling 2026-06-21): A=0070 → B=0071 (this) → F3
-- single-active UNIQUE index=0072 → C=0073 (F7 drop legacy tables). F4 MUST
-- integrate before F3, because step (3) below demotes each agent's surplus running
-- tasks to 'open' so F3's 0072 idx_pm_tasks_one_active_per_agent UNIQUE index
-- builds clean (the legacy pool cap=3 allowed several concurrent running tasks per
-- agent).
--
-- Mapping (legacy agent_work_items.status -> new pm_tasks annotation):
--   waiting_input        -> blocked_reason_type='input_required'
--   blocked | failed     -> blocked_reason_type='obstacle'
--   paused (in-flight)   -> status='open', lease/blocked cleared (§13.E)
--   active | done | ...  -> no annotation change
-- Conventions: SQLite; pm_ prefix; an EMPTY blocked_reason is stored as NULL (the
-- repo binds it via nullString), so a genuinely-blocked task whose legacy reason
-- text was empty gets a synthesized non-empty placeholder (a blocked annotation
-- with no reason would be invisible to 0070's "is blocked" indexes).
--
-- task_ref join: agent_work_items.task_ref is the URI form 'pm://tasks/<id>', so we
-- strip the 'pm://tasks/' prefix to match pm_tasks.id. agent_work_items.agent_id is
-- a bare id; pm_task_action_logs.agent_ref uses the 'agent:<id>' identity form.

-- (1a) waiting_input -> input_required. Only RUNNING tasks carry a block annotation
--      (post-0058 there is no 'blocked' status; stuck is a running-task annotation).
--      Synthesize a placeholder reason only when the existing one is NULL/empty.
UPDATE pm_tasks
SET blocked_reason_type = 'input_required',
    blocked_reason = CASE
        WHEN blocked_reason IS NULL OR blocked_reason = ''
        THEN '(migrated) awaiting user input'
        ELSE blocked_reason END,
    -- A blocked task holds no live lease (§2.3: Block clears the lease; the legacy
    -- rows have none anyway since execution_lease_expires_at is new in 0070).
    execution_lease_expires_at = NULL
WHERE status = 'running'
  AND id IN (
        SELECT substr(task_ref, length('pm://tasks/') + 1)
        FROM agent_work_items
        WHERE status = 'waiting_input'
  );

-- (1b) blocked | failed -> obstacle. The blocked_reason_type='' guard keeps an
--      input_required mapping from (1a) from being overwritten.
UPDATE pm_tasks
SET blocked_reason_type = 'obstacle',
    blocked_reason = CASE
        WHEN blocked_reason IS NULL OR blocked_reason = ''
        THEN '(migrated) needs owner/PM attention'
        ELSE blocked_reason END,
    execution_lease_expires_at = NULL
WHERE status = 'running'
  AND blocked_reason_type = ''
  AND id IN (
        SELECT substr(task_ref, length('pm://tasks/') + 1)
        FROM agent_work_items
        WHERE status IN ('blocked', 'failed')
  );

-- (2) §13.E in-flight paused -> Task back to 'open' (re-queueable), clearing the
--     lease and any block annotation. assignee is intentionally KEPT (the task is
--     still that agent's; PM re-dispatches). Done BEFORE (3) so paused rows are not
--     counted toward the single-active cap.
UPDATE pm_tasks
SET status = 'open',
    execution_lease_expires_at = NULL,
    blocked_reason = NULL,
    blocked_reason_type = '',
    blocked_comment = ''
WHERE status = 'running'
  AND id IN (
        SELECT substr(task_ref, length('pm://tasks/') + 1)
        FROM agent_work_items
        WHERE status = 'paused'
  );

-- (3) Single-active cleanup: the legacy pool cap=3 let an agent hold several
--     concurrent RUNNING tasks. F3's 0072 UNIQUE index allows at most one
--     running+non-blocked task per agent, so demote each agent's SURPLUS running
--     tasks to 'open' (clearing the lease), keeping the most-recently-updated one
--     live. BLOCKED running tasks do NOT occupy the active slot (0070's index is
--     WHERE blocked_reason=''), so they are excluded from both the count and the
--     demotion. Unassigned rows (no assignee) are not grouped.
UPDATE pm_tasks
SET status = 'open',
    execution_lease_expires_at = NULL
WHERE id IN (
    SELECT id FROM (
        SELECT id,
               ROW_NUMBER() OVER (
                   PARTITION BY assignee
                   ORDER BY updated_at DESC, id DESC
               ) AS rn
        FROM pm_tasks
        WHERE status = 'running'
          AND assignee IS NOT NULL AND assignee <> ''
          AND (blocked_reason IS NULL OR blocked_reason = '')
    )
    WHERE rn > 1
);

-- (4) Best-effort pm_task_action_logs backfill. The agent_work_item_transitions
--     table never had a persisted form (transitions fed an in-memory projector),
--     so history is reconstructed from the agent_work_items rows themselves: one
--     inferable record per work item (superseded -> 'reassigned', everything else
--     -> 'assigned'). History granularity is limited by design; realtime state
--     above is authoritative. Only logs for tasks that still exist in pm_tasks.
INSERT INTO pm_task_action_logs (id, task_id, occurred_at, action, actor_ref, agent_ref, note)
SELECT 'awimig-' || awi.id,
       substr(awi.task_ref, length('pm://tasks/') + 1),
       awi.created_at,
       CASE WHEN awi.status = 'superseded' THEN 'reassigned' ELSE 'assigned' END,
       'system:awi-migration',
       'agent:' || awi.agent_id,
       'rebuilt from agent_work_items (status=' || awi.status
           || ', interactions=' || awi.interactions || ')'
FROM agent_work_items awi
WHERE substr(awi.task_ref, length('pm://tasks/') + 1) IN (SELECT id FROM pm_tasks)
  AND 'awimig-' || awi.id NOT IN (SELECT id FROM pm_task_action_logs);

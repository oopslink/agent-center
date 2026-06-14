-- 0058_v291_builtin_plan.up.sql — v2.9.1 ADR-0047 (claimable built-in pool).
--
-- The per-project default "assignment pool" Plan: one per project, auto-created +
-- always-started, FLAT (no dependency edges), a "pull, no-wake" dispatch pool. It
-- cannot be stopped / archived / deleted on its own (it is archived WITH its
-- project). is_builtin marks the pool; a partial UNIQUE index enforces "at most one
-- builtin plan per project".
--
-- Backfill (PD-locked, forward-only): for EACH existing project create the builtin
-- pool if absent, then MOVE every existing assigned-non-terminal backlog task
-- (plan_id='' AND assignee!='' AND status NOT IN ('completed','discarded')) INTO
-- that pool, so assigned-backlog tasks keep surfacing in get_my_work (they become
-- claimable in the pool instead of vanishing). Unassigned backlog tasks stay in the
-- real backlog; terminal tasks stay put. Deterministic ids ('plan-builtin-'||id)
-- keep the migration idempotent + reproducible.

-- 1) Schema: the is_builtin flag + the one-builtin-per-project partial unique index.
ALTER TABLE pm_plans ADD COLUMN is_builtin INTEGER NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX idx_pm_plans_builtin_per_project ON pm_plans(project_id) WHERE is_builtin = 1;

-- 2) Backfill: create the builtin pool for every project that lacks one. status is
--    'running' (always-started) and is_builtin=1. created_by 'system'.
INSERT INTO pm_plans (id, project_id, name, description, status, creator_ref, conversation_id, target_date, is_builtin, created_at, updated_at, version)
SELECT 'plan-builtin-' || p.id, p.id, '[Built-in]', '', 'running', 'system', '', '', 1, p.created_at, p.created_at, 1
FROM pm_projects p
WHERE NOT EXISTS (
    SELECT 1 FROM pm_plans b WHERE b.project_id = p.id AND b.is_builtin = 1
);

-- 3) Backfill: move assigned-non-terminal backlog tasks into their project's pool.
--    Only tasks with NO plan (plan_id='') AND an assignee AND not terminal move;
--    unassigned backlog tasks and terminal tasks are left untouched.
UPDATE pm_tasks
SET plan_id = 'plan-builtin-' || project_id
WHERE plan_id = ''
  AND assignee IS NOT NULL
  AND assignee != ''
  AND status NOT IN ('completed', 'discarded');

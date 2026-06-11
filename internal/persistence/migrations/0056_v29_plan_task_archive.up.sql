-- 0056_v29_plan_task_archive.up.sql — v2.9 P3 Stage B: Plan delete + archive.
--
-- Adds the ORTHOGONAL archived state to tasks (archival does NOT change
-- task.status — a task may be archived in any status, and its status is preserved
-- through archive; mirrors Conversation's archived_at/archived_by, ADR-0032 §5):
--   archived_at — RFC3339Nano timestamp the task was archived; '' = not archived
--                 (the AR rehydrates '' → nil, IsArchived() == false).
--   archived_by — the IdentityRef (user:/agent:) that archived it; '' when not
--                 archived. Cascade-set by ArchivePlan when the parent Plan is
--                 archived.
--
-- pm_plans needs NO DDL for the new 'archived' status value: pm_plans.status is a
-- plain `TEXT NOT NULL` column with NO CHECK constraint (see 0054), so the new
-- PlanStatus 'archived' is storable as-is.
ALTER TABLE pm_tasks ADD COLUMN archived_at TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_tasks ADD COLUMN archived_by TEXT NOT NULL DEFAULT '';

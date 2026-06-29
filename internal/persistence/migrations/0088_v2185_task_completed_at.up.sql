-- 0088_v2185_task_completed_at.up.sql — T570 follow-up (plan-detail completed_at).
--
-- Add an AUTHORITATIVE completion timestamp to pm_tasks. Unlike status_changed_at
-- (which moves on EVERY status change), completed_at is set ONLY when a task enters
-- the 'completed' status and is CLEARED (NULL) on any transition out of completed
-- (e.g. reopen). It is the stable "when did this last complete" the plan task-list
-- DONE row shows, immune to later metadata edits and reset on reopen.
--
-- UPGRADE SAFETY: one additive ADD COLUMN (nullable) + a one-shot backfill. A row
-- currently in 'completed' inherits completed_at = its status_changed_at (the moment
-- it became completed — the same value the DTO emitted before this field existed);
-- every other row keeps NULL. No row is otherwise rewritten.
--
-- DIALECT NOTE: ADD COLUMN + a guarded UPDATE are in the SQLite (>=3.35) + PG common
-- subset.

ALTER TABLE pm_tasks ADD COLUMN completed_at TEXT;
UPDATE pm_tasks SET completed_at = status_changed_at WHERE status = 'completed';

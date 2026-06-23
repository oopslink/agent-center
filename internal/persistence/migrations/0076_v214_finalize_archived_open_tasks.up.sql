-- 0076_v214_finalize_archived_open_tasks.up.sql — T339 (issue-0e98c561)
--
-- Backfill the existing "archived + non-terminal" leak: an archived plan's
-- escape/skipped node task was archived ORTHOGONALLY (status preserved, see 0056),
-- leaving it archived_at != '' yet status='open' — a dead task that shows up in the
-- task board / list_tasks(open) but is LOCKED (ErrTaskArchived), so no normal
-- transition can ever finalize it (discard_task fails on the archived guard).
--
-- The root cause is fixed forward in code: ArchivePlan now finalizes a non-terminal
-- task to discarded BEFORE archiving it (Task.FinalizeForArchive). This one-time
-- DATA migration closes out the rows that already leaked — the 7 escape nodes of the
-- archived plan-e3ee9116 (T295/300/305/310/315/320/325) and any like them.
--
-- Safety: `archived_at != ''` restricts the UPDATE to read-only archived rows, so it
-- can NEVER clobber a live task. Idempotent (a re-run matches nothing once status is
-- terminal). status_changed_at is stamped to archived_at (when the task was
-- effectively concluded); blocked_reason is cleared (a discarded task is not stuck,
-- mirroring Task.FinalizeForArchive / Discard).
UPDATE pm_tasks
   SET status = 'discarded',
       status_changed_at = archived_at,
       blocked_reason = ''
 WHERE archived_at != ''
   AND status IN ('open', 'running', 'reopened');

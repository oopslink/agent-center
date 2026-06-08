-- 0052 down — best-effort inverse of the v2.8.1 state model fix.
--
-- discarded→canceled (Task) / discarded→withdrawn (Issue) is exact. The removed
-- "assigned" state cannot be perfectly reconstructed, but in the OLD model an
-- open Task WITH an assignee was "assigned" → restore those; open Tasks without
-- an assignee stay open.
UPDATE pm_issues SET status = 'withdrawn' WHERE status = 'discarded';
UPDATE pm_tasks  SET status = 'canceled'  WHERE status = 'discarded';
UPDATE pm_tasks  SET status = 'assigned'  WHERE status = 'open' AND assignee IS NOT NULL AND assignee != '';

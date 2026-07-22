-- 0114_remove_task_delivered_status.up.sql
--
-- Collapse the short-lived task lifecycle status `delivered` back into
-- `completed`. A delivered task represented work whose executor had finished, but
-- because plan readiness only advances on completed nodes it stranded ordinary
-- Dev→Review DAGs. From this migration forward completion is expressed with
-- complete_task plus optional structured delivery/review metadata.

UPDATE pm_tasks
   SET status = 'completed',
       completed_at = COALESCE(completed_at, status_changed_at, updated_at)
 WHERE status = 'delivered';

-- 0111 widened this partial index while delivered existed as an active parked
-- state. Rebuild it so migrated databases do not keep removed lifecycle
-- vocabulary in active-task lookup metadata.
DROP INDEX IF EXISTS idx_pm_tasks_assignee_status;
CREATE INDEX idx_pm_tasks_assignee_status
    ON pm_tasks (assignee, status)
    WHERE status IN ('open', 'running', 'blocked');

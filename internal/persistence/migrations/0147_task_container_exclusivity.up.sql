-- A live Task belongs to exactly one working container. Structured Plan
-- membership (pm_tasks.plan_id) is authoritative over the independent
-- AssignmentPool projection, so remove duplicates produced before this invariant.
DELETE FROM pm_assignment_pool_tasks
WHERE task_id IN (
    SELECT id FROM pm_tasks WHERE plan_id IS NOT NULL AND plan_id <> ''
);

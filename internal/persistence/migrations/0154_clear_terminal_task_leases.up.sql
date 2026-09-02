UPDATE pm_tasks
SET execution_lease_expires_at = NULL
WHERE status IN ('completed', 'failed', 'discarded')
  AND execution_lease_expires_at IS NOT NULL;

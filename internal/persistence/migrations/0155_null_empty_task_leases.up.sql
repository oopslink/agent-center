UPDATE pm_tasks
SET execution_lease_expires_at = NULL
WHERE execution_lease_expires_at = '';

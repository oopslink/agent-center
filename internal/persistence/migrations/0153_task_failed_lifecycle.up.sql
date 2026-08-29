ALTER TABLE pm_tasks ADD COLUMN failed_reason TEXT NOT NULL DEFAULT '';

UPDATE pm_tasks
SET
  failed_reason = COALESCE(NULLIF(blocked_reason, ''), 'migrated from legacy blocked task without a recorded reason'),
  status = 'failed',
  blocked_reason = NULL,
  blocked_reason_type = '',
  blocked_comment = '',
  execution_lease_expires_at = NULL,
  status_changed_at = updated_at
WHERE status = 'blocked';

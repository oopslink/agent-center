UPDATE pm_tasks
SET
  blocked_reason = failed_reason,
  blocked_reason_type = 'obstacle',
  failed_reason = '',
  status = 'blocked',
  status_changed_at = updated_at
WHERE status = 'failed';

ALTER TABLE pm_tasks DROP COLUMN failed_reason;

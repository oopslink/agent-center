DROP INDEX IF EXISTS idx_wce_worker_agent_task_status;
ALTER TABLE worker_control_events DROP COLUMN status_updated_at;
ALTER TABLE worker_control_events DROP COLUMN execution_id;
ALTER TABLE worker_control_events DROP COLUMN status_detail;
ALTER TABLE worker_control_events DROP COLUMN status_reason;
ALTER TABLE worker_control_events DROP COLUMN status;
ALTER TABLE worker_control_events DROP COLUMN task_id;
ALTER TABLE worker_control_events DROP COLUMN agent_id;

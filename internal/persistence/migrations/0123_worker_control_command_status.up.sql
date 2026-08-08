-- 0123_worker_control_command_status.up.sql — make accepted fork_executor commands observable.
ALTER TABLE worker_control_events ADD COLUMN agent_id TEXT;
ALTER TABLE worker_control_events ADD COLUMN task_id TEXT;
ALTER TABLE worker_control_events ADD COLUMN status TEXT;
ALTER TABLE worker_control_events ADD COLUMN status_reason TEXT;
ALTER TABLE worker_control_events ADD COLUMN status_detail TEXT;
ALTER TABLE worker_control_events ADD COLUMN execution_id TEXT;
ALTER TABLE worker_control_events ADD COLUMN status_updated_at TEXT;

CREATE INDEX IF NOT EXISTS idx_wce_worker_agent_task_status
    ON worker_control_events (worker_id, command_type, agent_id, task_id, status, "offset");

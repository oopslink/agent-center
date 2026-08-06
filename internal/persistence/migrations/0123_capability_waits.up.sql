CREATE TABLE IF NOT EXISTS capability_waits (
  task_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  assignee_ref TEXT NOT NULL,
  worker_id TEXT NOT NULL,
  required_cli TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  expires_at TEXT,
  redrive_count INTEGER NOT NULL DEFAULT 0,
  last_redrive_at TEXT,
  PRIMARY KEY (task_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_capability_waits_status_worker
  ON capability_waits(status, worker_id);

CREATE INDEX IF NOT EXISTS idx_capability_waits_status_expires
  ON capability_waits(status, expires_at);

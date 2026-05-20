-- 0002_taskruntime.down.sql — Phase 2 TaskRuntime Core (idempotent reverse)

DROP INDEX IF EXISTS idx_artifacts_execution;
DROP INDEX IF EXISTS idx_artifacts_task;
DROP TABLE IF EXISTS artifacts;

DROP INDEX IF EXISTS idx_input_requests_status;
DROP INDEX IF EXISTS idx_input_requests_execution;
DROP TABLE IF EXISTS input_requests;

DROP INDEX IF EXISTS idx_task_executions_dispatch;
DROP INDEX IF EXISTS idx_task_executions_active;
DROP INDEX IF EXISTS idx_task_executions_worker_status;
DROP INDEX IF EXISTS idx_task_executions_task;
DROP TABLE IF EXISTS task_executions;

DROP INDEX IF EXISTS idx_tasks_conv;
DROP INDEX IF EXISTS idx_tasks_issue;
DROP INDEX IF EXISTS idx_tasks_parent;
DROP INDEX IF EXISTS idx_tasks_project_status;
DROP TABLE IF EXISTS tasks;

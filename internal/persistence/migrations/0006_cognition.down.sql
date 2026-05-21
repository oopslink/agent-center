-- 0006_cognition.down.sql — revert Phase 6 Cognition tables.

DROP INDEX IF EXISTS idx_decisions_created;
DROP INDEX IF EXISTS idx_decisions_kind;
DROP INDEX IF EXISTS idx_decisions_invocation;
DROP TABLE IF EXISTS decision_records;

DROP INDEX IF EXISTS uniq_invocations_running_per_scope;
DROP INDEX IF EXISTS idx_invocations_started;
DROP INDEX IF EXISTS idx_invocations_status;
DROP INDEX IF EXISTS idx_invocations_scope;
DROP TABLE IF EXISTS supervisor_invocations;

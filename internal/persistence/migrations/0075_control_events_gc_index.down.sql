-- 0075_control_events_gc_index.down.sql — revert T340 worker_control_events GC index.
-- Schema-only (index drop); fully reversible. Down->Up lands on an identical schema.
DROP INDEX IF EXISTS idx_wce_created_at;

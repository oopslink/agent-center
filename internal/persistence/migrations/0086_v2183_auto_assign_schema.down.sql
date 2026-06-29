-- 0086_v2183_auto_assign_schema.down.sql — reverse of the up migration: drop the
-- auto-assign columns. The project master switch lives in the settings KV store
-- (no schema), so nothing to drop for it here — a rollback may leave stale
-- `auto_assign.enabled.*` keys, which are harmless (read only by the BE-2
-- reconciler, absent ⇒ default).
ALTER TABLE agents DROP COLUMN auto_assignable;
ALTER TABLE pm_tasks DROP COLUMN required_capabilities;

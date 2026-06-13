-- 0058_v291_builtin_plan.down.sql — v2.9.1 ADR-0047
--
-- Forward-only (PD-locked): the backfill (pool creation + moving assigned-backlog
-- tasks into the pool) has no faithful reverse — a moved task is indistinguishable
-- from one natively selected into the pool, and dropping the column would lose the
-- builtin marker. Down is a no-op for the DATA. We DROP the partial unique index
-- and the column so the SCHEMA still round-trips Down→Up on a fresh DB.
DROP INDEX IF EXISTS idx_pm_plans_builtin_per_project;
ALTER TABLE pm_plans DROP COLUMN is_builtin;

-- 0097_v228_task_recovery_reset_count.up.sql — T862 (reset_task tier-3 recovery).
--
-- Add the DURABLE circuit-breaker tally recovery_reset_count to pm_tasks. reset_task
-- returns a confirmed-dead running task to the pool (running→open, back to auto-assign)
-- so a FRESH executor picks it up; this column counts how many times that happened
-- consecutively-since-last-success (Complete zeroes it). When it reaches the cap
-- (pm.MaxRecoveryResets = 3) the service BLOCKS the task for PD triage instead of
-- resetting again — a reset loop is a symptom of a bad task / broken environment that
-- auto-recovery cannot fix. Durable so the tally survives center restarts (the recovery
-- budget must not silently refill on a bounce).
--
-- UPGRADE SAFETY: one ADDITIVE NOT NULL DEFAULT 0 column — every existing row inherits
-- 0 (a full, untouched recovery budget), no row is rewritten.
--
-- DIALECT NOTE: ADD COLUMN … NOT NULL DEFAULT <const> is in the SQLite (>=3.35) + PG
-- common subset.

ALTER TABLE pm_tasks ADD COLUMN recovery_reset_count INTEGER NOT NULL DEFAULT 0;

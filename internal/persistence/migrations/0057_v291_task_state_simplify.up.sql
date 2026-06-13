-- 0057_v291_task_state_simplify.up.sql — v2.9.1 ADR-0046
--
-- Task state machine 7→5: "blocked" and "verified" are removed.
--   * blocked → running, KEEPING blocked_reason (stuck is now a running-task
--     annotation, not a state — removes the deadlock class; see ADR-0046 §1 / T16).
--   * verified → completed ("verified" was unused; the no-self-accept discipline
--     lives in process, not a task state; ADR-0046 §2).
--
-- Data-only (no schema change). Idempotent on a fresh DB (no rows). NOTE: at merge
-- time this may renumber if a higher migration (e.g. the Thread 0057) already
-- landed on main — flagged to IntegrationDev.

UPDATE pm_tasks SET status = 'running'   WHERE status = 'blocked';
UPDATE pm_tasks SET status = 'completed' WHERE status = 'verified';

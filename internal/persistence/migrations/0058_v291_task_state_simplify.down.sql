-- 0057_v291_task_state_simplify.down.sql — v2.9.1 ADR-0046
--
-- Forward-only DATA migration: there is no faithful reverse (a migrated
-- running+blocked_reason row is indistinguishable from a natively-running one, and
-- completed←verified loses the distinction). No schema changed, so Down is a no-op;
-- a full Down→Up still round-trips the SCHEMA. (Intentional: ADR-0046 removes the
-- blocked/verified states permanently.)
SELECT 1;

-- 0108_i103_blocked_on.down.sql — reverse the I103 BlockedOn store. The table is a
-- pure-additive observational snapshot (§8, no gate reads it), so dropping it simply
-- reverts to the pre-I103 state. No data restore is needed.
DROP INDEX IF EXISTS idx_pm_plan_blocked_on_plan;
DROP TABLE IF EXISTS pm_plan_blocked_on;

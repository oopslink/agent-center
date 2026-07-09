-- 0105_agent_judge_enabled.down.sql — drop the per-agent judge opt-in column. The
-- switch defaulted to 0 (OFF) and nothing else depends on it, so dropping it simply
-- reverts to "judge never consulted" (the pre-T950 behavior). No data restore needed.
ALTER TABLE agents DROP COLUMN judge_enabled;

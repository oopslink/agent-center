-- 0036_supervisor_cut.up.sql — v2.6 BE-9
--
-- Drops the Cognition BC supervisor tables.
-- SupervisorInvocation + DecisionRecord concepts are removed in v2.6.
-- These tables were created by migrations 0006 + 0011.

DROP TABLE IF EXISTS decision_records;
DROP TABLE IF EXISTS supervisor_invocations;

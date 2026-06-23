-- 0076_v214_finalize_archived_open_tasks.down.sql — T339 (issue-0e98c561)
--
-- Forward-only DATA migration: there is no faithful reverse — a row discarded by
-- this backfill is indistinguishable from a natively-discarded archived task (the
-- pre-backfill status open/running/reopened is not recoverable). No schema changed,
-- so Down is a no-op; a full Down→Up still round-trips the SCHEMA. (Same pattern as
-- 0058's / 0071's data-only down.)
SELECT 1;

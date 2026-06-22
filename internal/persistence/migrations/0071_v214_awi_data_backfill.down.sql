-- 0071_v214_awi_data_backfill.down.sql — revert v2.14.0 I14/F4 migration B.
-- This is a DATA migration: the pm_tasks UPDATEs (block annotations, paused->open,
-- surplus-running->open) are NOT losslessly reversible — the legacy state lives in
-- agent_work_items, which still exists until F7's 0073 drop, so a re-Up faithfully
-- reproduces it. We therefore only revert the cleanly-reversible part: the
-- best-effort action-log rows this migration inserted (id LIKE 'awimig-%'). The
-- schema is unchanged by 0071, so Down->Up lands on an identical schema shape
-- (same pattern as 0058's data-only down).
DELETE FROM pm_task_action_logs WHERE id LIKE 'awimig-%';

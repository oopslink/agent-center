-- 0073_v214_drop_agent_work_items.up.sql — v2.14.0 I14/F7 (issue §七.1 / §八).
--
-- The final cut of the AgentWorkItem retirement (issue I14 "去掉 AgentWorkItem,
-- 收敛到 PM 域的 Task"). Migration ordering (PD ruling 2026-06-21): A=0070 (schema
-- add) → B=0071 (F4 data backfill) → 0072 (F3 single-active UNIQUE) → C=0073 (this:
-- drop the legacy tables, "编号 > B"). By now F1–F6 have folded every AgentWorkItem
-- responsibility into pm_tasks (block/lease/action-log columns 0070, backfilled
-- 0071) and the fleet/observability read layer has been repointed onto pm_tasks
-- (F7), so the legacy read+write tables have no remaining reader or writer.
--
-- Dropped:
--   - agent_work_items            (created 0043) — the work-queue item AR table;
--     its indexes (idx_awi_agent, idx_awi_task, and the UNIQUE idx_awi_agent_active
--     from 0051) are dropped automatically with the table.
--   - agent_work_item_projections (created 0046) — the fleet read-model table; the
--     fleet snapshot now sources executions from pm_tasks (+ pm_task_action_logs).
--
-- NOT dropped: agent_activity_events (created alongside agent_work_items in 0043)
-- is an INDEPENDENT append-only observation stream keyed by agent — it survives
-- (the agent activity feed / Worker Activity view still read it).
--
-- Note: the issue text also named `agent_work_item_transitions`; no such table was
-- ever created in this repo (transitions were carried on the outbox event stream,
-- not a dedicated table), so there is nothing to drop for it.

DROP TABLE IF EXISTS agent_work_item_projections;
DROP TABLE IF EXISTS agent_work_items;

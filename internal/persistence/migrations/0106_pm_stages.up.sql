-- 0106_pm_stages.up.sql — Plan Stage model (design 2026-07-03-plan-stage-model,
-- §4.1). A Plan may (optionally) be organized into Stages: each Stage is a sub-DAG
-- of the plan's nodes with a barrier + an optional gate (acceptance). Stage is a
-- LIGHTWEIGHT first-class aggregate — a `pm_stages` row that is addressable/queryable
-- — but it drives NO execution and stores NO independent state machine: its status is
-- a PROJECTION of its member nodes (§4.1) and execution is fully delegated to the
-- orchestration graph engine (§4.2, "不另起引擎").
--
--   - depends_on_stages: JSON array of upstream StageIDs (the outer stage DAG's edges).
--   - gate_node_id:      the stage's exit gate (a graph CONDITION node), "" when none.
--   - max_rounds:        the stage-local bounded-retry cap (gate reject re-runs, §5).
--
-- pm_tasks.stage_id is the node→stage membership (§4.1 "每个 task/graph 节点带
-- stage_id"), recorded at AUTHORING time; buildPlanGraph propagates it onto the graph
-- nodes (metadata.stage_id) when the plan starts.
--
-- UPGRADE SAFETY (§8 向后兼容): a brand-new EMPTY table + a purely additive ADD COLUMN
-- with TEXT NOT NULL DEFAULT '' (every existing task backfills to "" = no stage). An
-- existing plan with no `pm_stages` rows and all-empty stage_id is byte-identical to
-- today's pure-node DAG = 零回归. CREATE TABLE / ADD COLUMN / DROP COLUMN are in the
-- SQLite (>=3.35) + PG common subset, mirroring 0091 (pm_graphs) and 0092 (task node_id).

CREATE TABLE IF NOT EXISTS pm_stages (
    id                TEXT PRIMARY KEY,
    plan_id           TEXT NOT NULL,
    name              TEXT NOT NULL,
    depends_on_stages TEXT NOT NULL DEFAULT '[]',
    gate_node_id      TEXT NOT NULL DEFAULT '',
    max_rounds        INTEGER NOT NULL DEFAULT 0,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    version           INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_pm_stages_plan_id ON pm_stages(plan_id);

ALTER TABLE pm_tasks ADD COLUMN stage_id TEXT NOT NULL DEFAULT '';

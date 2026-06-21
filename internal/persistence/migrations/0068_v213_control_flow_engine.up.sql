-- 0068_v213_control_flow_engine.up.sql — v2.13.0 I18/B1: control-flow engine
-- (docs/design/v2.13.0/control-flow-engine-spec.md). Extends the plan DAG with
-- decision/conditional/loopback control flow. ALL additive: existing edges default
-- to kind='seq' / when='' / max_rounds=0 == the pre-B1 hard AND dependency, and the
-- two new tables start empty, so existing plans are byte-for-byte unchanged.
--
--   pm_task_dependencies.kind       — 'seq'(default) | 'conditional' | 'loopback'.
--   pm_task_dependencies."when"     — outcome label routing conditional/loopback edges
--                                     ("when" is a SQL keyword → quoted).
--   pm_task_dependencies.max_rounds — loopback round cap (>=1); 0 for non-loopback.
ALTER TABLE pm_task_dependencies ADD COLUMN kind TEXT NOT NULL DEFAULT 'seq';
ALTER TABLE pm_task_dependencies ADD COLUMN "when" TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_task_dependencies ADD COLUMN max_rounds INTEGER NOT NULL DEFAULT 0;

-- Decision outcomes (§2.3): a decision node's recorded outcome (latest-wins per
-- plan_id,task_id) routing its conditional/loopback out-edges. Orchestrator-owned
-- stored state, like dispatch records.
CREATE TABLE pm_plan_decision_outcomes (
    plan_id    TEXT NOT NULL,
    task_id    TEXT NOT NULL,
    outcome    TEXT NOT NULL,
    decided_at TEXT NOT NULL,
    PRIMARY KEY (plan_id, task_id)
);
CREATE INDEX idx_pm_plan_decision_outcomes_plan ON pm_plan_decision_outcomes (plan_id);

-- Loop rounds (§4): the completed-round count per loopback edge, for the max-rounds
-- exit guard. Bounded by the edge's max_rounds; the orchestrator increments on each
-- reject re-activation.
CREATE TABLE pm_plan_loop_rounds (
    plan_id      TEXT NOT NULL,
    from_task_id TEXT NOT NULL,
    to_task_id   TEXT NOT NULL,
    round        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (plan_id, from_task_id, to_task_id)
);
CREATE INDEX idx_pm_plan_loop_rounds_plan ON pm_plan_loop_rounds (plan_id);

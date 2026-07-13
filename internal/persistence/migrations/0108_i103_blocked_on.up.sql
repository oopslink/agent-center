-- 0108_i103_blocked_on.up.sql — I103 §1 BlockedOn 描述符 (deterministic-resume).
--
-- A BlockedOn is a旁路 OBSERVATIONAL snapshot answering "why is this plan node NOT
-- making terminal progress right now" — materialized by the reconcile sweep for every
-- non-terminal plan node and cleared when the node enters ready/running/terminal. It
-- is PURE OBSERVATION: it changes NO gating / readiness semantics (the acceptance hard
-- gate, reject-gate, stage barrier all stay authoritative) — downstream I103 tasks
-- (deadline engine, on_timeout routing, human_decision queue, external_event
-- subscription, executor_liveness detection) READ this snapshot; this migration only
-- provisions the store.
--
-- SINGLE-SLOT latest-wins per (plan_id, task_id): a node maps 1:1 to a task and to a
-- graph node, so one row per node. The reconcile materialize refreshes the row in
-- place (preserving waited_since while the wait_type is unchanged, and the downstream-
-- owned probe fields), so a re-run of the idempotent sweep produces no churn.
--
-- ADDITIVE / zero-regression: the table starts empty; nothing reads it as a gate, so
-- existing plans behave exactly as before.
CREATE TABLE pm_plan_blocked_on (
    plan_id           TEXT NOT NULL,
    task_id           TEXT NOT NULL,
    node_id           TEXT NOT NULL DEFAULT '',  -- bound orchestration graph node id
    wait_type         TEXT NOT NULL,             -- upstream_completion|acceptance_verdict|stage_barrier|human_decision|external_event|executor_liveness|timeout_only
    wait_keys         TEXT NOT NULL DEFAULT '',  -- JSON array of the ids being waited on
    trigger_condition TEXT NOT NULL DEFAULT '',  -- human-readable release condition
    waited_since      TEXT NOT NULL,             -- when THIS wait began (reset on wait_type change)
    deadline          TEXT NOT NULL DEFAULT '',  -- optional; the deadline engine (downstream) sets it
    on_timeout        TEXT NOT NULL DEFAULT '',  -- optional routing hint (downstream)
    last_probe_at     TEXT NOT NULL DEFAULT '',  -- downstream prober owns this
    probe_count       INTEGER NOT NULL DEFAULT 0,-- downstream prober owns this
    PRIMARY KEY (plan_id, task_id)               -- single-slot, latest-wins
);
CREATE INDEX idx_pm_plan_blocked_on_plan ON pm_plan_blocked_on (plan_id);

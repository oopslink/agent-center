-- 0054_v29_plan_orchestration.up.sql — v2.9 Plan Orchestration FOUNDATION (#283).
--
-- Adds the Plan aggregate, the Task↔Plan membership column, and the per-Plan
-- depends_on execution-DAG edge table.
--
-- §9.2 RED-LINE: node status is DERIVED by the orchestrator, NEVER stored. There
-- is intentionally NO node_status / node_state column anywhere below. Node status
-- = f(task.status, upstream-all-done?, dispatch-record) computed at read time
-- (#285); the dispatch record (the only orchestrator-owned state) lands later too.
--
-- §9.8 RED-LINE: the DAG is 1:1-scoped to one Plan. Every edge carries plan_id and
-- the PK includes it, so two plans' edges are isolated.

CREATE TABLE pm_plans (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL,
    creator_ref     TEXT NOT NULL,
    conversation_id TEXT NOT NULL DEFAULT '',  -- '' until #284 wires the 1:1 conversation
    target_date     TEXT NOT NULL DEFAULT '',  -- '' = no target date (RFC3339 otherwise)
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    version         INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_pm_plans_project ON pm_plans (project_id);

-- Task ↔ Plan membership (0..1). '' = in the backlog / no plan.
ALTER TABLE pm_tasks ADD COLUMN plan_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_pm_tasks_plan ON pm_tasks (plan_id);

-- depends_on edges: from_task_id depends_on to_task_id, scoped to one plan (§9.8).
CREATE TABLE pm_task_dependencies (
    plan_id      TEXT NOT NULL,
    from_task_id TEXT NOT NULL,
    to_task_id   TEXT NOT NULL,
    PRIMARY KEY (plan_id, from_task_id, to_task_id)
);
CREATE INDEX idx_pm_task_dependencies_plan ON pm_task_dependencies (plan_id);

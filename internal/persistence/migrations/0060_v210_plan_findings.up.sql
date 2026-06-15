-- 0060_v210_plan_findings.up.sql — v2.10 Plan Shared Findings (ADR-0053).
--
-- DeLM "shared verified context" minimal slice: a plan-scoped, immutable knowledge
-- gist an agent records back to its Plan, bound to the SOURCE task that produced it
-- and attributed to its author. Downstream/sibling agents read findings (injected
-- into dispatch, or via list_findings) to build on prior progress.
--
-- §9.w: NO foreign keys — plan_id/task_id/project_id are reference columns whose
-- integrity the pm Repository/AppService enforces. §9.0: TEXT ULID PK, TEXT
-- ISO8601 created_at, enum (kind) as TEXT. Findings are IMMUTABLE (no updated_at;
-- version fixed at 1). content stays COMPACT (≤ MaxFindingContentLen, app-enforced)
-- so it always fits the TEXT column (never BlobStore, §8).

CREATE TABLE pm_plan_findings (
    id          TEXT PRIMARY KEY,
    plan_id     TEXT NOT NULL,
    task_id     TEXT NOT NULL,            -- the SOURCE task that produced the finding
    project_id  TEXT NOT NULL,            -- denormalized from the plan (org-scoping + ListByProject)
    author_ref  TEXT NOT NULL,            -- kind-prefixed identity ref (agent:/user:/system)
    kind        TEXT NOT NULL,            -- fact | failure | constraint | patch_summary
    content     TEXT NOT NULL,            -- the compact gist
    created_at  TEXT NOT NULL,            -- ISO8601 UTC
    version     INTEGER NOT NULL DEFAULT 1
);

-- plan-scoped read (dispatch injection + list_findings) is the hot path.
CREATE INDEX idx_pm_plan_findings_plan ON pm_plan_findings (plan_id);
-- task-scoped read (findings produced by one task).
CREATE INDEX idx_pm_plan_findings_task ON pm_plan_findings (task_id);

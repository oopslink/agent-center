-- ADR-0055: AssignmentPool is a project-scoped pull queue, not a resident Plan.
CREATE TABLE IF NOT EXISTS pm_assignment_pools (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL UNIQUE,
    scheduling_class    TEXT NOT NULL DEFAULT 'background',
    auto_assign_enabled INTEGER NOT NULL DEFAULT 1,
    holding_cap         INTEGER NOT NULL DEFAULT 3,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    version             INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS pm_assignment_pool_tasks (
    pool_id          TEXT NOT NULL,
    task_id          TEXT NOT NULL UNIQUE,
    priority         INTEGER NOT NULL DEFAULT 0,
    added_by         TEXT NOT NULL DEFAULT 'system',
    added_at         TEXT NOT NULL,
    claimed_by       TEXT NOT NULL DEFAULT '',
    claimed_at       TEXT NOT NULL DEFAULT '',
    claim_expires_at TEXT NOT NULL DEFAULT '',
    version          INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (pool_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_pm_assignment_pool_tasks_claim
    ON pm_assignment_pool_tasks(pool_id, claimed_by, priority DESC, added_at, task_id);

-- Expand/backfill only: keep legacy builtin rows during the rollback window, but
-- make the new membership table the data source available to the new binary.
INSERT INTO pm_assignment_pools
    (id, project_id, scheduling_class, auto_assign_enabled, holding_cap,
     created_at, updated_at, version)
SELECT 'pool-' || p.id, p.id, 'background', 1, 3, p.created_at, p.updated_at, 1
FROM pm_projects p
WHERE NOT EXISTS (
    SELECT 1 FROM pm_assignment_pools ap WHERE ap.project_id = p.id
);

INSERT INTO pm_assignment_pool_tasks
    (pool_id, task_id, priority, added_by, added_at, claimed_by, claimed_at,
     claim_expires_at, version)
SELECT ap.id, t.id, 0, 'system', t.created_at,
       CASE WHEN t.assignee IS NULL THEN '' ELSE t.assignee END,
       CASE WHEN t.assignee IS NULL OR t.assignee = '' THEN '' ELSE t.updated_at END,
       '', 1
FROM pm_tasks t
JOIN pm_plans legacy ON legacy.id = t.plan_id AND legacy.is_builtin = 1
JOIN pm_assignment_pools ap ON ap.project_id = t.project_id
WHERE NOT EXISTS (
    SELECT 1 FROM pm_assignment_pool_tasks apt WHERE apt.task_id = t.id
);

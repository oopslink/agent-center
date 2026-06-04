-- 0049_v271_pm_org_sequence.up.sql — v2.7.1 #245 (@oopslink)
--
-- Org-internal sequence numbers for issues + tasks (T<n> / I<n>) so humans can
-- reference them in conversation. The hash id (issue-xxx / task-xxx) stays the
-- canonical/stable internal ref (URLs + API keep using it); org_number is a
-- display + reference token, unique + monotonic PER ORGANIZATION PER TYPE.
--
-- pm_issues/pm_tasks have no org_id of their own (org lives on pm_projects), so
-- the per-org grouping joins through the owning project.
--
-- UPGRADE-safe (v2.7.0/earlier → v2.7.1): existing rows are backfilled below
-- (deterministic order per org), and the counter watermark is seeded to the max
-- assigned so new allocations continue without collision.
ALTER TABLE pm_issues ADD COLUMN org_number INTEGER;
ALTER TABLE pm_tasks  ADD COLUMN org_number INTEGER;

-- Per-(org, type) counter. next_value = the most-recently-allocated number
-- (high-water mark). Allocation is an atomic UPSERT ... RETURNING (see
-- OrgSequenceRepo.Allocate): a missing row inserts next_value=1 (allocates 1);
-- an existing row does next_value=next_value+1 RETURNING the new value. SQLite's
-- per-row write lock serializes concurrent allocations → no collision, no skip.
CREATE TABLE pm_org_sequence (
    organization_id TEXT    NOT NULL,
    entity_type     TEXT    NOT NULL,   -- 'issue' | 'task'
    next_value      INTEGER NOT NULL,
    PRIMARY KEY (organization_id, entity_type)
);

-- Backfill existing issues per org, deterministic order (created_at, then id as
-- a stable tiebreak), assigning 1..N.
WITH numbered AS (
    SELECT i.id AS row_id,
           ROW_NUMBER() OVER (PARTITION BY p.organization_id ORDER BY i.created_at, i.id) AS n
    FROM pm_issues i
    JOIN pm_projects p ON i.project_id = p.id
)
UPDATE pm_issues
SET org_number = (SELECT n FROM numbered WHERE numbered.row_id = pm_issues.id)
WHERE id IN (SELECT row_id FROM numbered);

WITH numbered AS (
    SELECT t.id AS row_id,
           ROW_NUMBER() OVER (PARTITION BY p.organization_id ORDER BY t.created_at, t.id) AS n
    FROM pm_tasks t
    JOIN pm_projects p ON t.project_id = p.id
)
UPDATE pm_tasks
SET org_number = (SELECT n FROM numbered WHERE numbered.row_id = pm_tasks.id)
WHERE id IN (SELECT row_id FROM numbered);

-- Seed the watermark = max assigned per (org, type). Only orgs that already have
-- issues/tasks get a seed row; a fresh (org, type) is created lazily by the first
-- Allocate (insert next_value=1). next_value is the LAST allocated number, so the
-- next allocation continues at max+1.
INSERT INTO pm_org_sequence (organization_id, entity_type, next_value)
SELECT p.organization_id, 'issue', MAX(i.org_number)
FROM pm_issues i
JOIN pm_projects p ON i.project_id = p.id
WHERE i.org_number IS NOT NULL
GROUP BY p.organization_id;

INSERT INTO pm_org_sequence (organization_id, entity_type, next_value)
SELECT p.organization_id, 'task', MAX(t.org_number)
FROM pm_tasks t
JOIN pm_projects p ON t.project_id = p.id
WHERE t.org_number IS NOT NULL
GROUP BY p.organization_id;

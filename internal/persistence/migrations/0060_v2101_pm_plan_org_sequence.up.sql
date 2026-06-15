-- 0060_v2101_pm_plan_org_sequence.up.sql — v2.10.1 [T99] (owner 2026-06-15)
--
-- Org-internal sequence numbers for PLANS (P<n>), mirroring v2.7.1 #245's
-- T<n>/I<n> for tasks/issues. The plan hash id (plan-xxx) stays the canonical/
-- stable internal ref (URLs + API keep using it); org_number is a display +
-- reference token, unique + monotonic PER ORGANIZATION, on its OWN 'plan'
-- entity_type counter in pm_org_sequence — INDEPENDENT of the task/issue
-- sequences (a fresh, separate P-series, per the T99 spec).
--
-- pm_plans has no org_id of its own (org lives on pm_projects), so the per-org
-- grouping joins through the owning project. The builtin assignment-pool plan
-- (is_builtin=1, ADR-0047) is NOT a user plan (it is hidden from the global Plan
-- list), so it is EXCLUDED from the sequence — user plans keep clean P numbers.
--
-- UPGRADE-safe: existing user plans are backfilled below (deterministic order
-- per org), and the counter watermark is seeded to the max assigned so new
-- allocations continue without collision.
ALTER TABLE pm_plans ADD COLUMN org_number INTEGER;

-- Backfill existing USER plans per org, deterministic order (created_at, then id
-- as a stable tiebreak), assigning 1..N. Builtin pools are skipped.
WITH numbered AS (
    SELECT pl.id AS row_id,
           ROW_NUMBER() OVER (PARTITION BY pr.organization_id ORDER BY pl.created_at, pl.id) AS n
    FROM pm_plans pl
    JOIN pm_projects pr ON pl.project_id = pr.id
    WHERE pl.is_builtin = 0
)
UPDATE pm_plans
SET org_number = (SELECT n FROM numbered WHERE numbered.row_id = pm_plans.id)
WHERE id IN (SELECT row_id FROM numbered);

-- Seed the watermark = max assigned per org (entity_type 'plan'). Only orgs that
-- already have user plans get a seed row; a fresh (org, 'plan') is created lazily
-- by the first Allocate (insert next_value=1). next_value is the LAST allocated
-- number, so the next allocation continues at max+1.
INSERT INTO pm_org_sequence (organization_id, entity_type, next_value)
SELECT pr.organization_id, 'plan', MAX(pl.org_number)
FROM pm_plans pl
JOIN pm_projects pr ON pl.project_id = pr.id
WHERE pl.org_number IS NOT NULL
GROUP BY pr.organization_id;

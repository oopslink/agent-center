import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useRelatedPlans } from '@/api/plans';
import { PlanStatusChip } from '@/components/planDisplay';
import { refLabel } from '@/components/workItemDisplay';

// RelatedPlansBlock — the plan detail rail's "Related Plans" section (T581). It is
// the plan-side mirror of the issue sidebar's DerivedTasksBlock: a list of the OTHER
// plans derived from the SAME source issue as this plan, so you can hop between the
// plans an issue spawned. Each row: the P-number ref + plan name + status chip,
// linking to that plan's detail page.
//
// Data: useRelatedPlans (GET …/plans/{id}/related-plans) — the backend resolves the
// plan's source issue (the shared derived_from_issue of its cycle nodes, set by
// scaffold_cycle_plan) and returns the OTHER plans whose tasks derive from that issue,
// already EXCLUDING this plan and the built-in pool. The component renders the rows
// as-is (no client-side filtering).
//
// Rendered as a bordered rail SECTION (matching the rail's Up-next / Participants
// blocks) rather than a standalone card, so it sits flush inside PlanInfoRail.
export function RelatedPlansBlock({
  projectId,
  currentPlanId,
}: {
  projectId: string;
  currentPlanId: string;
}): React.ReactElement {
  const plans = useRelatedPlans(projectId, currentPlanId);
  const related = plans.data ?? [];

  return (
    <div className="border-b border-border-base p-5" data-testid="plan-related-plans">
      <h3 className="mb-3 text-[0.625rem] font-semibold uppercase tracking-wide text-text-muted">
        Related Plans
      </h3>
      {plans.isLoading ? (
        <p className="text-xs text-text-muted" data-testid="related-plans-loading">
          Loading…
        </p>
      ) : plans.isError ? (
        <p className="text-xs text-danger" data-testid="related-plans-error">
          {(plans.error as Error).message}
        </p>
      ) : related.length === 0 ? (
        <p className="text-xs text-text-muted" data-testid="related-plans-empty">
          No other plans from this issue.
        </p>
      ) : (
        <ul className="space-y-1" data-testid="related-plans-list">
          {related.map((pl) => (
            <li key={pl.id}>
              <OrgLink
                to={`/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(pl.id)}`}
                className="flex items-center gap-2 rounded px-1.5 py-1 hover:bg-bg-subtle"
                data-testid="related-plan-item"
                data-plan-id={pl.id}
                title={pl.name}
              >
                <span className="shrink-0 font-mono text-xs text-accent">{refLabel(pl.org_ref, pl.id)}</span>
                <span className="min-w-0 flex-1 truncate text-text-primary">{pl.name}</span>
                <span className="shrink-0">
                  <PlanStatusChip status={pl.status} />
                </span>
              </OrgLink>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

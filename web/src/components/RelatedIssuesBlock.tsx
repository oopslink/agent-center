import type React from 'react';
import { useTranslation } from 'react-i18next';
import { OrgLink } from '@/OrgContext';
import { useRelatedIssues } from '@/api/plans';
import { StatusChip, refLabel } from '@/components/workItemDisplay';

// RelatedIssuesBlock — the plan detail rail's "Related Issues" section. It is the
// issue-side mirror of the issue sidebar's DerivedTasksBlock: a list of the source
// issue(s) this plan's tasks derive from, so you can hop from a plan back to the
// issue(s) that spawned it. Each row: the I-number ref + issue title + status chip,
// linking to that issue's detail page.
//
// Data: useRelatedIssues (GET …/plans/{id}/related-issues) — the backend resolves the
// DISTINCT non-empty derived_from_issue values of the plan's tasks (a cycle plan has
// one; a hand-built plan may span several). The component renders the rows as-is.
//
// Rendered as a bordered rail SECTION (matching the rail's Up-next / Participants
// blocks) rather than a standalone card, so it sits flush inside PlanInfoRail.
export function RelatedIssuesBlock({
  projectId,
  currentPlanId,
}: {
  projectId: string;
  currentPlanId: string;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const issues = useRelatedIssues(projectId, currentPlanId);
  const related = issues.data ?? [];

  return (
    <div className="border-b border-border-base p-5" data-testid="plan-related-issues">
      <h3 className="mb-3 text-[0.625rem] font-semibold uppercase tracking-wide text-text-muted">
        {t('issue.relatedIssues.heading')}
      </h3>
      {issues.isLoading ? (
        <p className="text-xs text-text-muted" data-testid="related-issues-loading">
          {t('issue.relatedIssues.loading')}
        </p>
      ) : issues.isError ? (
        <p className="text-xs text-danger" data-testid="related-issues-error">
          {(issues.error as Error).message}
        </p>
      ) : related.length === 0 ? (
        <p className="text-xs text-text-muted" data-testid="related-issues-empty">
          {t('issue.relatedIssues.empty')}
        </p>
      ) : (
        <ul className="space-y-1" data-testid="related-issues-list">
          {related.map((iss) => (
            <li key={iss.id}>
              <OrgLink
                to={`/projects/${encodeURIComponent(projectId)}/issues/${encodeURIComponent(iss.id)}`}
                className="flex items-center gap-2 rounded px-1.5 py-1 hover:bg-bg-subtle"
                data-testid="related-issue-item"
                data-issue-id={iss.id}
                title={iss.title}
              >
                <span className="shrink-0 font-mono text-xs text-accent">{refLabel(iss.org_ref, iss.id)}</span>
                <span className="min-w-0 flex-1 truncate text-text-primary">{iss.title}</span>
                <span className="shrink-0">
                  <StatusChip status={iss.status} />
                </span>
              </OrgLink>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

import type React from 'react';
import { useTranslation } from 'react-i18next';
import { OrgLink } from '@/OrgContext';
import type { Issue } from '@/api/types';
import { useTasksOfIssue } from '@/api/tasks';
import { useRelatedPlansForIssue } from '@/api/plans';
import { PlanStatusChip } from '@/components/planDisplay';
import { StatusBlock } from '@/components/IssueTaskSidebar';
import { StatusChip, refLabel } from '@/components/workItemDisplay';
import { taskDetailPath } from '@/components/TaskTitleLink';
import { tagColorFor } from '@/components/tagColors';
import { formatLocalTime, formatStatusDuration } from '@/utils/time';

// IssueDetailSidebar — the @oopslink mockup's TWO-SECTION Issue detail sidebar,
// a visual mirror of TaskDetailSidebar (symmetric: TaskDetail→TaskDetailSidebar,
// IssueDetail→IssueDetailSidebar). It is built from the SAME shared primitives
// (StatusBlock, EntityRef, refLabel, tagColorFor, formatStatusDuration/
// formatLocalTime) so the two sidebars stay pixel-aligned with zero risk to the
// Task side. Issues have NO assignee, so that section is omitted; otherwise the
// labels / order / dividers / spacing / typography are identical to Task.
//
// v2.8.1 sidebar-align: editing is modal-only — the SOLE edit entry is the
// Edit-Issue pencil button (→ IssueEditModal, the single atomic PATCH of
// title/description/status/tags). The top section is a READ-ONLY display of
// status (+ in-status duration) and tags (hashed chips); no inline status menu,
// no "+ Add" tag input.
//
//   TOP (display)    : Edit-Issue button · STATUS (+ in-status duration) ·
//                      TAGS (hashed chips)        ← NO assignee (Issues have none)
//   BOTTOM (read-only): PROJECT · ISSUE ID (handle pill) · CREATED (local time)

// PencilIcon — Edit affordance (SVG, NOT emoji, per the no-emoji-icon rule).
// Identical glyph/size to TaskDetailSidebar's pencil for visual parity.
function PencilIcon(): React.ReactElement {
  return (
    <svg
      viewBox="0 0 16 16"
      className="h-3.5 w-3.5"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M11.5 2.5l2 2L6 12l-2.5.5.5-2.5z" />
    </svg>
  );
}

interface Props {
  issue: Issue;
  projectName?: string;
  /** opens the existing IssueEditModal — the single edit path. */
  onEdit: () => void;
  /** whether the issue is editable (non-terminal). Terminal → no Edit button. */
  editable: boolean;
}

export function IssueDetailSidebar({
  issue,
  projectName,
  onEdit,
  editable,
}: Props): React.ReactElement {
  const { t } = useTranslation('work');
  const iss = issue;
  const tags = iss.tags ?? [];
  const duration = formatStatusDuration(iss.status_changed_at);

  return (
    <aside
      className="space-y-4 rounded border border-border-base bg-bg-elevated p-3 text-sm"
      data-testid="issue-detail-sidebar"
      aria-label={t('issue.sidebar.aria')}
    >
      {/* ───────── TOP: display (edit only via the modal) ───────── */}
      <section className="space-y-3" data-testid="issue-sidebar-editable">
        <div className="flex items-start justify-between gap-2">
          <p className="text-xs font-semibold uppercase tracking-wide text-text-muted">{t('issue.sidebar.details')}</p>
          {editable && (
            <button
              type="button"
              onClick={onEdit}
              className="inline-flex items-center gap-1 rounded bg-bg-subtle px-2 py-1 text-xs font-medium text-text-primary hover:bg-border-base"
              data-testid="issue-edit-button"
              aria-label={t('issue.sidebar.editAria')}
            >
              <PencilIcon />
              {t('issue.sidebar.editButton')}
            </button>
          )}
        </div>

        {/* STATUS + in-status duration — display only */}
        <div data-testid="issue-sidebar-status">
          <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">{t('issue.sidebar.status')}</p>
          <div className="flex flex-wrap items-center gap-2">
            <StatusBlock status={iss.status} />
            {duration && (
              <span
                className="text-xs text-text-muted"
                data-testid="issue-status-duration"
                aria-label={t('issue.sidebar.statusDuration', { duration })}
                title={t('issue.sidebar.statusDuration', { duration })}
              >
                {duration}
              </span>
            )}
          </div>
        </div>

        {/* TAGS — hashed (both-mode-AA) chips, display only */}
        <div data-testid="issue-sidebar-tags">
          <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">{t('issue.sidebar.tags')}</p>
          <div className="flex flex-wrap items-center gap-1.5">
            {tags.map((tag) => {
              const c = tagColorFor(tag);
              return (
                <span
                  key={tag}
                  data-testid="issue-tag-chip"
                  data-tag={tag}
                  className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${c.bg} ${c.text}`}
                >
                  {tag}
                </span>
              );
            })}
            {tags.length === 0 && (
              <span className="text-xs text-text-muted" data-testid="issue-tags-empty">
                {t('issue.sidebar.noTags')}
              </span>
            )}
          </div>
        </div>
      </section>

      <hr className="border-border-base" />

      {/* ───────── BOTTOM: read-only ───────── */}
      <section className="space-y-3" data-testid="issue-sidebar-readonly">
        {iss.project_id && (
          <div>
            <p className="mb-0.5 text-xs uppercase tracking-wide text-text-muted">{t('issue.sidebar.project')}</p>
            <OrgLink
              to={`/projects/${encodeURIComponent(iss.project_id)}`}
              className="text-accent hover:underline"
              data-testid="issue-project-link"
              title={iss.project_id}
            >
              {projectName || iss.project_id}
            </OrgLink>
          </div>
        )}

        <div>
          <p className="mb-0.5 text-xs uppercase tracking-wide text-text-muted">{t('issue.sidebar.issueId')}</p>
          {/* #192 chrome rule: id-as-content → a clean handle pill (tail), full id on hover. */}
          <span
            className="inline-block rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-xs text-text-secondary"
            data-testid="issue-id-pill"
            title={iss.id}
          >
            {refLabel(iss.org_ref, iss.id)}
          </span>
        </div>

        <div>
          <p className="mb-0.5 text-xs uppercase tracking-wide text-text-muted">{t('issue.sidebar.created')}</p>
          <span className="text-text-secondary" data-testid="issue-created">
            {formatLocalTime(iss.created_at)}
          </span>
        </div>
      </section>
    </aside>
  );
}

// DerivedTasksBlock — the issue's DERIVED tasks (tasks created with
// derived_from_issue == this issue), rendered as its own card below the
// IssueDetailSidebar. Each row: org_ref (T-number) + title + status chip,
// linking to the task detail page (T133-style name-as-link). Read-only; an
// empty list shows a placeholder. Kept SEPARATE from the pure prop-driven
// IssueDetailSidebar so the sidebar stays data-free (and its tests untouched).
export function DerivedTasksBlock({
  projectId,
  issueId,
}: {
  projectId: string;
  issueId: string;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const tasks = useTasksOfIssue(projectId, issueId);
  return (
    <aside
      className="mt-4 space-y-2 rounded border border-border-base bg-bg-elevated p-3 text-sm"
      data-testid="issue-derived-tasks"
      aria-label={t('issue.derivedTasks.aria')}
    >
      <p className="text-xs font-semibold uppercase tracking-wide text-text-muted">{t('issue.derivedTasks.heading')}</p>
      {tasks.isLoading ? (
        <p className="text-xs text-text-muted" data-testid="derived-tasks-loading">
          {t('issue.derivedTasks.loading')}
        </p>
      ) : tasks.isError ? (
        <p className="text-xs text-danger" data-testid="derived-tasks-error">
          {(tasks.error as Error).message}
        </p>
      ) : tasks.data.length === 0 ? (
        <p className="text-xs text-text-muted" data-testid="derived-tasks-empty">
          {t('issue.derivedTasks.empty')}
        </p>
      ) : (
        <ul className="space-y-1" data-testid="derived-tasks-list">
          {tasks.data.map((t) => (
            <li key={t.id}>
              <OrgLink
                to={taskDetailPath(t.project_id, t.id)}
                className="flex items-center gap-2 rounded px-1.5 py-1 hover:bg-bg-subtle"
                data-testid="derived-task-item"
                data-task-id={t.id}
                title={t.title}
              >
                <span className="shrink-0 font-mono text-xs text-accent">{refLabel(t.org_ref, t.id)}</span>
                <span className="min-w-0 flex-1 truncate text-text-primary">{t.title}</span>
                <span className="shrink-0">
                  <StatusChip status={t.status} />
                </span>
              </OrgLink>
            </li>
          ))}
        </ul>
      )}
    </aside>
  );
}

// IssueRelatedPlansBlock — the plans DERIVED from this issue (the distinct non-builtin
// plans whose tasks carry derived_from_issue == this issue), rendered as its own card
// below DerivedTasksBlock. It is the plan-DIMENSION mirror of Derived Tasks (which lists
// the task dimension) and the reverse of the plan rail's Related Issues. Each row:
// org_ref (P-number) + name + status chip, linking to the plan detail page. Read-only;
// an empty list shows a placeholder. Self-fetching (useRelatedPlansForIssue).
export function IssueRelatedPlansBlock({
  projectId,
  issueId,
}: {
  projectId: string;
  issueId: string;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const plans = useRelatedPlansForIssue(projectId, issueId);
  const related = plans.data ?? [];
  return (
    <aside
      className="mt-4 space-y-2 rounded border border-border-base bg-bg-elevated p-3 text-sm"
      data-testid="issue-related-plans"
      aria-label={t('issue.relatedPlans.aria')}
    >
      <p className="text-xs font-semibold uppercase tracking-wide text-text-muted">{t('issue.relatedPlans.heading')}</p>
      {plans.isLoading ? (
        <p className="text-xs text-text-muted" data-testid="related-plans-loading">
          {t('issue.relatedPlans.loading')}
        </p>
      ) : plans.isError ? (
        <p className="text-xs text-danger" data-testid="related-plans-error">
          {(plans.error as Error).message}
        </p>
      ) : related.length === 0 ? (
        <p className="text-xs text-text-muted" data-testid="related-plans-empty">
          {t('issue.relatedPlans.empty')}
        </p>
      ) : (
        <ul className="space-y-1" data-testid="related-plans-list">
          {related.map((pl) => (
            <li key={pl.id}>
              <OrgLink
                to={`/projects/${encodeURIComponent(pl.project_id)}/plans/${encodeURIComponent(pl.id)}`}
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
    </aside>
  );
}

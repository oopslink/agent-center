import type React from 'react';
import { useTranslation } from 'react-i18next';
import { OrgLink } from '@/OrgContext';
import type { Task } from '@/api/types';
import { Avatar } from '@/components/Avatar';
import { EntityRef } from '@/components/EntityRef';
import { useSenderSidebar } from '@/components/SenderSidebarContext';
import { StatusBlock } from '@/components/IssueTaskSidebar';
import { refLabel, IssueRefTag } from '@/components/workItemDisplay';
import { PlanRefTag } from '@/components/planDisplay';
import { tagColorFor } from '@/components/tagColors';
import { formatLocalTime, formatStatusDuration } from '@/utils/time';
import { ObjectAuditTimeline } from '@/components/ObjectAuditTimeline';

// TaskDetailSidebar — the @oopslink mockup's TWO-SECTION Task detail sidebar.
// It is TaskDetail-specific (the editable/read-only split + the live status
// duration + tag chips are not shared with IssueDetail), so it is built from
// the shared primitives (StatusBlock, Avatar, EntityRef, refLabel) rather than
// bending the shared IssueTaskSidebar — IssueDetail keeps using IssueTaskSidebar
// untouched.
//
// v2.8.1 #281 readonly: per @oopslink, a task may ONLY be edited via the Edit
// Task modal. The top section is now a READ-ONLY DISPLAY of status (+ in-status
// duration), assignee (avatar + name) and tags (hashed chips) — the ONLY edit
// entry is the Edit Task button (→ TaskEditModal, the single atomic edit path).
// No inline status-change menu, no assignee Change/Unassign, no "+ Add" tag input.
//
//   TOP (display)    : Edit-Task button · STATUS (+ in-status duration) ·
//                      ASSIGNEE (avatar + name) · TAGS (hashed chips)
//   BOTTOM (read-only): PROJECT · TASK ID (handle pill) · CREATED (local time)

// PencilIcon — Edit affordance (SVG, NOT emoji, per the no-emoji-icon rule).
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
  task: Task;
  projectName?: string;
  /** resolved assignee display name ("" / ref-echo when unresolved). */
  assigneeName?: string;
  /**
   * T106: the owning plan (id + display name) when the task is in a STRUCTURED
   * plan — renders a clickable "Plan" row linking to the plan detail. The page
   * resolves it (via usePlan) and omits it for a backlog task OR the built-in
   * assignment pool (the pool is not a user-facing plan).
   */
  plan?: { id: string; name: string; org_ref?: string };
  /**
   * T193: the issue this task was DERIVED FROM (task.derived_from_issue), resolved
   * to {ref, title} by the page so the sidebar shows a clickable "Related Issue"
   * row (the symmetric reverse of T191's issue→derived-tasks list). Omitted when
   * the task has no derived_from_issue or the issue hasn't resolved yet.
   */
  derivedIssue?: { id: string; org_ref?: string; title: string };
  /** opens the existing TaskEditModal — the single edit path. */
  onEdit: () => void;
  /** whether the task is editable (non-terminal). Terminal → no Edit button. */
  editable: boolean;
}

export function TaskDetailSidebar({
  task,
  projectName,
  assigneeName,
  plan,
  derivedIssue,
  onEdit,
  editable,
}: Props): React.ReactElement {
  const { t } = useTranslation('work');
  const tk = task;
  // Hoist to a const so the truthy-narrowing below survives into the
  // openSender closure (T102) — TS re-widens a mutable property (tk.assignee)
  // inside a callback, but preserves narrowing of a const local.
  const assignee = tk.assignee;
  const tags = tk.tags ?? [];
  const duration = formatStatusDuration(tk.status_changed_at);
  // T102: clicking the assignee opens that identity's activity sidebar (the
  // shared SenderDetailSidebar — agent info + activity feed), reusing the same
  // openSender path as @mentions / message senders. Null-safe: when rendered
  // without a SenderSidebarProvider the name stays plain (no-op).
  const openSender = useSenderSidebar();

  return (
    <aside
      className="space-y-4 rounded border border-border-base bg-bg-elevated p-3 text-sm"
      data-testid="task-detail-sidebar"
      aria-label={t('task.sidebar.ariaLabel')}
    >
      {/* ───────── TOP: display (edit only via the modal) ───────── */}
      <section className="space-y-3" data-testid="task-sidebar-editable">
        <div className="flex items-start justify-between gap-2">
          <p className="text-xs font-semibold uppercase tracking-wide text-text-muted">{t('task.sidebar.details')}</p>
          {editable && (
            <button
              type="button"
              onClick={onEdit}
              className="inline-flex items-center gap-1 rounded bg-bg-subtle px-2 py-1 text-xs font-medium text-text-primary hover:bg-border-base"
              data-testid="task-edit-button"
              aria-label={t('task.sidebar.editTaskAria')}
            >
              <PencilIcon />
              {t('task.sidebar.editTask')}
            </button>
          )}
        </div>

        {/* STATUS + in-status duration — display only */}
        <div data-testid="task-sidebar-status">
          <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">{t('task.sidebar.status')}</p>
          <div className="flex flex-wrap items-center gap-2">
            <StatusBlock status={tk.status} />
            {duration && (
              <span
                className="text-xs text-text-muted"
                data-testid="task-status-duration"
                aria-label={t('task.sidebar.inStatusFor', { duration })}
                title={t('task.sidebar.inStatusFor', { duration })}
              >
                {duration}
              </span>
            )}
          </div>
        </div>

        {/* ASSIGNEE — avatar + name, display only */}
        <div data-testid="task-sidebar-assignee">
          <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">{t('task.sidebar.assignee')}</p>
          <div className="flex flex-wrap items-center gap-2">
            {assignee ? (
              openSender ? (
                <button
                  type="button"
                  onClick={() => openSender(assignee)}
                  data-testid="task-assignee-open"
                  title={t('task.sidebar.openActivity', { name: assigneeName && assigneeName !== assignee ? assigneeName : assignee })}
                  className="inline-flex items-center gap-2 rounded hover:bg-bg-subtle focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                >
                  <Avatar name={assigneeName && assigneeName.trim() ? assigneeName : assignee} size="sm" />
                  <EntityRef
                    id={assignee}
                    name={assigneeName && assigneeName !== assignee ? assigneeName : undefined}
                    testId="task-assignee"
                  />
                </button>
              ) : (
                <span className="inline-flex items-center gap-2">
                  <Avatar name={assigneeName && assigneeName.trim() ? assigneeName : assignee} size="sm" />
                  <EntityRef
                    id={assignee}
                    name={assigneeName && assigneeName !== assignee ? assigneeName : undefined}
                    testId="task-assignee"
                  />
                </span>
              )
            ) : (
              <span className="text-text-muted" data-testid="task-assignee-empty">
                {t('task.sidebar.unassigned')}
              </span>
            )}
          </div>
        </div>

        {/* TAGS — hashed (both-mode-AA) chips, display only */}
        <div data-testid="task-sidebar-tags">
          <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">{t('task.sidebar.tags')}</p>
          <div className="flex flex-wrap items-center gap-1.5">
            {tags.map((tag) => {
              const c = tagColorFor(tag);
              return (
                <span
                  key={tag}
                  data-testid="task-tag-chip"
                  data-tag={tag}
                  className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${c.bg} ${c.text}`}
                >
                  {tag}
                </span>
              );
            })}
            {tags.length === 0 && (
              <span className="text-xs text-text-muted" data-testid="task-tags-empty">
                {t('task.sidebar.noTags')}
              </span>
            )}
          </div>
        </div>
      </section>

      <hr className="border-border-base" />

      {/* ───────── BOTTOM: read-only ───────── */}
      <section className="space-y-3" data-testid="task-sidebar-readonly">
        {tk.project_id && (
          <div>
            <p className="mb-0.5 text-xs uppercase tracking-wide text-text-muted">{t('task.sidebar.project')}</p>
            <OrgLink
              to={`/projects/${encodeURIComponent(tk.project_id)}`}
              className="text-accent hover:underline"
              data-testid="task-project-link"
            >
              {projectName || tk.project_id}
            </OrgLink>
          </div>
        )}

        {/* T106: owning plan (when the task is in a structured plan) → click to
            the plan detail. Hidden for a backlog task / the built-in pool (the
            page passes no `plan` then). */}
        {plan && (
          <div data-testid="task-sidebar-plan">
            <p className="mb-0.5 text-xs uppercase tracking-wide text-text-muted">{t('task.sidebar.plan')}</p>
            <OrgLink
              to={`/projects/${encodeURIComponent(tk.project_id)}/plans/${encodeURIComponent(plan.id)}`}
              className="inline-flex items-center gap-1.5 text-accent hover:underline"
              data-testid="task-plan-link"
              data-plan-id={plan.id}
            >
              {/* T574 sidebar polish: show the human Plan id (P123) as a tag. */}
              <PlanRefTag planId={plan.id} orgRef={plan.org_ref} testId="task-plan-ref-tag" />
              <span>{plan.name}</span>
            </OrgLink>
          </div>
        )}

        {/* T193: the issue this task was derived from (provenance), grouped with
            Project/Plan. Clickable → the issue detail. Reuses the OrgLink + accent
            link style and refLabel (org_ref → "I123", id-handle fallback). Only
            shown when the page resolved the derived issue. */}
        {derivedIssue && (
          <div data-testid="task-sidebar-derived-issue">
            <p className="mb-0.5 text-xs uppercase tracking-wide text-text-muted">{t('task.sidebar.relatedIssue')}</p>
            <OrgLink
              to={`/projects/${encodeURIComponent(tk.project_id)}/issues/${encodeURIComponent(derivedIssue.id)}`}
              className="inline-flex items-center gap-1.5 text-accent hover:underline"
              data-testid="task-derived-issue-link"
              data-issue-id={derivedIssue.id}
              title={derivedIssue.title}
            >
              {/* T574 sidebar polish: render the Issue id (I123) as a tag, matching
                  the Plan id tag above (was an inline mono prefix). */}
              <IssueRefTag issueId={derivedIssue.id} orgRef={derivedIssue.org_ref} testId="task-derived-issue-ref-tag" />
              <span className="min-w-0 truncate">{derivedIssue.title}</span>
            </OrgLink>
          </div>
        )}

        {tk.execution_mirror && (
          <div data-testid="task-sidebar-execution-mirror">
            <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">{t('task.sidebar.executionMirror')}</p>
            <div className="space-y-2 rounded border border-border-base bg-bg-subtle p-2">
              <div className="flex flex-wrap items-center gap-1.5 text-xs">
                <span className="rounded bg-bg-elevated px-1.5 py-0.5 font-mono text-text-secondary" data-testid="task-execution-mode">
                  {tk.execution_mirror.execution_mode || t('task.sidebar.executionUnknown')}
                </span>
                {tk.execution_mirror.executor_state && (
                  <span className="rounded bg-bg-elevated px-1.5 py-0.5 text-text-secondary" data-testid="task-executor-state">
                    {tk.execution_mirror.executor_state}
                  </span>
                )}
                {tk.execution_mirror.required_next_action && (
                  <span className="rounded bg-status-amber-bg px-1.5 py-0.5 font-medium text-status-amber-fg" data-testid="task-execution-next-action">
                    {tk.execution_mirror.required_next_action}
                  </span>
                )}
              </div>
              <MirrorField label={t('task.sidebar.executor')} value={tk.execution_mirror.executor_id} testId="task-executor-id" mono />
              <MirrorField label={t('task.sidebar.delivery')} value={tk.execution_mirror.delivery_state} testId="task-delivery-state" />
              <MirrorField label={t('task.sidebar.branch')} value={tk.execution_mirror.branch} testId="task-execution-branch" mono />
              <MirrorField label={t('task.sidebar.headSha')} value={shortSHA(tk.execution_mirror.head_sha)} title={tk.execution_mirror.head_sha} testId="task-execution-head-sha" mono />
              <MirrorField label={t('task.sidebar.worktree')} value={tk.execution_mirror.worktree} testId="task-execution-worktree" mono truncate />
              <MirrorField
                label={t('task.sidebar.observed')}
                value={tk.execution_mirror.observed_at ? formatLocalTime(tk.execution_mirror.observed_at) : ''}
                testId="task-execution-observed"
              />
            </div>
          </div>
        )}

        <div>
          <p className="mb-0.5 text-xs uppercase tracking-wide text-text-muted">{t('task.sidebar.taskId')}</p>
          {/* #192 chrome rule: id-as-content → a clean handle pill (tail), full id on hover. */}
          <span
            className="inline-block rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-xs text-text-secondary"
            data-testid="task-id-pill"
            title={tk.id}
          >
            {refLabel(tk.org_ref, tk.id)}
          </span>
        </div>

        <div>
          <p className="mb-0.5 text-xs uppercase tracking-wide text-text-muted">{t('task.sidebar.created')}</p>
          <span className="text-text-secondary" data-testid="task-created">
            {formatLocalTime(tk.created_at)}
          </span>
        </div>
      </section>

      {/* Divider before the change-history block — separates it from the read-only
          meta, mirroring the editable↔read-only divider above. */}
      <hr className="border-border-base" data-testid="task-sidebar-history-divider" />

      {/* 变更记录 / audit-trail (change-log design §7): this task's semantic change
          history rendered as a human-readable timeline, after the read-only block. */}
      <ObjectAuditTimeline objectType="task" projectId={tk.project_id} objectId={tk.id} />
    </aside>
  );
}

function shortSHA(sha?: string): string {
  return sha ? sha.slice(0, 12) : '';
}

function MirrorField({
  label,
  value,
  title,
  testId,
  mono,
  truncate,
}: {
  label: string;
  value?: string;
  title?: string;
  testId: string;
  mono?: boolean;
  truncate?: boolean;
}): React.ReactElement | null {
  if (!value) return null;
  return (
    <div className="grid grid-cols-[5.5rem_minmax(0,1fr)] gap-2 text-xs">
      <span className="text-text-muted">{label}</span>
      <span
        className={`${mono ? 'font-mono' : ''} ${truncate ? 'truncate' : 'break-words'} text-text-secondary`}
        data-testid={testId}
        title={title || value}
      >
        {value}
      </span>
    </div>
  );
}

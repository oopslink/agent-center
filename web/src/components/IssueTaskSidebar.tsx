import type React from 'react';
import type { IssueStatus, TaskStatus } from '@/api/types';
import { tagColorFor } from '@/components/tagColors';

// v2.8.1 #5th (@oopslink Phabricator-style Issue/Task refactor) — the shared
// right-hand metadata sidebar for IssueDetail + TaskDetail. Pure prop-driven so
// both pages reuse it (single source, no per-page drift). It renders a prominent
// status block, optional action buttons, a metadata list, and (Task-only) a
// WorkItems status summary.
//
// Status colors align with the existing StatusChip (#258) palette — no
// cross-component drift (a big block in one hue + a chip in another would
// confuse). @oopslink REVISION 4 lock: white text on a saturated color
// background (bg-<color> text-white). blocked uses the custom `blockedred`
// token (#dc2626) so the a11y guardrail's raw bg-red-/text-red- ban stays green.
type StatusKey = IssueStatus | TaskStatus;

const STATUS_BLOCK: Record<StatusKey, { label: string; cls: string }> = {
  // not started (sky)
  open: { label: 'Open', cls: 'bg-sky-600 text-white' },
  // in flight (blue)
  in_progress: { label: 'In Progress', cls: 'bg-blue-600 text-white' },
  running: { label: 'Running', cls: 'bg-blue-600 text-white' },
  // blocked (red #dc2626 via custom blockedred token)
  blocked: { label: 'Blocked', cls: 'bg-blockedred text-white' },
  // done (green)
  resolved: { label: 'Resolved', cls: 'bg-green-600 text-white' },
  completed: { label: 'Completed', cls: 'bg-green-600 text-white' },
  // verified (teal — distinct hue from done green)
  verified: { label: 'Verified', cls: 'bg-teal-600 text-white' },
  // closed (Issue) → slate (terminal)
  closed: { label: 'Closed', cls: 'bg-slate-500 text-white' },
  // discarded (both Issue+Task; replaces canceled/withdrawn) → zinc.
  discarded: { label: 'Discarded', cls: 'bg-zinc-700 text-white' },
  // reopened (amber — back in play)
  reopened: { label: 'Reopened', cls: 'bg-amber-600 text-white' },
};

export function StatusBlock({ status }: { status: StatusKey }): React.ReactElement {
  const s = STATUS_BLOCK[status] ?? { label: status, cls: 'bg-slate-500 text-white' };
  return (
    <div
      className={`rounded px-3 py-2 text-center text-sm font-semibold uppercase tracking-wide ${s.cls}`}
      data-testid="status-block"
      data-status={status}
    >
      {s.label}
    </div>
  );
}

export interface SidebarMetaRow {
  label: string;
  value: React.ReactNode;
  testId?: string;
}

export interface IssueTaskSidebarProps {
  /** current status — drives the prominent StatusBlock. */
  status: StatusKey;
  /** action buttons (Edit / transitions); rendered under the status block. */
  actions?: React.ReactNode;
  /** metadata rows (Created by, Project, Assignee, …) — typed, no bare fields. */
  meta?: SidebarMetaRow[];
  /** read-only tag chips (hashed both-mode-AA colors, mirrors TaskDetail). When
   * provided, renders the TAGS section (chips, or a "No tags" placeholder). */
  tags?: string[];
  /** Task-only: a WorkItems status summary (e.g. "2 In Progress · 5 Pending"). */
  workItemsSummary?: React.ReactNode;
}

export function IssueTaskSidebar({
  status,
  actions,
  meta,
  tags,
  workItemsSummary,
}: IssueTaskSidebarProps): React.ReactElement {
  return (
    <aside
      className="space-y-3 rounded border border-border-base bg-bg-elevated p-3 text-sm"
      data-testid="issuetask-sidebar"
      aria-label="Details"
    >
      <StatusBlock status={status} />

      {actions && (
        <div className="flex flex-wrap gap-2" data-testid="sidebar-actions">
          {actions}
        </div>
      )}

      {meta && meta.length > 0 && (
        <dl className="space-y-2" data-testid="sidebar-meta">
          {meta.map((row) => (
            <div key={row.label} className="flex flex-col gap-0.5">
              <dt className="text-xs uppercase tracking-wide text-text-muted">{row.label}</dt>
              <dd className="text-text-secondary" data-testid={row.testId}>
                {row.value}
              </dd>
            </div>
          ))}
        </dl>
      )}

      {tags && (
        <div data-testid="sidebar-tags">
          <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">Tags</p>
          <div className="flex flex-wrap items-center gap-1.5">
            {tags.map((tag) => {
              const c = tagColorFor(tag);
              return (
                <span
                  key={tag}
                  data-testid="sidebar-tag-chip"
                  data-tag={tag}
                  className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${c.bg} ${c.text}`}
                >
                  {tag}
                </span>
              );
            })}
            {tags.length === 0 && (
              <span className="text-xs text-text-muted" data-testid="sidebar-tags-empty">
                No tags
              </span>
            )}
          </div>
        </div>
      )}

      {workItemsSummary && (
        <div data-testid="sidebar-workitems-summary">
          <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">Work items</p>
          {workItemsSummary}
        </div>
      )}
    </aside>
  );
}

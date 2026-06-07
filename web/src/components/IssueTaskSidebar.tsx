import type React from 'react';
import type { IssueStatus, TaskStatus } from '@/api/types';

// v2.8.1 #5th (@oopslink Phabricator-style Issue/Task refactor) — the shared
// right-hand metadata sidebar for IssueDetail + TaskDetail. Pure prop-driven so
// both pages reuse it (single source, no per-page drift). It renders a prominent
// status block, optional action buttons, a metadata list, and (Task-only) a
// WorkItems status summary.
//
// Status colors align with the existing StatusChip (#258) color FAMILY — no
// cross-component drift (a big block in one hue + a chip in another would
// confuse). They use the "深字浅底" X-100 bg / X-900 text pattern; every pair is
// WCAG-AA (>= 4.5:1) on a light surface, most AAA. Two a11y-driven choices:
//  • verified = TEAL (not emerald): teal-100 vs done's green-100 is ~66% more
//    separated in RGB (18.9 vs 11.4), so the hue is a real secondary cue for
//    fast scanning / partial color vision; the text label is the primary cue.
//  • blocked = orange, reopened = purple — consistency with StatusChip (#258).
type StatusKey = IssueStatus | TaskStatus;

const STATUS_BLOCK: Record<StatusKey, { label: string; cls: string }> = {
  // not started (slate)
  open: { label: 'Open', cls: 'bg-slate-100 text-slate-700' },
  // in flight (blue)
  in_progress: { label: 'In Progress', cls: 'bg-blue-100 text-blue-900' },
  assigned: { label: 'Assigned', cls: 'bg-blue-100 text-blue-900' },
  running: { label: 'Running', cls: 'bg-blue-100 text-blue-900' },
  // blocked (orange — StatusChip align)
  blocked: { label: 'Blocked', cls: 'bg-orange-100 text-orange-900' },
  // done (green)
  resolved: { label: 'Resolved', cls: 'bg-green-100 text-green-900' },
  completed: { label: 'Completed', cls: 'bg-green-100 text-green-900' },
  // verified (teal — distinct hue from done green, a11y)
  verified: { label: 'Verified', cls: 'bg-teal-100 text-teal-900' },
  // terminal neutral (slate)
  closed: { label: 'Closed', cls: 'bg-slate-100 text-slate-700' },
  canceled: { label: 'Canceled', cls: 'bg-slate-100 text-slate-700' },
  // withdrawn → slate (terminal, grouped with closed/canceled). Consistency-wins
  // with StatusChip (#258 maps withdrawn → muted, NOT red); AA-clean (~9:1) and
  // guardrail-clean (no raw red-*; the danger token #ef4444 couldn't hit 4.5:1).
  withdrawn: { label: 'Withdrawn', cls: 'bg-slate-100 text-slate-700' },
  // reopened (purple — StatusChip align)
  reopened: { label: 'Reopened', cls: 'bg-purple-100 text-purple-900' },
};

export function StatusBlock({ status }: { status: StatusKey }): React.ReactElement {
  const s = STATUS_BLOCK[status] ?? { label: status, cls: 'bg-slate-100 text-slate-700' };
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
  /** Task-only: a WorkItems status summary (e.g. "2 In Progress · 5 Pending"). */
  workItemsSummary?: React.ReactNode;
}

export function IssueTaskSidebar({
  status,
  actions,
  meta,
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

      {workItemsSummary && (
        <div data-testid="sidebar-workitems-summary">
          <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">Work items</p>
          {workItemsSummary}
        </div>
      )}
    </aside>
  );
}

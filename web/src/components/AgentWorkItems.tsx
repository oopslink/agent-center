import type React from 'react';
import { useMemo, useState } from 'react';
import { OrgLink } from '@/OrgContext';
import { useAgentWorkItems } from '@/api/agents';
import { TypeChip } from '@/components/TypeChip';
import type { AgentWorkItem, WorkItemStatus } from '@/api/types';

// AgentWorkItems (v2.7.1 #228 PR(d)) — the Work items tab body. A READ-ONLY
// table (design4): ID / Title / Type / Priority / Status / Updated, a summary
// strip (N Total · In Progress · Pending · Done · Blocked) and Status/Type
// filters. There is intentionally NO "+ New" button (PD ruling A): work items
// are a projection of task dispatch — they have no manual create endpoint, so a
// disabled/stub button would be a dead affordance. "+ New" returns in v2.8 #235
// as a "Create Task → auto-assign this agent" shortcut.
//
// v2.7.1 fallbacks (no backend schema yet → labelled, never fabricated):
//   Type = "Task" for every row (#231 will model real types), Priority = "—".

// Status → user-facing bucket (the 4 summary buckets + a catch-all). The raw
// WorkItemStatus is kept on the row (data-status) for operators / tests.
type Bucket = 'in_progress' | 'paused' | 'pending' | 'done' | 'blocked' | 'other';

const STATUS_DISPLAY: Record<WorkItemStatus, { label: string; cls: string; bucket: Bucket }> = {
  active: { label: 'In Progress', cls: 'bg-brand/10 text-brand', bucket: 'in_progress' },
  // v2.8.1 #278 D: agent-paused (scheduling autonomy) — a distinct bucket, not
  // "pending" (queued, waiting to be picked) nor "blocked" (system/reconciler).
  // dark: lighter text — the fixed mid-tone (violet/orange-600) on an alpha-tint
  // over the dark page bg is dark-on-dark (FAILs AA in dark mode); the lighter
  // -400 variant restores AA (violet-400 ~5.9:1, the token-based chips below
  // already adapt via --color-* dark variants). Light mode unchanged.
  paused: { label: 'Paused', cls: 'bg-violet-500/10 text-violet-600 dark:text-violet-400', bucket: 'paused' },
  // queued: double fix — orange-600 FAILed even in LIGHT (3.21:1, pre-existing
  // #228) → orange-700 (4.68 AA); + dark:orange-400 (7.03 AA) for dark mode.
  queued: { label: 'Pending', cls: 'bg-orange-500/10 text-orange-700 dark:text-orange-400', bucket: 'pending' },
  waiting_input: { label: 'Blocked', cls: 'bg-danger/10 text-danger', bucket: 'blocked' },
  failed: { label: 'Blocked', cls: 'bg-danger/10 text-danger', bucket: 'blocked' },
  done: { label: 'Done', cls: 'bg-success/10 text-success', bucket: 'done' },
  canceled: { label: 'Canceled', cls: 'bg-bg-subtle text-text-muted', bucket: 'other' },
  superseded: { label: 'Superseded', cls: 'bg-bg-subtle text-text-muted', bucket: 'other' },
};

const STATUS_FILTERS: Array<{ value: Bucket | 'all'; label: string }> = [
  { value: 'all', label: 'All Status' },
  { value: 'in_progress', label: 'In Progress' },
  { value: 'paused', label: 'Paused' },
  { value: 'pending', label: 'Pending' },
  { value: 'blocked', label: 'Blocked' },
  { value: 'done', label: 'Done' },
];

export function AgentWorkItems({ agentId }: { agentId: string }): React.ReactElement {
  const workItems = useAgentWorkItems(agentId);
  const [statusFilter, setStatusFilter] = useState<Bucket | 'all'>('all');
  // v2.7.1: every work item is type "task" (no schema). The filter is present
  // to match the design; "task" is the only non-"all" option.
  const [typeFilter, setTypeFilter] = useState<'all' | 'task'>('all');

  const items = useMemo(() => workItems.data ?? [], [workItems.data]);

  const counts = useMemo(() => {
    const c = { total: items.length, in_progress: 0, paused: 0, pending: 0, done: 0, blocked: 0 };
    for (const w of items) {
      const b = STATUS_DISPLAY[w.status]?.bucket;
      if (b === 'in_progress') c.in_progress += 1;
      else if (b === 'paused') c.paused += 1;
      else if (b === 'pending') c.pending += 1;
      else if (b === 'done') c.done += 1;
      else if (b === 'blocked') c.blocked += 1;
    }
    return c;
  }, [items]);

  const filtered = useMemo(
    () =>
      items.filter((w) => {
        const okStatus = statusFilter === 'all' || STATUS_DISPLAY[w.status]?.bucket === statusFilter;
        const okType = typeFilter === 'all' || typeFilter === 'task'; // all rows are "task" in v2.7.1
        return okStatus && okType;
      }),
    [items, statusFilter, typeFilter],
  );

  return (
    <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="agent-tabpanel-workitems">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-semibold text-text-primary">Work items</h3>
        <div className="flex items-center gap-2">
          <select
            className="rounded border border-border-strong bg-bg-elevated px-2 py-1 text-xs text-text-primary"
            data-testid="agent-workitems-filter-status"
            aria-label="Filter by status"
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value as Bucket | 'all')}
          >
            {STATUS_FILTERS.map((f) => (
              <option key={f.value} value={f.value}>
                {f.label}
              </option>
            ))}
          </select>
          <select
            className="rounded border border-border-strong bg-bg-elevated px-2 py-1 text-xs text-text-primary"
            data-testid="agent-workitems-filter-type"
            aria-label="Filter by type"
            value={typeFilter}
            onChange={(e) => setTypeFilter(e.target.value as 'all' | 'task')}
          >
            <option value="all">All Types</option>
            <option value="task">Task</option>
          </select>
        </div>
      </div>

      {workItems.isLoading && (
        <p className="text-xs text-text-muted" data-testid="agent-workitems-loading">
          Loading work items…
        </p>
      )}
      {workItems.isError && (
        <p className="text-xs text-danger" data-testid="agent-workitems-error">
          {(workItems.error as Error).message}
        </p>
      )}

      {workItems.isSuccess && items.length === 0 && (
        // Dev-suggested copy: explain how work items appear (intent, not affordance).
        <p className="text-xs text-text-muted" data-testid="agent-workitems-empty">
          Work items are created when tasks are assigned to this agent.
        </p>
      )}

      {workItems.isSuccess && items.length > 0 && (
        <>
          {/* Summary strip (v2.8.1 #278: + Paused). Order: Total · In Progress ·
              Paused · Pending · Blocked · Done. */}
          <dl className="mb-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs" data-testid="agent-workitems-summary">
            <span className="font-medium text-text-primary">{counts.total} Total</span>
            <span className="text-brand">{counts.in_progress} In Progress</span>
            <span className="text-violet-600 dark:text-violet-400">{counts.paused} Paused</span>
            <span className="text-orange-700 dark:text-orange-400">{counts.pending} Pending</span>
            <span className="text-danger">{counts.blocked} Blocked</span>
            <span className="text-success">{counts.done} Done</span>
          </dl>

          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs" data-testid="agent-workitems-table">
              <thead>
                <tr className="border-b border-border-base text-[0.625rem] uppercase tracking-wide text-text-muted">
                  <th className="py-1.5 pr-3 font-medium">ID</th>
                  <th className="py-1.5 pr-3 font-medium">Title</th>
                  <th className="py-1.5 pr-3 font-medium">Type</th>
                  <th className="py-1.5 pr-3 font-medium">Priority</th>
                  <th className="py-1.5 pr-3 font-medium">Status</th>
                  <th className="py-1.5 font-medium">Updated</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border-base">
                {filtered.map((w) => (
                  <WorkItemRow key={w.id} item={w} />
                ))}
              </tbody>
            </table>
          </div>

          {filtered.length === 0 && (
            <p className="mt-3 text-xs text-text-muted" data-testid="agent-workitems-no-match">
              No work items match the current filters.
            </p>
          )}
        </>
      )}
    </section>
  );
}

function WorkItemRow({ item: w }: { item: AgentWorkItem }): React.ReactElement {
  // v2.7.1 #206: link the title to its task when resolved; raw pm ref on hover.
  const taskId = w.task_id || w.task_ref?.replace(/^pm:\/\/tasks\//, '') || '';
  const linkable = Boolean(w.task_title && w.project_id && taskId);
  const status = STATUS_DISPLAY[w.status] ?? { label: w.status, cls: 'bg-bg-subtle text-text-muted' };

  return (
    <tr className="align-top" data-testid="agent-workitem-row" data-workitem-id={w.id} data-status={w.status}>
      {/* T100: show the underlying task's org_ref (T84) when present. The work
          item itself has no human-facing number, so absent an org_ref fall back
          to a short id handle with the full id on hover (#192 — never the full
          raw id as chrome). Use the id TAIL: ULIDs lead with a timestamp, so
          near-simultaneously created items share a prefix — the trailing random
          segment distinguishes rows (Tester/Tester2 #228 finding). */}
      <td className="py-2 pr-3 font-mono text-text-muted" data-testid="agent-workitem-id" title={w.id}>
        {w.org_ref || `#${w.id.slice(-6)}`}
      </td>
      <td className="max-w-[18rem] truncate py-2 pr-3" title={w.task_ref}>
        {linkable ? (
          <OrgLink
            to={`/projects/${encodeURIComponent(w.project_id as string)}/tasks/${encodeURIComponent(taskId)}`}
            className="text-text-secondary hover:text-accent"
            data-testid="agent-workitem-task"
          >
            {w.task_title}
          </OrgLink>
        ) : (
          <span className="text-text-secondary">{w.task_title || 'Work item'}</span>
        )}
      </td>
      <td className="py-2 pr-3" data-testid="agent-workitem-type">
        {/* v2.7.1 fallback: every work item is a Task (real types = v2.8 #231). */}
        <TypeChip kind="task" />
      </td>
      <td className="py-2 pr-3 text-text-muted" data-testid="agent-workitem-priority">
        {/* v2.7.1 fallback: no priority schema yet (#231). */}—
      </td>
      <td className="py-2 pr-3" data-testid="agent-workitem-status">
        <span className={`rounded px-1.5 py-0.5 text-[0.625rem] font-medium uppercase tracking-wide ${status.cls}`}>
          {status.label}
        </span>
      </td>
      <td className="py-2 tabular-nums text-text-muted" data-testid="agent-workitem-updated" title={w.updated_at}>
        {formatUpdated(w.updated_at)}
      </td>
    </tr>
  );
}

function formatUpdated(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (d.toDateString() === yesterday.toDateString()) return 'Yesterday';
  return d.toLocaleDateString();
}

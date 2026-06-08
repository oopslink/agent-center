import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { StatusChip, idHandle, shortDate } from '@/components/workItemDisplay';
import type { OrgWorkItem } from '@/api/types';

// OrgWorkItemsView (v2.8 #258) — the shared table body for the org-scoped
// cross-project Issues / Tasks aggregation pages. Same table idiom as the
// per-project #242 tables, plus a Project column (the whole point: see issues/
// tasks across all projects in one org-scoped list).
//
// Columns: ID / Project / Title / Status / Assigned to / Updated.
// Default view = open only (backend excludes terminal states); an "All" toggle
// drops the status filter.
interface QueryShape {
  data?: { items: OrgWorkItem[]; total: number };
  isLoading: boolean;
  isError: boolean;
  error: unknown;
}

// Status options per kind, in lifecycle order — drive the FilterBar chips.
export const STATUS_OPTIONS: Record<'issue' | 'task', string[]> = {
  issue: ['open', 'in_progress', 'resolved', 'closed', 'discarded', 'reopened'],
  task: ['open', 'running', 'blocked', 'completed', 'verified', 'discarded', 'reopened'],
};

export function OrgWorkItemsView({
  kind,
  query,
  selectedStatuses,
  onStatusesChange,
  onCreate,
}: {
  kind: 'issue' | 'task';
  query: QueryShape;
  // selected status filter (multi). Empty = backend default (all open, terminal
  // states excluded) — same default as the old "open only" view.
  selectedStatuses: string[];
  onStatusesChange: (s: string[]) => void;
  // opens the cross-project create modal.
  onCreate: () => void;
}): React.ReactElement {
  const title = kind === 'issue' ? 'Issues' : 'Tasks';
  const seg = kind === 'issue' ? 'issues' : 'tasks';
  const items = query.data?.items ?? [];
  const defaultView = selectedStatuses.length === 0;
  const toggleStatus = (s: string) =>
    onStatusesChange(selectedStatuses.includes(s) ? selectedStatuses.filter((x) => x !== s) : [...selectedStatuses, s]);

  return (
    <section className="space-y-4" data-testid={`page-Org${title}`}>
      <header className="space-y-2 border-b border-border-base pb-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-xl font-semibold text-text-primary">{title}</h2>
          <button
            type="button"
            data-testid="org-workitems-create"
            onClick={onCreate}
            className="rounded bg-brand px-2.5 py-1 text-xs font-medium text-white hover:bg-brand-hover"
          >
            + New {kind === 'issue' ? 'Issue' : 'Task'}
          </button>
        </div>
        {/* FilterBar — status multi-select; empty = default (all open, terminal excluded). */}
        <div className="flex flex-wrap items-center gap-1.5" data-testid="org-workitems-filterbar">
          <span className="text-[0.625rem] uppercase tracking-wide text-text-muted">Status</span>
          {STATUS_OPTIONS[kind].map((s) => {
            const on = selectedStatuses.includes(s);
            return (
              <button
                key={s}
                type="button"
                data-testid={`org-filter-status-${s}`}
                aria-pressed={on}
                onClick={() => toggleStatus(s)}
                className={`rounded px-2 py-0.5 text-xs ${on ? 'bg-brand text-white' : 'bg-bg-subtle text-text-secondary hover:bg-border-base'}`}
              >
                {s.replace(/_/g, ' ')}
              </button>
            );
          })}
          {!defaultView && (
            <button
              type="button"
              data-testid="org-filter-clear"
              onClick={() => onStatusesChange([])}
              className="text-xs text-accent hover:underline"
            >
              Clear
            </button>
          )}
        </div>
      </header>

      {query.isLoading && (
        <p className="text-xs text-text-muted" data-testid="org-workitems-loading">Loading {title.toLowerCase()}…</p>
      )}
      {query.isError && (
        <p className="text-xs text-danger" data-testid="org-workitems-error">{(query.error as Error).message}</p>
      )}
      {query.data && items.length === 0 && (
        <p className="text-xs text-text-muted" data-testid="org-workitems-empty">
          {defaultView ? `No open ${title.toLowerCase()}.` : `No matching ${title.toLowerCase()}.`}
        </p>
      )}

      {query.data && items.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs" data-testid="org-workitems-table">
            <thead>
              <tr className="border-b border-border-base text-[0.625rem] uppercase tracking-wide text-text-muted">
                <th className="py-1.5 pr-3 font-medium">ID</th>
                <th className="py-1.5 pr-3 font-medium">Project</th>
                <th className="py-1.5 pr-3 font-medium">Title</th>
                <th className="py-1.5 pr-3 font-medium">Status</th>
                <th className="py-1.5 pr-3 font-medium">Assigned to</th>
                <th className="py-1.5 pr-3 font-medium">Created</th>
                <th className="py-1.5 font-medium">Updated</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border-base">
              {items.map((it) => (
                <tr key={it.id} data-testid="org-workitem-row" data-id={it.id} data-status={it.status} data-kind={kind}>
                  <td className="py-1.5 pr-3 font-mono text-text-muted" data-testid="org-workitem-id" title={it.id}>
                    {/* org_ref (I12/T34) when present; else id-tail handle (#192 id-as-content). */}
                    {it.org_ref || `#${idHandle(it.id)}`}
                  </td>
                  <td className="py-1.5 pr-3" data-testid="org-workitem-project">
                    <OrgLink
                      to={`/projects/${encodeURIComponent(it.project.id)}`}
                      className="text-text-secondary hover:text-accent"
                      title={it.project.id}
                    >
                      {it.project.name}
                    </OrgLink>
                  </td>
                  <td className="max-w-[20rem] truncate py-1.5 pr-3">
                    <OrgLink
                      to={`/projects/${encodeURIComponent(it.project.id)}/${seg}/${encodeURIComponent(it.id)}`}
                      className="text-text-primary hover:text-accent"
                      data-testid="org-workitem-title"
                    >
                      {it.title || it.id}
                    </OrgLink>
                  </td>
                  <td className="py-1.5 pr-3">
                    <StatusChip status={it.status} />
                  </td>
                  <td className="py-1.5 pr-3 text-text-secondary" data-testid="org-workitem-assignee">
                    {it.assignee ? (
                      // #192: display name visible; member-id on hover (id-as-content).
                      <span title={it.assignee.member_id}>
                        {it.assignee.display_name}
                        {/* #270/#272: archived agent assignee → "(archived)" chip
                            (#215 deleted-peer pattern; ref/history preserved). */}
                        {it.assignee.assignee_lifecycle === 'archived' && (
                          <span
                            className="ml-1 text-xs italic text-text-muted"
                            data-testid="org-workitem-assignee-archived"
                          >
                            (archived)
                          </span>
                        )}
                      </span>
                    ) : (
                      '—'
                    )}
                  </td>
                  <td className="py-1.5 pr-3 tabular-nums text-text-muted" data-testid="org-workitem-created" title={it.created_at}>
                    {shortDate(it.created_at)}
                  </td>
                  <td className="py-1.5 tabular-nums text-text-muted" data-testid="org-workitem-updated" title={it.updated_at}>
                    {shortDate(it.updated_at)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

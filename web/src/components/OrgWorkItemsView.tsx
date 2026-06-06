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

export function OrgWorkItemsView({
  kind,
  query,
  openOnly,
  onOpenOnlyChange,
}: {
  kind: 'issue' | 'task';
  query: QueryShape;
  openOnly: boolean;
  onOpenOnlyChange: (v: boolean) => void;
}): React.ReactElement {
  const title = kind === 'issue' ? 'Issues' : 'Tasks';
  const seg = kind === 'issue' ? 'issues' : 'tasks';
  const items = query.data?.items ?? [];

  return (
    <section className="space-y-4" data-testid={`page-Org${title}`}>
      <header className="flex flex-wrap items-center justify-between gap-2 border-b border-border-base pb-3">
        <h2 className="text-xl font-semibold text-text-primary">{title}</h2>
        {/* default = open only; toggle to include terminal states. */}
        <label className="flex items-center gap-1.5 text-xs text-text-muted">
          <input
            type="checkbox"
            data-testid="org-workitems-openonly"
            checked={openOnly}
            onChange={(e) => onOpenOnlyChange(e.target.checked)}
          />
          Open only
        </label>
      </header>

      {query.isLoading && (
        <p className="text-xs text-text-muted" data-testid="org-workitems-loading">Loading {title.toLowerCase()}…</p>
      )}
      {query.isError && (
        <p className="text-xs text-danger" data-testid="org-workitems-error">{(query.error as Error).message}</p>
      )}
      {query.data && items.length === 0 && (
        <p className="text-xs text-text-muted" data-testid="org-workitems-empty">
          {openOnly ? `No open ${title.toLowerCase()}.` : `No ${title.toLowerCase()}.`}
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

import type React from 'react';
import { useTranslation } from 'react-i18next';
import { OrgLink } from '@/OrgContext';
import { StatusChip, refLabel, shortDate } from '@/components/workItemDisplay';
import { useCreatorLabel } from '@/api/members';
import { ContextPanel } from '@/shell/contextPanel';
import { WorkItemFilterBar, type DateRange } from '@/components/WorkItemFilterBar';
import { SortHeader, Pagination, type ListControls } from '@/components/listControls';
import type { OrgWorkItem } from '@/api/types';

// Re-exported for back-compat: callers (OrgWorkItems page, tests) historically
// imported these from here; they now live with the extracted FilterBar (T131).
export type { DateRange } from '@/components/WorkItemFilterBar';
export { STATUS_OPTIONS } from '@/components/WorkItemFilterBar';

// OrgWorkItemsView (v2.8 #258) — the shared table body for the org-scoped
// cross-project Issues / Tasks aggregation pages. Same table idiom as the
// per-project #242 tables, plus a Project column (the whole point: see issues/
// tasks across all projects in one org-scoped list).
//
// Columns: ID / Project / Title / Status / Assigned to / Created / Creator / Updated.
// Default view = open only (backend excludes terminal states); an "All" toggle
// drops the status filter.
interface QueryShape {
  data?: { items: OrgWorkItem[]; total: number };
  isLoading: boolean;
  isError: boolean;
  error: unknown;
}

// The four date-range keys (used to detect "any date set" for the empty-state copy).
const DATE_KEYS: (keyof DateRange)[] = [
  'created_after',
  'created_before',
  'updated_after',
  'updated_before',
];

export function OrgWorkItemsView({
  kind,
  query,
  selectedStatuses,
  onStatusesChange,
  selectedProjects,
  onProjectsChange,
  assignee,
  onAssigneeChange,
  dateRange,
  onDateRangeChange,
  onCreate,
  selectedId,
  onSelect,
  controls,
}: {
  kind: 'issue' | 'task';
  query: QueryShape;
  // server-side sort + pagination state (per @oopslink). Drives the sortable
  // column headers + the pagination bar; the page passes these to the hook.
  controls: ListControls;
  // selected status filter (multi). Empty = backend default (all open, terminal
  // states excluded) — same default as the old "open only" view.
  selectedStatuses: string[];
  onStatusesChange: (s: string[]) => void;
  // selected project filter (multi). Array of project ids. Empty = all projects.
  // Sent as repeated `project=<id>` params (mirrors the multi-value status param).
  selectedProjects: string[];
  onProjectsChange: (p: string[]) => void;
  // selected assignee filter (single). The prefixed identity ref ("user:<id>" /
  // "agent:<id>"), or '' = Any (no assignee param).
  assignee: string;
  onAssigneeChange: (a: string) => void;
  // #258 raw date-picker values (YYYY-MM-DD; '' = unset).
  dateRange: DateRange;
  onDateRangeChange: (d: DateRange) => void;
  // opens the cross-project create modal.
  onCreate: () => void;
  // v2.10.0 [T3]: the selected row id (drives the col④ metadata panel), or null.
  selectedId: string | null;
  onSelect: (id: string | null) => void;
}): React.ReactElement {
  const { t } = useTranslation('work');
  // Stable, English token used ONLY to build the `page-Org…` testid (NOT shown).
  const titleToken = kind === 'issue' ? 'Issues' : 'Tasks';
  // Localised display title + lowercased variant for the empty/loading copy.
  const title = kind === 'issue' ? t('workItem.title.issues') : t('workItem.title.tasks');
  const titleLower = kind === 'issue' ? t('workItem.titleLower.issues') : t('workItem.titleLower.tasks');
  const seg = kind === 'issue' ? 'issues' : 'tasks';
  // Owner ask: surface the creator's NAME (agent / human), not the raw id.
  const creatorLabel = useCreatorLabel();
  const items = query.data?.items ?? [];
  // v2.10.0 [T3]: the selected item (if still present in the current result set)
  // feeds the read-only col④ metadata panel.
  const selectedItem = selectedId ? items.find((it) => it.id === selectedId) ?? null : null;
  const anyDateSet = DATE_KEYS.some((k) => dateRange[k] !== '');
  // "default view" (empty-state copy) = no status, project, assignee OR date filter.
  const defaultView =
    selectedStatuses.length === 0 && selectedProjects.length === 0 && assignee === '' && !anyDateSet;

  return (
    <section className="space-y-4" data-testid={`page-Org${titleToken}`}>
      <header className="space-y-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="text-xl font-semibold text-text-primary">{title}</h1>
          <button
            type="button"
            data-testid="org-workitems-create"
            onClick={onCreate}
            className="rounded bg-brand px-2.5 py-1 text-xs font-medium text-white hover:bg-brand-hover"
          >
            {t('workItem.new', { type: kind === 'issue' ? t('type.issue') : t('type.task') })}
          </button>
        </div>
        {/* FilterBar — the shared status / project / assignee / date-range control
            (T131: extracted to WorkItemFilterBar so the per-project lists reuse
            the identical UI; the global page shows the Project picker). */}
        <WorkItemFilterBar
          kind={kind}
          selectedStatuses={selectedStatuses}
          onStatusesChange={onStatusesChange}
          selectedProjects={selectedProjects}
          onProjectsChange={onProjectsChange}
          assignee={assignee}
          onAssigneeChange={onAssigneeChange}
          dateRange={dateRange}
          onDateRangeChange={onDateRangeChange}
        />
      </header>

      {query.isLoading && (
        <p className="text-xs text-text-muted" data-testid="org-workitems-loading">{t('workItem.loading', { items: titleLower })}</p>
      )}
      {query.isError && (
        <p className="text-xs text-danger" data-testid="org-workitems-error">{(query.error as Error).message}</p>
      )}
      {query.data && items.length === 0 && (
        <p className="text-xs text-text-muted" data-testid="org-workitems-empty">
          {defaultView ? t('workItem.empty.default', { items: titleLower }) : t('workItem.empty.filtered', { items: titleLower })}
        </p>
      )}

      {/* v2.10.1 [M3] desktop (≥md): the 8-column table. On mobile it would
          h-scroll at 375px (critical①), so it is md:-only and a card flow
          (below) replaces it. */}
      {query.data && items.length > 0 && (
        <div className="hidden overflow-x-auto md:block">
          <table className="w-full text-left text-xs" data-testid="org-workitems-table">
            <thead>
              <tr className="border-b border-border-base text-[0.625rem] uppercase tracking-wide text-text-muted">
                <SortHeader label={t('workItem.col.id')} sortKey="org_ref" controls={controls} className="py-1.5 pr-3 font-medium" />
                <th className="py-1.5 pr-3 font-medium">{t('workItem.col.project')}</th>
                <SortHeader label={t('workItem.col.title')} sortKey="title" controls={controls} className="py-1.5 pr-3 font-medium" />
                <SortHeader label={t('workItem.col.status')} sortKey="status" controls={controls} className="py-1.5 pr-3 font-medium" />
                <th className="py-1.5 pr-3 font-medium">{t('workItem.col.assignedTo')}</th>
                <th className="py-1.5 pr-3 font-medium">{t('workItem.col.plan')}</th>
                <SortHeader label={t('workItem.col.created')} sortKey="created_at" controls={controls} className="py-1.5 pr-3 font-medium" />
                <th className="py-1.5 pr-3 font-medium">{t('workItem.col.creator')}</th>
                <SortHeader label={t('workItem.col.updated')} sortKey="updated_at" controls={controls} className="py-1.5 font-medium" />
              </tr>
            </thead>
            <tbody className="divide-y divide-border-base">
              {items.map((it) => {
                const isSelected = it.id === selectedId;
                return (
                <tr
                  key={it.id}
                  data-testid="org-workitem-row"
                  data-id={it.id}
                  data-status={it.status}
                  data-kind={kind}
                  data-selected={isSelected}
                  aria-selected={isSelected}
                  // v2.10.0 [T3]: clicking a row selects it → opens the col④
                  // read-only metadata panel (toggles off when re-clicked). Links
                  // inside the row stopPropagation so they navigate, not select.
                  onClick={() => onSelect(isSelected ? null : it.id)}
                  className={`cursor-pointer ${isSelected ? 'bg-bg-subtle' : 'hover:bg-bg-subtle/60'}`}
                >
                  <td className="py-1.5 pr-3 font-mono text-text-muted" data-testid="org-workitem-id" title={it.id}>
                    {/* org_ref (I12/T34) when present; else id-tail handle (#192 id-as-content). */}
                    {refLabel(it.org_ref, it.id)}
                  </td>
                  <td className="py-1.5 pr-3" data-testid="org-workitem-project">
                    <OrgLink
                      to={`/projects/${encodeURIComponent(it.project.id)}`}
                      className="text-text-secondary hover:text-accent"
                      title={it.project.id}
                      onClick={(e) => e.stopPropagation()}
                    >
                      {it.project.name}
                    </OrgLink>
                  </td>
                  <td className="max-w-[20rem] truncate py-1.5 pr-3">
                    <OrgLink
                      to={`/projects/${encodeURIComponent(it.project.id)}/${seg}/${encodeURIComponent(it.id)}`}
                      className="text-text-primary hover:text-accent"
                      data-testid="org-workitem-title"
                      onClick={(e) => e.stopPropagation()}
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
                            {t('workItem.archived')}
                          </span>
                        )}
                      </span>
                    ) : (
                      '—'
                    )}
                  </td>
                  <td className="py-1.5 pr-3 text-text-muted" data-testid="org-workitem-plan" title={it.plan_name || undefined}>
                    {it.plan_name || '—'}
                  </td>
                  <td className="py-1.5 pr-3 tabular-nums text-text-muted" data-testid="org-workitem-created" title={it.created_at}>
                    {shortDate(it.created_at)}
                  </td>
                  <td className="py-1.5 pr-3 text-text-secondary" data-testid="org-workitem-creator" title={it.creator_ref ?? ''}>
                    {creatorLabel(it.creator_ref)}
                  </td>
                  <td className="py-1.5 tabular-nums text-text-muted" data-testid="org-workitem-updated" title={it.updated_at}>
                    {shortDate(it.updated_at)}
                  </td>
                </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* v2.10.1 [M3] mobile (<md) card flow — mirrors the table rows without a
          horizontal scroll (critical①). Tapping a card selects it (→ the col④
          metadata, which the M1 shell reflows to a bottom sheet); the title and
          project links navigate (stopPropagation so they don't also select). */}
      {query.data && items.length > 0 && (
        <ul className="space-y-2 md:hidden" data-testid="org-workitems-cards">
          {items.map((it) => {
            const isSelected = it.id === selectedId;
            return (
              <li key={it.id}>
                <div
                  data-testid="org-workitem-card"
                  data-id={it.id}
                  data-status={it.status}
                  data-kind={kind}
                  data-selected={isSelected}
                  aria-selected={isSelected}
                  onClick={() => onSelect(isSelected ? null : it.id)}
                  className={`min-h-[44px] cursor-pointer rounded-xl border border-border-base bg-bg-elevated p-3 shadow-1 ${
                    isSelected ? 'ring-2 ring-accent' : ''
                  }`}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span
                      className="font-mono text-[0.6875rem] text-text-muted"
                      data-testid="org-workitem-card-id"
                      title={it.id}
                    >
                      {/* org_ref (I12/T34) when present; else id-tail handle (#192). */}
                      {refLabel(it.org_ref, it.id)}
                    </span>
                    <StatusChip status={it.status} />
                  </div>
                  <OrgLink
                    to={`/projects/${encodeURIComponent(it.project.id)}/${seg}/${encodeURIComponent(it.id)}`}
                    className="mt-1 block text-sm font-semibold text-text-primary hover:text-accent"
                    data-testid="org-workitem-card-title"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <span className="line-clamp-2">{it.title || it.id}</span>
                  </OrgLink>
                  <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-text-muted">
                    <OrgLink
                      to={`/projects/${encodeURIComponent(it.project.id)}`}
                      className="truncate text-text-secondary hover:text-accent"
                      title={it.project.id}
                      onClick={(e) => e.stopPropagation()}
                    >
                      {it.project.name}
                    </OrgLink>
                    {it.assignee && (
                      <span className="truncate" title={it.assignee.member_id}>
                        {it.assignee.display_name}
                        {it.assignee.assignee_lifecycle === 'archived' && (
                          <span className="ml-1 italic text-text-muted">{t('workItem.archived')}</span>
                        )}
                      </span>
                    )}
                    {it.creator_ref && (
                      <span className="truncate" title={it.creator_ref}>
                        {t('workItem.by', { name: creatorLabel(it.creator_ref) })}
                      </span>
                    )}
                    <span className="ml-auto tabular-nums" title={it.updated_at}>
                      {shortDate(it.updated_at)}
                    </span>
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      )}

      {/* Server-side pagination bar (shared control) — shown for both the desktop
          table and the mobile cards; hidden when everything fits one page. */}
      {query.data && (
        <Pagination
          page={controls.page}
          pageSize={controls.pageSize}
          total={query.data.total}
          onPageChange={controls.setPage}
        />
      )}

      {/* col④ — read-only metadata for the selected row (v2.10.0 [T3]). Mounting
          <ContextPanel> reveals the fourth column; clearing the selection (or
          navigating away) unmounts it → back to three columns. */}
      {selectedItem && (
        <ContextPanel>
          <WorkItemMetaPanel
            item={selectedItem}
            kind={kind}
            seg={seg}
            onClose={() => onSelect(null)}
          />
        </ContextPanel>
      )}
    </section>
  );
}

// A single key/value row in the metadata panel.
function MetaKV({ k, children }: { k: string; children: React.ReactNode }): React.ReactElement {
  return (
    <div className="flex items-center justify-between gap-3 py-0.5 text-xs">
      <span className="text-text-muted">{k}</span>
      <span className="min-w-0 truncate text-right font-medium text-text-primary">{children}</span>
    </div>
  );
}

// WorkItemMetaPanel — the col④ read-only metadata bar for a selected Issue/Task
// (mockup example 2). Status / assignee / project / id / timestamps, plus a link
// into the item's full detail (its conversation). Read-only by design (T3).
function WorkItemMetaPanel({
  item,
  kind,
  seg,
  onClose,
}: {
  item: OrgWorkItem;
  kind: 'issue' | 'task';
  seg: string;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const label = kind === 'issue' ? t('type.issue') : t('type.task');
  return (
    <div className="flex flex-col" data-testid="org-workitem-meta-panel" data-id={item.id}>
      <div className="flex items-center justify-between px-4 pb-1 pt-3.5">
        <h2 className="text-[0.625rem] font-semibold uppercase tracking-wider text-text-muted">
          {t('workItem.meta.heading', { type: label })}
        </h2>
        <button
          type="button"
          onClick={onClose}
          data-testid="org-workitem-meta-close"
          aria-label={t('workItem.meta.closeAria')}
          title={t('workItem.meta.close')}
          className="inline-flex h-5 w-5 items-center justify-center rounded text-text-muted hover:bg-bg-subtle hover:text-text-primary"
        >
          <span aria-hidden="true">&times;</span>
        </button>
      </div>

      <div className="border-b border-border-base px-4 pb-2.5">
        <MetaKV k={t('workItem.meta.status')}><StatusChip status={item.status} /></MetaKV>
        <MetaKV k={t('workItem.meta.assignee')}>
          {item.assignee ? (
            <span title={item.assignee.member_id}>
              {item.assignee.display_name}
              {item.assignee.assignee_lifecycle === 'archived' && (
                <span className="ml-1 italic text-text-muted">{t('workItem.archived')}</span>
              )}
            </span>
          ) : (
            <span className="text-text-muted">—</span>
          )}
        </MetaKV>
        <MetaKV k={t('workItem.meta.project')}>{item.project.name}</MetaKV>
      </div>

      <div className="border-b border-border-base px-4 pb-2.5 pt-1.5">
        <MetaKV k={t('workItem.meta.id')}>
          <span className="font-mono text-[0.6875rem]">{refLabel(item.org_ref, item.id)}</span>
        </MetaKV>
        <MetaKV k={t('workItem.meta.created')}><span className="tabular-nums">{shortDate(item.created_at)}</span></MetaKV>
        <MetaKV k={t('workItem.meta.updated')}><span className="tabular-nums">{shortDate(item.updated_at)}</span></MetaKV>
      </div>

      <h2 className="px-4 pb-1 pt-3 text-[0.625rem] font-semibold uppercase tracking-wider text-text-muted">
        {t('workItem.meta.conversation')}
      </h2>
      <div className="px-4 pb-3">
        <OrgLink
          to={`/projects/${encodeURIComponent(item.project.id)}/${seg}/${encodeURIComponent(item.id)}`}
          data-testid="org-workitem-meta-open"
          className="inline-flex items-center gap-1 text-xs text-accent hover:underline"
        >
          <span aria-hidden="true">↗</span>
          {t('workItem.meta.openDiscussion', { type: label })}
        </OrgLink>
      </div>
    </div>
  );
}

import type React from 'react';
import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { OrgLink } from '@/OrgContext';
import { useOrgPlans, type OrgPlanItem, type OrgPlanFilters } from '@/api/plans';
import { useProjects } from '@/api/projects';
import { PlanStatusChip, PlanFailedIndicator, planProgressLabel, PlanRefTag } from '@/components/planDisplay';
import { shortDate } from '@/components/workItemDisplay';
import { SortHeader, Pagination, useListControls } from '@/components/listControls';
import { ContextPanel } from '@/shell/contextPanel';
import { normalizeIdentityRef } from '@/api/members';

// OrgPlans (v2.10.0 [T6]) — the global, org-scoped, cross-project Plan list
// (Workspace > Plan), modelled on the cross-project Tasks list. col③ = a
// searchable/filterable table of every structured plan in the org (name /
// status / project / progress / updated); selecting a row opens the col④
// read-only summary (status / project / nodes / created) with a link into the
// Plan detail (Chat / DAG / Task-list tabs — the existing PlanDetail page).
//
// Plan creation is NOT here: plans are authored per-project on the project Work
// Board (T5), so this global list is read+navigate (status filtering + search).

// Status chips offered by the filter bar, in lifecycle order. The chips drive
// the explicit status filter (`?status=`). `archived` is terminal and EXCLUDED
// from the backend default (no chip on) — it surfaces only when its chip is
// toggled on (T98: let the global list view archived plans, which otherwise
// disappear). Archived plans open as read-only detail (PlanDetail offers no
// Start/Advance/Stop/destroy for a terminal plan); archive is irreversible by
// domain design, so there is no unarchive action here.
const PLAN_STATUS_OPTIONS = ['draft', 'running', 'done', 'archived'] as const;

// A small done/total progress bar (mockup `.pgmini`). Tokens only.
function ProgressMini({ done, total }: { done: number; total: number }): React.ReactElement {
  const pct = total > 0 ? Math.round((done / total) * 100) : 0;
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="h-1.5 w-12 overflow-hidden rounded-full bg-border-base" aria-hidden="true">
        <span className="block h-full bg-success" style={{ width: `${pct}%` }} />
      </span>
      <span className="tabular-nums text-text-muted">{planProgressLabel({ done, total })}</span>
    </span>
  );
}

export default function OrgPlansPage(): React.ReactElement {
  const { slug } = useParams<{ slug: string }>();
  const [search, setSearch] = useState('');
  const [selectedStatuses, setSelectedStatuses] = useState<string[]>([]);
  const [selectedProject, setSelectedProject] = useState<string>('');
  const [selectedId, setSelectedId] = useState<string | null>(null);

  // server-side sort + pagination (per @oopslink). Default newest-first.
  const controls = useListControls({ pageSize: 25, defaultSort: 'updated_at', defaultDir: 'desc' });

  const filters: OrgPlanFilters = {
    sort: controls.sort,
    dir: controls.dir,
    page: controls.page,
    page_size: controls.pageSize,
  };
  if (selectedStatuses.length > 0) filters.status = selectedStatuses;
  if (selectedProject) filters.project = [selectedProject];
  // name search is now SERVER-side (q param) so it works across pages, not just
  // the loaded page (which client-side search would have broken under pagination).
  const q = search.trim();
  if (q) filters.q = q;
  const query = useOrgPlans(slug, filters);
  const projects = useProjects();
  const projectList = projects.data ?? [];

  const items = query.data?.items ?? [];
  const selected = selectedId ? items.find((p) => p.id === selectedId) ?? null : null;

  // Reset to page 1 whenever a filter/search narrows the set (else you could be
  // stranded on an out-of-range page after the result count shrinks).
  const setPage = controls.setPage;
  useEffect(() => {
    setPage(1);
  }, [q, selectedProject, selectedStatuses, setPage]);

  const toggleStatus = (s: string) =>
    setSelectedStatuses((cur) => (cur.includes(s) ? cur.filter((x) => x !== s) : [...cur, s]));
  const anyFilter = selectedStatuses.length > 0 || selectedProject !== '' || q !== '';
  const clearAll = () => {
    setSelectedStatuses([]);
    setSelectedProject('');
    setSearch('');
  };

  return (
    <section className="space-y-4" data-testid="page-OrgPlans">
      <header className="space-y-2">
        {/* v2.10.2 [T142]: "Plans" (plural) — matches the Workspace nav + the
            Projects/Issues/Tasks list-page title convention. */}
        <h1 className="text-xl font-semibold text-text-primary">Plans</h1>
        <div
          className="space-y-2 rounded-md border border-border-base bg-bg-subtle/40 p-2.5"
          data-testid="org-plans-filterbar"
        >
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <label className="flex flex-1 items-center gap-1.5 text-[0.625rem] font-medium uppercase tracking-wide text-text-muted">
              <span className="sr-only">Search plans</span>
              <input
                type="search"
                data-testid="org-plans-search"
                aria-label="Search plans"
                placeholder="Search plans…"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="min-w-[10rem] flex-1 rounded border border-border-base bg-bg-base px-2 py-1 text-xs normal-case tracking-normal text-text-secondary"
              />
            </label>
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-[0.625rem] font-medium uppercase tracking-wide text-text-muted">Status</span>
              {PLAN_STATUS_OPTIONS.map((s) => {
                const on = selectedStatuses.includes(s);
                return (
                  <button
                    key={s}
                    type="button"
                    data-testid={`org-plan-status-${s}`}
                    aria-pressed={on}
                    onClick={() => toggleStatus(s)}
                    className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs ${
                      on
                        ? 'bg-brand text-white'
                        : 'border border-border-base bg-bg-base text-text-secondary hover:bg-bg-subtle'
                    }`}
                  >
                    {s}
                  </button>
                );
              })}
            </div>
            <label className="flex items-center gap-1.5 text-[0.625rem] font-medium uppercase tracking-wide text-text-muted">
              <span>Project</span>
              <select
                data-testid="org-plan-project"
                aria-label="Project"
                value={selectedProject}
                onChange={(e) => setSelectedProject(e.target.value)}
                className="rounded border border-border-base bg-bg-base px-1.5 py-0.5 text-xs normal-case tracking-normal text-text-secondary"
              >
                <option value="">All projects</option>
                {projectList.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
            <button
              type="button"
              data-testid="org-plans-clear"
              onClick={clearAll}
              disabled={!anyFilter}
              className="ml-auto inline-flex items-center gap-1 text-xs text-accent hover:underline disabled:text-text-muted disabled:no-underline disabled:opacity-60"
            >
              <span aria-hidden="true">&times;</span>
              Clear filters
            </button>
          </div>
        </div>
      </header>

      {query.isLoading && (
        <p className="text-xs text-text-muted" data-testid="org-plans-loading">Loading plans…</p>
      )}
      {query.isError && (
        <p className="text-xs text-danger" data-testid="org-plans-error">{(query.error as Error).message}</p>
      )}
      {query.data && items.length === 0 && (
        <p className="text-xs text-text-muted" data-testid="org-plans-empty">
          {anyFilter ? 'No matching plans.' : 'No plans yet.'}
        </p>
      )}

      {query.data && items.length > 0 && (
        // v2.10.1 [M4] Mobile (<md): the wide table would h-scroll at 375px, so
        // it reflows to a card flow (below). The table is md:-only.
        <div className="hidden overflow-x-auto md:block">
          <table className="w-full text-left text-xs" data-testid="org-plans-table">
            <thead>
              <tr className="border-b border-border-base text-[0.625rem] uppercase tracking-wide text-text-muted">
                <SortHeader label="Name" sortKey="name" controls={controls} className="py-1.5 pr-3 font-medium" />
                <SortHeader label="Status" sortKey="status" controls={controls} className="py-1.5 pr-3 font-medium" />
                <th className="py-1.5 pr-3 font-medium">Project</th>
                <th className="py-1.5 pr-3 font-medium">Progress</th>
                <SortHeader label="Created" sortKey="created_at" controls={controls} className="py-1.5 pr-3 font-medium" />
                <th className="py-1.5 pr-3 font-medium">Creator</th>
                <SortHeader label="Updated" sortKey="updated_at" controls={controls} className="py-1.5 font-medium" />
              </tr>
            </thead>
            <tbody className="divide-y divide-border-base">
              {items.map((p) => {
                const isSelected = p.id === selectedId;
                return (
                  <tr
                    key={p.id}
                    data-testid="org-plan-row"
                    data-id={p.id}
                    data-status={p.status}
                    aria-selected={isSelected}
                    onClick={() => setSelectedId(isSelected ? null : p.id)}
                    className={`cursor-pointer ${isSelected ? 'bg-bg-subtle' : 'hover:bg-bg-subtle/60'}`}
                  >
                    <td className="max-w-[20rem] py-1.5 pr-3">
                      <span className="flex items-center gap-1.5">
                        {/* v2.10.1 [T99]: the human Plan id (P123). */}
                        <PlanRefTag planId={p.id} orgRef={p.org_ref} testId="org-plan-ref" />
                        <OrgLink
                          to={`/projects/${encodeURIComponent(p.project.id)}/plans/${encodeURIComponent(p.id)}`}
                          className="min-w-0 truncate font-medium text-text-primary hover:text-accent"
                          data-testid="org-plan-name"
                          onClick={(e) => e.stopPropagation()}
                        >
                          {p.name}
                        </OrgLink>
                      </span>
                    </td>
                    <td className="py-1.5 pr-3">
                      <span className="inline-flex items-center gap-1.5">
                        <PlanStatusChip status={p.status} />
                        <PlanFailedIndicator hasFailed={p.has_failed} />
                      </span>
                    </td>
                    <td className="py-1.5 pr-3" data-testid="org-plan-project-cell">
                      <OrgLink
                        to={`/projects/${encodeURIComponent(p.project.id)}`}
                        className="text-text-secondary hover:text-accent"
                        title={p.project.id}
                        onClick={(e) => e.stopPropagation()}
                      >
                        {p.project.name}
                      </OrgLink>
                    </td>
                    <td className="py-1.5 pr-3">
                      <ProgressMini done={p.progress.done} total={p.progress.total} />
                    </td>
                    <td className="py-1.5 pr-3 tabular-nums text-text-muted" data-testid="org-plan-created" title={p.created_at}>
                      {shortDate(p.created_at)}
                    </td>
                    <td className="py-1.5 pr-3 text-text-secondary" data-testid="org-plan-creator" title={p.creator_ref}>
                      {p.creator_ref ? normalizeIdentityRef(p.creator_ref) : '—'}
                    </td>
                    <td className="py-1.5 tabular-nums text-text-muted" data-testid="org-plan-updated" title={p.updated_at}>
                      {shortDate(p.updated_at)}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* v2.10.1 [M4] Mobile (<md) card flow — mirrors the table rows (the wide
          table h-scrolls at 375px). Tapping a card selects it (→ the col④ plan
          summary, which the M1 shell reflows to a bottom sheet); the name links
          into the Plan detail. */}
      {query.data && items.length > 0 && (
        <ul className="space-y-2 md:hidden" data-testid="org-plans-cards">
          {items.map((p) => {
            const isSelected = p.id === selectedId;
            return (
              <li key={p.id}>
                <div
                  data-testid="org-plan-card"
                  data-id={p.id}
                  data-status={p.status}
                  aria-selected={isSelected}
                  onClick={() => setSelectedId(isSelected ? null : p.id)}
                  className={`min-h-[44px] cursor-pointer rounded-xl border border-border-base bg-bg-elevated p-3 shadow-1 ${
                    isSelected ? 'ring-2 ring-accent' : ''
                  }`}
                >
                  <div className="flex items-start justify-between gap-2">
                    <OrgLink
                      to={`/projects/${encodeURIComponent(p.project.id)}/plans/${encodeURIComponent(p.id)}`}
                      className="min-w-0 flex-1 text-sm font-semibold text-text-primary hover:text-accent"
                      data-testid="org-plan-card-name"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <span className="line-clamp-2">{p.name}</span>
                    </OrgLink>
                    <span className="inline-flex shrink-0 items-center gap-1.5">
                      <PlanStatusChip status={p.status} />
                      <PlanFailedIndicator hasFailed={p.has_failed} />
                    </span>
                  </div>
                  <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-text-muted">
                    <span className="truncate" data-testid="org-plan-card-project">{p.project.name}</span>
                    <ProgressMini done={p.progress.done} total={p.progress.total} />
                    {p.creator_ref && (
                      <span className="truncate" title={p.creator_ref}>
                        by {normalizeIdentityRef(p.creator_ref)}
                      </span>
                    )}
                    <span className="tabular-nums" title={p.created_at}>{shortDate(p.created_at)}</span>
                    <span className="ml-auto tabular-nums" title={p.updated_at}>{shortDate(p.updated_at)}</span>
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      )}

      {query.data && (
        <Pagination
          page={controls.page}
          pageSize={controls.pageSize}
          total={query.data.total}
          onPageChange={controls.setPage}
        />
      )}

      {/* col④ — read-only summary of the selected plan (mockup §1 col④). On
          mobile the M1 shell reflows this ContextPanel into a bottom sheet. */}
      {selected && <PlanSummaryPanel plan={selected} onClose={() => setSelectedId(null)} />}
    </section>
  );
}

// A single key/value row in the col④ summary.
function SummaryKV({ k, children }: { k: string; children: React.ReactNode }): React.ReactElement {
  return (
    <div className="flex items-center justify-between gap-3 py-0.5 text-xs">
      <span className="text-text-muted">{k}</span>
      <span className="min-w-0 truncate text-right font-medium text-text-primary">{children}</span>
    </div>
  );
}

// PlanSummaryPanel — the col④ read-only summary for a selected plan, with the
// primary action being "Open" (→ the Plan detail: Chat / DAG / Task-list tabs).
function PlanSummaryPanel({
  plan,
  onClose,
}: {
  plan: OrgPlanItem;
  onClose: () => void;
}): React.ReactElement {
  return (
    <ContextPanel>
      <div className="flex flex-col" data-testid="org-plan-meta-panel" data-id={plan.id}>
        <div className="flex items-center justify-between px-4 pb-1 pt-3.5">
          <h2 className="text-[0.625rem] font-semibold uppercase tracking-wider text-text-muted">
            Plan · summary
          </h2>
          <button
            type="button"
            onClick={onClose}
            data-testid="org-plan-meta-close"
            aria-label="Close summary panel"
            title="Close"
            className="inline-flex h-5 w-5 items-center justify-center rounded text-text-muted hover:bg-bg-subtle hover:text-text-primary"
          >
            <span aria-hidden="true">&times;</span>
          </button>
        </div>

        <div className="border-b border-border-base px-4 pb-2.5">
          <SummaryKV k="ID"><PlanRefTag planId={plan.id} orgRef={plan.org_ref} testId="org-plan-meta-ref" /></SummaryKV>
          <SummaryKV k="Name">{plan.name}</SummaryKV>
          <SummaryKV k="Status"><PlanStatusChip status={plan.status} /></SummaryKV>
          <SummaryKV k="Project">{plan.project.name}</SummaryKV>
          <SummaryKV k="Nodes">{planProgressLabel(plan.progress)}</SummaryKV>
          <SummaryKV k="Created"><span className="tabular-nums">{shortDate(plan.created_at)}</span></SummaryKV>
          <SummaryKV k="Creator">{plan.creator_ref ? normalizeIdentityRef(plan.creator_ref) : '—'}</SummaryKV>
        </div>

        <h2 className="px-4 pb-1 pt-3 text-[0.625rem] font-semibold uppercase tracking-wider text-text-muted">
          Actions
        </h2>
        <div className="px-4 pb-3">
          <OrgLink
            to={`/projects/${encodeURIComponent(plan.project.id)}/plans/${encodeURIComponent(plan.id)}`}
            data-testid="org-plan-meta-open"
            className="inline-flex items-center gap-1 rounded bg-brand px-2.5 py-1 text-xs font-medium text-white hover:bg-brand-hover"
          >
            <span aria-hidden="true">↗</span>
            Open plan
          </OrgLink>
        </div>
      </div>
    </ContextPanel>
  );
}

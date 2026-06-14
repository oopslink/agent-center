import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { StatusChip, idHandle, shortDate, statusSolidClass, statusDotClass } from '@/components/workItemDisplay';
import { useProjects } from '@/api/projects';
import { useMembers, normalizeIdentityRef } from '@/api/members';
import { ContextPanel } from '@/shell/contextPanel';
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
  task: ['open', 'running', 'completed', 'discarded', 'reopened'],
};

// #258 date-range filter state — raw "YYYY-MM-DD" `<input type="date">` values
// (empty = unset). Converted to RFC3339-with-local-offset by the parent before
// hitting the hook (the off-by-one 命门 lives there, not here).
export interface DateRange {
  created_after: string;
  created_before: string;
  updated_after: string;
  updated_before: string;
}

const EMPTY_DATE_RANGE: DateRange = {
  created_after: '',
  created_before: '',
  updated_after: '',
  updated_before: '',
};

// The four date-range keys (used to detect "any date set").
const DATE_KEYS: (keyof DateRange)[] = [
  'created_after',
  'created_before',
  'updated_after',
  'updated_before',
];

// Per-field a11y label + stable testid (kept identical to the old layout so the
// existing date hooks/tests don't churn).
const DATE_META: Record<keyof DateRange, { label: string; testid: string }> = {
  created_after: { label: 'Created after', testid: 'org-filter-created-after' },
  created_before: { label: 'Created before', testid: 'org-filter-created-before' },
  updated_after: { label: 'Updated after', testid: 'org-filter-updated-after' },
  updated_before: { label: 'Updated before', testid: 'org-filter-updated-before' },
};

// One native date input. lang="en" → the browser shows the `yyyy-mm-dd`
// placeholder, NOT the viewer's locale (e.g. the Chinese 年/月/日). The raw
// value stays "YYYY-MM-DD"; the local→RFC3339-offset conversion (the tz 命门)
// happens in the parent page via localDateToRFC3339, untouched here.
function DateInput({
  field,
  dateRange,
  onDateRangeChange,
}: {
  field: keyof DateRange;
  dateRange: DateRange;
  onDateRangeChange: (d: DateRange) => void;
}): React.ReactElement {
  const meta = DATE_META[field];
  return (
    <input
      type="date"
      lang="en"
      data-testid={meta.testid}
      aria-label={meta.label}
      value={dateRange[field]}
      onChange={(e) => onDateRangeChange({ ...dateRange, [field]: e.target.value })}
      className="rounded border border-border-base bg-bg-base px-1.5 py-0.5 text-xs normal-case tracking-normal text-text-secondary"
    />
  );
}

// An inline "Label [start] → [end]" date pair (Created or Updated).
function DatePair({
  groupLabel,
  startKey,
  endKey,
  dateRange,
  onDateRangeChange,
}: {
  groupLabel: string;
  startKey: keyof DateRange;
  endKey: keyof DateRange;
  dateRange: DateRange;
  onDateRangeChange: (d: DateRange) => void;
}): React.ReactElement {
  return (
    <span role="group" aria-label={groupLabel} className="inline-flex items-center gap-1.5">
      <span className="text-[0.625rem] font-medium uppercase tracking-wide text-text-muted">{groupLabel}</span>
      <DateInput field={startKey} dateRange={dateRange} onDateRangeChange={onDateRangeChange} />
      <span aria-hidden="true" className="text-text-muted">→</span>
      <DateInput field={endKey} dateRange={dateRange} onDateRangeChange={onDateRangeChange} />
    </span>
  );
}

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
}: {
  kind: 'issue' | 'task';
  query: QueryShape;
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
  const title = kind === 'issue' ? 'Issues' : 'Tasks';
  const seg = kind === 'issue' ? 'issues' : 'tasks';
  const items = query.data?.items ?? [];
  // v2.10.0 [T3]: the selected item (if still present in the current result set)
  // feeds the read-only col④ metadata panel.
  const selectedItem = selectedId ? items.find((it) => it.id === selectedId) ?? null : null;
  // Project picker source (multi-select) — each Project has id + name.
  const projects = useProjects();
  const projectList = projects.data ?? [];
  // Assignee picker source (single-select) — org members (users + agents).
  const members = useMembers();
  const memberList = members.data ?? [];
  const anyDateSet = DATE_KEYS.some((k) => dateRange[k] !== '');
  // "default view" (empty-state copy) = no status, project, assignee OR date filter.
  const defaultView =
    selectedStatuses.length === 0 && selectedProjects.length === 0 && assignee === '' && !anyDateSet;
  // Clear is offered whenever ANY filter (status / project / assignee / date) is active.
  const anyFilter =
    selectedStatuses.length > 0 || selectedProjects.length > 0 || assignee !== '' || anyDateSet;
  const clearAll = () => {
    onStatusesChange([]);
    onProjectsChange([]);
    onAssigneeChange('');
    onDateRangeChange(EMPTY_DATE_RANGE);
  };
  const toggleStatus = (s: string) =>
    onStatusesChange(selectedStatuses.includes(s) ? selectedStatuses.filter((x) => x !== s) : [...selectedStatuses, s]);
  // Project picker is now a SINGLE <select> (mockup) but the parent still holds a
  // string[] (so the API param contract — repeated `project=<id>` — is unchanged).
  // We surface the lone selected id and wrap a single pick back into a 1- or
  // 0-element array. '' = All projects (no param).
  const selectedProject = selectedProjects[0] ?? '';
  const onProjectChange = (id: string) => onProjectsChange(id ? [id] : []);
  // Build the prefixed identity ref for an assignee option. identity_id may arrive
  // bare ("user-ab12") or already prefixed ("user:user-ab12"); normalize then
  // re-prefix with the member's kind so the backend gets a stable "<kind>:<id>".
  // kind may be absent on legacy rows → derive it from the ref prefix.
  const memberKind = (m: (typeof memberList)[number]): 'user' | 'agent' =>
    m.kind ?? (m.identity_id.startsWith('agent') ? 'agent' : 'user');
  const memberRef = (m: (typeof memberList)[number]): string =>
    `${memberKind(m)}:${normalizeIdentityRef(m.identity_id)}`;

  return (
    <section className="space-y-4" data-testid={`page-Org${title}`}>
      <header className="space-y-2 border-b border-border-base pb-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="text-xl font-semibold text-text-primary">{title}</h1>
          <button
            type="button"
            data-testid="org-workitems-create"
            onClick={onCreate}
            className="rounded bg-brand px-2.5 py-1 text-xs font-medium text-white hover:bg-brand-hover"
          >
            + New {kind === 'issue' ? 'Issue' : 'Task'}
          </button>
        </div>
        {/* FilterBar (@oopslink REV4 mockup) — a clean 3-region card:
            Row 1: STATUS toggle chips + PROJECT single-select + ASSIGNEE single-select.
            Row 2: DATE RANGE = Created/Updated inline start→end pairs + always-on Clear. */}
        <div
          className="space-y-2 rounded-md border border-border-base bg-bg-subtle/40 p-2.5"
          data-testid="org-workitems-filterbar"
        >
          {/* Row 1 — status chips, then the two single-selects (right-aligned). */}
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-[0.625rem] font-medium uppercase tracking-wide text-text-muted">Status</span>
              {STATUS_OPTIONS[kind].map((s) => {
                const on = selectedStatuses.includes(s);
                // SELECTED = solid REV4 fill + white text; UNSELECTED = light bg +
                // a REV4 color DOT + dark text + border. Distinguished by FILL +
                // aria-pressed + the dot — never color alone.
                return (
                  <button
                    key={s}
                    type="button"
                    data-testid={`org-filter-status-${s}`}
                    aria-pressed={on}
                    onClick={() => toggleStatus(s)}
                    className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs ${
                      on
                        ? statusSolidClass(s)
                        : 'border border-border-base bg-bg-base text-text-secondary hover:bg-bg-subtle'
                    }`}
                  >
                    {!on && (
                      <span
                        aria-hidden="true"
                        className={`h-2 w-2 rounded-full ${statusDotClass(s)}`}
                      />
                    )}
                    {s.replace(/_/g, ' ')}
                  </button>
                );
              })}
            </div>
            {/* Project picker — SINGLE <select> per mockup (was multi-chip). Value =
                lone project id ('' = All); parent still keeps a string[] so the
                repeated `project=<id>` API param is unchanged. */}
            <label className="flex items-center gap-1.5 text-[0.625rem] font-medium uppercase tracking-wide text-text-muted">
              <span>Project</span>
              <select
                data-testid="org-filter-project"
                aria-label="Project"
                value={selectedProject}
                onChange={(e) => onProjectChange(e.target.value)}
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
            {/* Assignee picker — SINGLE-select <select> (keyboard-accessible). Each
                option carries a textual kind cue ("· agent" / "· user") so agents vs
                users are distinguishable without color (Avatar #211 discipline).
                Value = the prefixed identity ref ("<kind>:<id>"); '' = Any. */}
            <label className="flex items-center gap-1.5 text-[0.625rem] font-medium uppercase tracking-wide text-text-muted">
              <span>Assignee</span>
              <select
                data-testid="org-filter-assignee"
                aria-label="Assignee"
                value={assignee}
                onChange={(e) => onAssigneeChange(e.target.value)}
                className="rounded border border-border-base bg-bg-base px-1.5 py-0.5 text-xs normal-case tracking-normal text-text-secondary"
              >
                <option value="">Any</option>
                {memberList.map((m) => {
                  const ref = memberRef(m);
                  const name = m.display_name || normalizeIdentityRef(m.identity_id);
                  return (
                    <option key={ref} value={ref}>
                      {name} · {memberKind(m)}
                    </option>
                  );
                })}
              </select>
            </label>
          </div>
          {/* Row 2 — DATE RANGE: two inline start→end pairs (Created / Updated),
              then the always-visible Clear pushed to the right. All 4 pickers stay
              functional + lang="en" (native yyyy-mm-dd placeholder, not 年/月/日). */}
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <span className="text-[0.625rem] font-medium uppercase tracking-wide text-text-muted">Date range</span>
            <DatePair
              groupLabel="Created"
              startKey="created_after"
              endKey="created_before"
              dateRange={dateRange}
              onDateRangeChange={onDateRangeChange}
            />
            <span aria-hidden="true" className="text-text-muted">|</span>
            <DatePair
              groupLabel="Updated"
              startKey="updated_after"
              endKey="updated_before"
              dateRange={dateRange}
              onDateRangeChange={onDateRangeChange}
            />
            {/* Clear — ASCII x glyph (multiplication sign ×, aria-hidden) + text;
                ALWAYS rendered (not conditionally hidden). Disabled when nothing
                is active so it never misleads, but stays in the DOM. NOT an emoji
                (no-emoji-icons guardrail). */}
            <button
              type="button"
              data-testid="org-filter-clear"
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
                    {it.org_ref || `#${idHandle(it.id)}`}
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
                );
              })}
            </tbody>
          </table>
        </div>
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
  const label = kind === 'issue' ? 'Issue' : 'Task';
  return (
    <div className="flex flex-col" data-testid="org-workitem-meta-panel" data-id={item.id}>
      <div className="flex items-center justify-between px-4 pb-1 pt-3.5">
        <h2 className="text-[0.625rem] font-semibold uppercase tracking-wider text-text-muted">
          {label} · metadata
        </h2>
        <button
          type="button"
          onClick={onClose}
          data-testid="org-workitem-meta-close"
          aria-label="Close metadata panel"
          title="Close"
          className="inline-flex h-5 w-5 items-center justify-center rounded text-text-muted hover:bg-bg-subtle hover:text-text-primary"
        >
          <span aria-hidden="true">&times;</span>
        </button>
      </div>

      <div className="border-b border-border-base px-4 pb-2.5">
        <MetaKV k="Status"><StatusChip status={item.status} /></MetaKV>
        <MetaKV k="Assignee">
          {item.assignee ? (
            <span title={item.assignee.member_id}>
              {item.assignee.display_name}
              {item.assignee.assignee_lifecycle === 'archived' && (
                <span className="ml-1 italic text-text-muted">(archived)</span>
              )}
            </span>
          ) : (
            <span className="text-text-muted">—</span>
          )}
        </MetaKV>
        <MetaKV k="Project">{item.project.name}</MetaKV>
      </div>

      <div className="border-b border-border-base px-4 pb-2.5 pt-1.5">
        <MetaKV k="ID">
          <span className="font-mono text-[0.6875rem]">{item.org_ref || `#${idHandle(item.id)}`}</span>
        </MetaKV>
        <MetaKV k="Created"><span className="tabular-nums">{shortDate(item.created_at)}</span></MetaKV>
        <MetaKV k="Updated"><span className="tabular-nums">{shortDate(item.updated_at)}</span></MetaKV>
      </div>

      <h2 className="px-4 pb-1 pt-3 text-[0.625rem] font-semibold uppercase tracking-wider text-text-muted">
        Conversation
      </h2>
      <div className="px-4 pb-3">
        <OrgLink
          to={`/projects/${encodeURIComponent(item.project.id)}/${seg}/${encodeURIComponent(item.id)}`}
          data-testid="org-workitem-meta-open"
          className="inline-flex items-center gap-1 text-xs text-accent hover:underline"
        >
          <span aria-hidden="true">↗</span>
          Open {label} discussion
        </OrgLink>
      </div>
    </div>
  );
}

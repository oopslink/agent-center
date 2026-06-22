import type React from 'react';
import { useState } from 'react';
import { useProjects } from '@/api/projects';
import { useMembers, normalizeIdentityRef } from '@/api/members';
import { statusSolidClass, statusDotClass } from '@/components/workItemDisplay';
import { useIsMobile } from '@/components/WorkItemMobileMeta';

// T339: the filter panel collapses behind a "Filters" disclosure so the list is
// immediately visible — esp. on mobile, where the full status/assignee/date form
// pushed the list far below the fold. Default: collapsed on mobile, open on
// desktop; the user's toggle persists. An active-filter count + Clear stay in the
// header so you see/clear active filters without expanding.
const FILTER_OPEN_KEY = 'ac.workitemfilter.open';
function readStoredOpen(): boolean | null {
  try {
    const v = window.localStorage.getItem(FILTER_OPEN_KEY);
    return v === null ? null : v === '1';
  } catch {
    return null;
  }
}

// WorkItemFilterBar (v2.10.2 [T131]) — the shared status / project / assignee /
// date-range FilterBar for the org-wide Issues/Tasks aggregation pages AND the
// per-project Workspace lists. Extracted from OrgWorkItemsView so both surfaces
// reuse ONE filter UI + one query-param contract (the project lists differ only
// in that the project dimension is fixed → `hideProject`).
//
// Presentational + self-contained: it owns the project/member option sources and
// the filter chrome; the parent owns the filter STATE (so it can convert dates →
// RFC3339-local and feed the right list hook). All `org-filter-*` testids are
// preserved verbatim so existing tests keep working across both surfaces.

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

export const EMPTY_DATE_RANGE: DateRange = {
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

export function WorkItemFilterBar({
  kind,
  selectedStatuses,
  onStatusesChange,
  selectedProjects,
  onProjectsChange,
  assignee,
  onAssigneeChange,
  dateRange,
  onDateRangeChange,
  hideProject = false,
}: {
  kind: 'issue' | 'task';
  selectedStatuses: string[];
  onStatusesChange: (s: string[]) => void;
  // project filter (multi). Array of project ids. Empty = all projects. Hidden +
  // unused when `hideProject` (the per-project lists fix the project by path).
  selectedProjects: string[];
  onProjectsChange: (p: string[]) => void;
  assignee: string;
  onAssigneeChange: (a: string) => void;
  dateRange: DateRange;
  onDateRangeChange: (d: DateRange) => void;
  // T131: hide the Project picker when the surface fixes the project dimension
  // (the per-project Workspace lists). The ONLY difference from the global list.
  hideProject?: boolean;
}): React.ReactElement {
  // Project picker source (multi-select) — each Project has id + name.
  const projects = useProjects();
  const projectList = projects.data ?? [];
  // Assignee picker source (single-select) — org members (users + agents).
  const members = useMembers();
  const memberList = members.data ?? [];
  const dateSetCount = DATE_KEYS.filter((k) => dateRange[k] !== '').length;
  const anyDateSet = dateSetCount > 0;
  // Clear is offered whenever ANY user filter (status / project / assignee / date)
  // is active. The fixed project (hideProject) is NOT a user filter.
  const anyFilter =
    selectedStatuses.length > 0 ||
    (!hideProject && selectedProjects.length > 0) ||
    assignee !== '' ||
    anyDateSet;
  // T339: count of active filter values, shown as a header badge so a collapsed
  // panel still signals "filters are on".
  const activeCount =
    selectedStatuses.length +
    (!hideProject && selectedProjects.length > 0 ? 1 : 0) +
    (assignee !== '' ? 1 : 0) +
    dateSetCount;
  const isMobile = useIsMobile();
  const [open, setOpen] = useState<boolean>(() => readStoredOpen() ?? !isMobile);
  const setOpenPersist = (v: boolean): void => {
    setOpen(v);
    try {
      window.localStorage.setItem(FILTER_OPEN_KEY, v ? '1' : '0');
    } catch {
      /* storage disabled */
    }
  };
  const clearAll = () => {
    onStatusesChange([]);
    if (!hideProject) onProjectsChange([]);
    onAssigneeChange('');
    onDateRangeChange(EMPTY_DATE_RANGE);
  };
  const toggleStatus = (s: string) =>
    onStatusesChange(selectedStatuses.includes(s) ? selectedStatuses.filter((x) => x !== s) : [...selectedStatuses, s]);
  // Project picker is a SINGLE <select> but the parent keeps a string[] (so the
  // repeated `project=<id>` API param is unchanged). '' = All projects (no param).
  const selectedProject = selectedProjects[0] ?? '';
  const onProjectChange = (id: string) => onProjectsChange(id ? [id] : []);
  // Build the prefixed identity ref for an assignee option. identity_id may arrive
  // bare ("user-ab12") or already prefixed ("user:user-ab12"); normalize then
  // re-prefix with the member's kind so the backend gets a stable "<kind>:<id>".
  const memberKind = (m: (typeof memberList)[number]): 'user' | 'agent' =>
    m.kind ?? (m.identity_id.startsWith('agent') ? 'agent' : 'user');
  const memberRef = (m: (typeof memberList)[number]): string =>
    `${memberKind(m)}:${normalizeIdentityRef(m.identity_id)}`;

  return (
    // FilterBar (@oopslink REV4 mockup) — a clean 3-region card:
    // Row 1: STATUS toggle chips + PROJECT single-select + ASSIGNEE single-select.
    // Row 2: DATE RANGE = Created/Updated inline start→end pairs + always-on Clear.
    <div
      className="space-y-2 rounded-md border border-border-base bg-bg-subtle/40 p-2.5"
      data-testid="org-workitems-filterbar"
    >
      {/* T339: disclosure header — toggle + active-count badge (left), Clear
          (right). Always visible so a collapsed panel still shows/clears state. */}
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() => setOpenPersist(!open)}
          aria-expanded={open}
          data-testid="org-filter-toggle"
          className="flex items-center gap-1.5 text-[0.625rem] font-semibold uppercase tracking-wide text-text-muted hover:text-text-secondary"
        >
          <svg
            viewBox="0 0 12 12"
            aria-hidden="true"
            className={`h-2.5 w-2.5 shrink-0 transition-transform ${open ? 'rotate-90' : ''}`}
          >
            <path d="M4 2l4 4-4 4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          <span>Filters</span>
          {activeCount > 0 && (
            <span
              data-testid="org-filter-active-count"
              className="inline-flex min-w-[1.125rem] items-center justify-center rounded-full bg-accent px-1 text-[0.625rem] font-bold leading-none text-white tabular-nums"
            >
              {activeCount}
            </span>
          )}
        </button>
        {/* Clear — ASCII × glyph (aria-hidden) + text; always in the DOM, disabled
            when nothing is active so it never misleads. NOT an emoji. */}
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
      {!open ? null : (
      <>
      {/* Row 1 — status chips, then the two single-selects (right-aligned). */}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-[0.625rem] font-medium uppercase tracking-wide text-text-muted">Status</span>
          {STATUS_OPTIONS[kind].map((s) => {
            const on = selectedStatuses.includes(s);
            // SELECTED = solid REV4 fill + white text; UNSELECTED = light bg + a
            // REV4 color DOT + dark text + border. Distinguished by FILL +
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
        {/* Project picker — SINGLE <select> per mockup. Hidden when the surface
            fixes the project (T131 per-project lists). */}
        {!hideProject && (
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
        )}
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
      {/* Row 2 — DATE RANGE: two inline start→end pairs (Created / Updated), then
          the always-visible Clear pushed to the right. All 4 pickers stay
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
      </div>
      </>
      )}
    </div>
  );
}

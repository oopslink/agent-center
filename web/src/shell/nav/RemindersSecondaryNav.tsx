import type React from 'react';
import { useSearchParams } from 'react-router-dom';
import type { ReminderListFilter, ReminderStatus } from '@/api/reminders';
import type { ModuleSecondaryNavProps } from '@/shell/secondaryNav';

// ============================================================================
// T248 Reminders — col② secondary nav (registered in shell/secondaryNav.tsx).
//
// The three-column fix (issue-c438cde1): the Reminders FILTER rail — search +
// Scope (All / Created by me / Reminding me) + Status (All / Active / Paused) —
// belongs in col② (the left nav column), 1:1 with the approved mockup
// (docs/design/v2.11.0/mockups/reminder-mockup-v0.1-I4.png). It used to live as
// a page-internal <aside>, which made the list page render its OWN sidebar
// inside col③ — breaking the v2.10.0 three-column layout. Now col③ is the list
// (middle workspace) only.
//
// Filter state lives in the URL query (?range=&status=&q=) so col② (this nav)
// and col③ (the Reminders page) share it without a store: this component WRITES
// the params, the page READS them. On mobile the shell renders this nav inside
// the nav sheet, so the same filters drive the full-screen list. The shell owns
// the col② chrome (header / search-launcher / footer / collapse); this fills the
// nav body.
// ============================================================================

const RANGES: ReadonlyArray<{ key: ReminderListFilter; label: string }> = [
  { key: 'all', label: 'All' },
  { key: 'created', label: 'Created by me' },
  { key: 'remindee', label: 'Reminding me' },
];
const STATUSES: ReadonlyArray<{ key: ReminderStatus; label: string; dot: string }> = [
  { key: 'active', label: 'Active', dot: 'bg-success' },
  { key: 'paused', label: 'Paused', dot: 'bg-warning' },
];

export function RemindersSecondaryNav(_props: ModuleSecondaryNavProps): React.ReactElement {
  const [params, setParams] = useSearchParams();
  const range = (params.get('range') as ReminderListFilter) || 'all';
  const status = params.get('status'); // null/'' = all statuses
  const search = params.get('q') ?? '';

  // Merge a single key into the existing query (preserving the others); an empty
  // value drops the key. replace:true so typing/filtering doesn't spam history.
  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    setParams(next, { replace: true });
  };

  return (
    <div className="flex flex-col" data-testid="reminders-secondary-nav">
      <input
        value={search}
        onChange={(e) => setParam('q', e.target.value)}
        placeholder="Search reminders…"
        className="mb-3 w-full rounded-md border border-border-base bg-bg-base px-2.5 py-1.5 text-xs"
        data-testid="reminder-search"
      />
      <FilterGroup label="Scope">
        {RANGES.map((rg) => (
          <FilterItem
            key={rg.key}
            active={range === rg.key}
            onClick={() => setParam('range', rg.key === 'all' ? '' : rg.key)}
            testId={`reminder-range-${rg.key}`}
          >
            {rg.label}
          </FilterItem>
        ))}
      </FilterGroup>
      <FilterGroup label="Status">
        <FilterItem active={!status} onClick={() => setParam('status', '')} testId="reminder-status-all">
          All statuses
        </FilterItem>
        {STATUSES.map((st) => (
          <FilterItem
            key={st.key}
            active={status === st.key}
            onClick={() => setParam('status', st.key)}
            testId={`reminder-status-${st.key}`}
            dot={st.dot}
          >
            {st.label}
          </FilterItem>
        ))}
      </FilterGroup>
    </div>
  );
}

function FilterGroup({ label, children }: { label: string; children: React.ReactNode }): React.ReactElement {
  return (
    <div className="mb-3">
      <div className="px-1 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">{label}</div>
      <div className="space-y-0.5">{children}</div>
    </div>
  );
}

function FilterItem({
  active,
  onClick,
  testId,
  dot,
  children,
}: {
  active: boolean;
  onClick: () => void;
  testId: string;
  dot?: string;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      data-testid={testId}
      className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs ${
        active ? 'bg-brand/10 font-semibold text-brand' : 'text-text-secondary hover:bg-bg-subtle'
      }`}
    >
      {dot && <span className={`h-1.5 w-1.5 rounded-full ${dot}`} />}
      {children}
    </button>
  );
}

import type React from 'react';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { NavLink } from 'react-router-dom';
import { useAgents } from '@/api/agents';
import { useMembers, normalizeIdentityRef } from '@/api/members';
import { AgentStatsPill } from '@/components/AgentBadges';
import type { ModuleSecondaryNavProps } from '@/shell/secondaryNav';
import { useListOrder, rowDragClass } from './useListOrder';

// ============================================================================
// v2.10.0 [T7] Members — col② secondary nav (registered in
// shell/secondaryNav.tsx). Two sections — Humans and Agents — each with an
// "All …" row (the list/table page) plus the individual members, mirroring the
// mockup `docs/design/v2.10.0/members.html` col②. Agent rows open AgentDetail;
// human rows open UserDetail; archived agents are dropped (they live in
// history). The shell owns the col② chrome (header / search / footer / collapse
// / col④ host); this component only fills the nav body.
// ============================================================================

interface NavRow {
  to: string;
  label: string;
  // T235: optional status chips rendered beneath the row label (agent rows show
  // Lifecycle + Availability + derived Idle/Busy; human rows leave it undefined).
  meta?: React.ReactNode;
}

const SECTION_STATE_KEY = 'ac.members.sections';

function readSectionOpen(key: string): boolean {
  try {
    if (typeof localStorage === 'undefined' || typeof localStorage.getItem !== 'function') return true;
    const raw = localStorage.getItem(SECTION_STATE_KEY);
    if (!raw) return true;
    const parsed = JSON.parse(raw) as Record<string, boolean>;
    return parsed[key] === undefined ? true : parsed[key];
  } catch {
    return true;
  }
}

function writeSectionOpen(key: string, open: boolean): void {
  try {
    if (typeof localStorage === 'undefined' || typeof localStorage.setItem !== 'function') return;
    const raw = localStorage.getItem(SECTION_STATE_KEY);
    const parsed = (raw ? (JSON.parse(raw) as Record<string, boolean>) : {}) ?? {};
    parsed[key] = open;
    localStorage.setItem(SECTION_STATE_KEY, JSON.stringify(parsed));
  } catch {
    // ignore
  }
}

export function MembersSecondaryNav({ orgBase }: ModuleSecondaryNavProps): React.ReactElement {
  const { t } = useTranslation('common');
  const agents = useAgents();
  const members = useMembers();

  const humanRows: NavRow[] = (members.data ?? [])
    .filter((m) => m.kind === 'user' || m.identity_id.startsWith('user'))
    .map((m) => {
      const ref = normalizeIdentityRef(m.identity_id);
      return { to: `${orgBase}/users/${encodeURIComponent(ref)}`, label: m.display_name || ref };
    });

  // T322: each agent row carries ONE unified status (dot + word), derived from
  // lifecycle + availability + activity by priority — replacing the three
  // look-alike chips operators found confusing. The full breakdown is in the
  // badge's hover tooltip. `now` is sampled once per render so all rows compare
  // against the same instant.
  const now = Date.now();
  const agentRows: NavRow[] = (agents.data ?? [])
    .filter((a) => a.lifecycle !== 'archived')
    .map((a) => ({
      to: `${orgBase}/agents/${encodeURIComponent(a.id)}`,
      label: a.name || a.id,
      meta: (
        <span className="block" data-testid="agent-nav-status">
          <AgentStatsPill agent={a} now={now} />
        </span>
      ),
    }));

  return (
    <div className="space-y-1" data-testid="members-secondary-nav">
      <NavSection
        sectionKey="humans"
        orderKey={`${orgBase}/members/humans`}
        title={t('shell.members.humans')}
        allTo={`${orgBase}/members/humans`}
        allLabel={t('shell.members.allHumans')}
        rows={humanRows}
      />
      <NavSection
        sectionKey="agents"
        orderKey={`${orgBase}/members/agents`}
        title={t('shell.members.agents')}
        allTo={`${orgBase}/agents`}
        allLabel={t('shell.members.allAgents')}
        rows={agentRows}
      />
    </div>
  );
}

function NavSection({
  sectionKey,
  orderKey,
  title,
  allTo,
  allLabel,
  rows,
}: {
  sectionKey: string;
  orderKey: string;
  title: string;
  allTo: string;
  allLabel: string;
  rows: NavRow[];
}): React.ReactElement {
  const { t } = useTranslation('common');
  const [open, setOpen] = useState(() => readSectionOpen(sectionKey));
  useEffect(() => {
    writeSectionOpen(sectionKey, open);
  }, [sectionKey, open]);

  // Drag-reorder (per-user, persisted) — @oopslink. Rows are identified by their
  // unique `to` path; the "All …" row stays pinned on top and is not draggable.
  const order = useListOrder(orderKey, rows.map((r) => r.to));
  const byTo = new Map(rows.map((r) => [r.to, r]));
  const orderedRows = order.orderedIds.map((to) => byTo.get(to)).filter((r): r is NavRow => r !== undefined);

  return (
    <div>
      <h3 className="px-1">
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          data-testid={`members-section-toggle-${sectionKey}`}
          className="flex w-full items-center justify-between rounded px-1 pb-1 pt-2 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted hover:text-text-secondary"
        >
          <span data-testid="section-label">{title}</span>
          <span aria-hidden="true">{open ? '⌄' : '›'}</span>
        </button>
      </h3>
      {open && (
        <ul className="space-y-0.5" data-testid={`members-section-list-${sectionKey}`}>
          <li>
            <NavLink to={allTo} end className={rowClass} data-testid={`members-all-${sectionKey}`}>
              {allLabel}
            </NavLink>
          </li>
          {orderedRows.map((r) => (
            <li key={r.to} {...order.rowProps(r.to)} className={rowDragClass(order, r.to)}>
              {/* aria-label keeps the link's accessible name = the member name;
                  the T235 status chips are supplementary visual context and must
                  not bloat the announced link name. */}
              <NavLink to={r.to} className={rowClass} aria-label={r.label}>
                {r.meta ? (
                  <span className="flex min-w-0 flex-col gap-0.5">
                    <span className="block truncate">{r.label}</span>
                    {r.meta}
                  </span>
                ) : (
                  <span className="block truncate">{r.label}</span>
                )}
              </NavLink>
            </li>
          ))}
          {rows.length === 0 && (
            <li className="px-2 py-0.5 text-xs italic text-text-muted">{t('shell.none')}</li>
          )}
        </ul>
      )}
    </div>
  );
}

function rowClass({ isActive }: { isActive: boolean }): string {
  return [
    'block rounded-lg px-2 py-1.5 text-sm motion-safe:transition-colors',
    isActive ? 'bg-brand/10 text-brand font-semibold' : 'text-text-primary hover:bg-bg-subtle',
  ].join(' ');
}

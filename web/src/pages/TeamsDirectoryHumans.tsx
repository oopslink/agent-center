// Teams directory — Humans (/organizations/:slug/teams/humans).
//
// MERGED surface (members-into-teams): the union of the old Teams directory
// Humans view (TEAMS column + by-team filter + status chips + search) and the
// retired Members → Humans page (org-role badge + membership status + the
// change-role / disable / re-enable kebab + mobile DM cards).
//
// DATA is a client-side OUTER JOIN, keyed by normalizeIdentityRef applied to
// BOTH sides — normalizeIdentityRef(member.identity_id) === normalizeIdentityRef
// (directoryHuman.ref). A row shows if it appears in EITHER source; a single-
// source row degrades gracefully (em-dash the columns the missing source would
// have filled). The two sides carry DIFFERENT dimensions that we surface side by
// side, never collapsed:
//   • Org role (owner/admin/member) — the member row's permission role.
//   • Team role (planner/coder/…)   — the directory's team-declared free string.
//   • Membership status (joined/disabled) — the member row's membership.
//   • Invite status (Joined/Invited)      — the directory's invite state.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type React from 'react';
import { createPortal } from 'react-dom';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useDirectoryHumans, useTeams, type DirectoryHuman } from '@/api/teams';
import {
  useMembers,
  useChangeMemberRole,
  useDisableMember,
  useReEnableMember,
  normalizeIdentityRef,
  identityRefOf,
  type MemberResult,
} from '@/api/members';
import { useAppStore } from '@/store/app';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { EntityRef } from '@/components/EntityRef';
import { Avatar } from '@/components/Avatar';
import { MembersSegmentControl } from '@/components/MembersSegmentControl';
import { useOpenDm } from '@/components/useOpenDm';
import { Note } from '@/components/teams/kit';
import { FilterChip, TeamsCell } from './TeamsDirectoryAgents';

type StatusFilter = 'all' | 'joined';

// A merged directory row: the same identity seen from the member side, the
// directory side, or both. `member`/`dir` are each present only when that source
// carried the identity (outer join).
interface HumanRow {
  key: string; // normalizeIdentityRef of the identity — the join key
  member?: MemberResult;
  dir?: DirectoryHuman;
  name: string;
  identityRef: string; // prefixed "user:<id>" — for DM + link
}

// v2.7.1 #193: short date for the columns (empty → em dash).
function fmtDate(v?: string): string {
  if (!v) return '—';
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleDateString();
}

const EM_DASH = <span className="text-text-muted">—</span>;

function isHumanMember(m: MemberResult): boolean {
  return m.kind === 'user' || m.identity_id.startsWith('user-') || m.identity_id.startsWith('user:');
}

// Org permission-role badge (ported from the Members → Humans page). Colors keyed
// by role; label via the reused `members` namespace so the strings are not
// duplicated into `teams`.
function RoleBadge({ role }: { role: string }): React.ReactElement {
  const { t } = useTranslation('members');
  const colors: Record<string, string> = {
    owner: 'bg-brand/10 text-brand',
    admin: 'bg-accent/10 text-accent',
    member: 'bg-bg-subtle text-text-secondary',
  };
  return (
    <span className={`rounded px-1.5 py-0.5 text-xs font-medium ${colors[role] ?? colors.member}`}>
      {t(`humans.role.${role}`, { defaultValue: role })}
    </span>
  );
}

export default function TeamsDirectoryHumans(): React.ReactElement {
  const { t } = useTranslation('teams');
  const humans = useDirectoryHumans();
  const members = useMembers();
  const teams = useTeams();
  const currentUserId = useAppStore((s) => s.currentUserId);
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState<StatusFilter>('all');
  const [team, setTeam] = useState('all');

  const allRows = useMemo(() => {
    const byKey = new Map<string, HumanRow>();
    const ensure = (key: string): HumanRow => {
      let r = byKey.get(key);
      if (!r) {
        r = { key, name: '', identityRef: '' };
        byKey.set(key, r);
      }
      return r;
    };
    for (const m of members.data ?? []) {
      if (!isHumanMember(m)) continue;
      ensure(normalizeIdentityRef(m.identity_id)).member = m;
    }
    for (const d of humans.data ?? []) {
      ensure(normalizeIdentityRef(d.ref)).dir = d;
    }
    for (const r of byKey.values()) {
      r.name = r.member?.display_name || r.dir?.name || r.key;
      r.identityRef = r.member
        ? identityRefOf(r.member)
        : r.dir?.ref ?? `user:${r.key}`;
    }
    return [...byKey.values()];
  }, [members.data, humans.data]);

  // The invite/membership dimension used by the status chip + count: prefer the
  // directory invite state; a member-only row falls back to its membership flag
  // so it is not wrongly hidden by the Joined filter.
  const isJoined = (r: HumanRow): boolean =>
    r.dir ? r.dir.status === 'Joined' : r.member?.status === 'joined';

  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    return allRows.filter((r) => {
      if (status === 'joined' && !isJoined(r)) return false;
      // A by-team filter only matches rows the directory placed on that team.
      if (team !== 'all' && !(r.dir?.teams ?? []).includes(team)) return false;
      if (q) {
        const email = (r.member?.email || r.dir?.email || '').toLowerCase();
        if (!(r.name.toLowerCase().includes(q) || email.includes(q))) return false;
      }
      return true;
    });
  }, [allRows, query, status, team]);

  const joinedCount = allRows.filter(isJoined).length;
  const isLoading = humans.isLoading || members.isLoading;
  const isError = humans.isError || members.isError;
  const selfKey = currentUserId ? normalizeIdentityRef(currentUserId) : undefined;

  return (
    <section className="space-y-4" data-testid="page-TeamsDirectoryHumans">
      <header>
        <h1 className="font-heading text-2xl font-semibold text-text-primary">{t('humans.title')}</h1>
      </header>

      {/* Mobile (col② hidden <md): segmented Humans/Agents switch. */}
      <MembersSegmentControl active="humans" />

      <div className="flex flex-wrap items-center gap-3">
        <input
          className="w-72 rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
          placeholder={t('humans.searchPlaceholder')}
          value={query}
          data-testid="humans-search"
          onChange={(e) => setQuery(e.target.value)}
        />
        <div className="ml-auto flex flex-wrap gap-2">
          <FilterChip on={status === 'all'} testId="humans-filter-all" onClick={() => setStatus('all')}>
            {t('humans.filter.all', { count: allRows.length })}
          </FilterChip>
          <FilterChip on={status === 'joined'} testId="humans-filter-joined" onClick={() => setStatus('joined')}>
            {t('humans.filter.joined', { count: joinedCount })}
          </FilterChip>
          <select
            className="rounded border border-border-base bg-bg-elevated px-2.5 py-1.5 text-xs font-semibold text-text-secondary focus-visible:border-accent focus-visible:outline-none"
            value={team}
            data-testid="humans-team-filter"
            onChange={(e) => setTeam(e.target.value)}
          >
            <option value="all">{t('common.teamFilterAll')}</option>
            {(teams.data ?? []).map((tm) => (
              <option key={tm.id} value={tm.name}>
                {tm.name}
              </option>
            ))}
          </select>
        </div>
      </div>

      {isLoading && <Skeleton height="12rem" />}
      {isError && (
        <p className="text-sm text-danger" data-testid="humans-error">
          {t('common.loadFailed', { error: String(humans.error ?? members.error) })}
        </p>
      )}
      {!isLoading && !isError && rows.length === 0 && (
        <EmptyState title={t('humans.empty.title')} body={t('humans.empty.body')} testId="humans-empty" />
      )}

      {/* Mobile (<md): card rows — avatar (tap → DM) + name; tap row → UserDetail. */}
      {!isLoading && !isError && rows.length > 0 && (
        <ul className="space-y-2 md:hidden" data-testid="humans-cards">
          {rows.map((r) => (
            <HumanCard key={r.key} row={r} />
          ))}
        </ul>
      )}

      {/* Desktop (≥md): the full dual-dimension table. */}
      {!isLoading && !isError && rows.length > 0 && (
        <div className="hidden overflow-x-auto rounded-lg border border-border-base md:block">
          <table className="w-full min-w-[64rem] text-sm" data-testid="humans-table">
            <thead>
              <tr className="border-b border-border-base text-left text-[0.6875rem] uppercase tracking-wide text-text-muted">
                <th className="px-4 py-3 font-semibold">{t('humans.col.identity')}</th>
                <th className="px-4 py-3 font-semibold">{t('humans.col.orgRole')}</th>
                <th className="px-4 py-3 font-semibold">{t('humans.col.teamRole')}</th>
                <th className="px-4 py-3 font-semibold">{t('humans.col.membership')}</th>
                <th className="px-4 py-3 font-semibold">{t('humans.col.invite')}</th>
                <th className="px-4 py-3 font-semibold">{t('humans.col.teams')}</th>
                <th className="px-4 py-3 font-semibold">{t('humans.col.email')}</th>
                <th className="px-4 py-3 font-semibold">{t('humans.col.created')}</th>
                <th className="px-4 py-3 font-semibold">{t('humans.col.lastActive')}</th>
                <th className="px-4 py-3 font-semibold" />
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <HumanTableRow key={r.key} row={r} isSelf={!!selfKey && selfKey === r.key} />
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Note>{t('humans.note')}</Note>
    </section>
  );
}

function HumanTableRow({ row, isSelf }: { row: HumanRow; isSelf: boolean }): React.ReactElement {
  const { t: tm } = useTranslation('members');
  const { member, dir } = row;
  return (
    <tr data-testid={`human-row-${row.name}`} className="border-b border-border-base last:border-0 hover:bg-bg-subtle">
      <td className="px-4 py-3 font-semibold text-text-primary">
        <EntityRef
          id={row.key}
          name={member?.display_name ?? dir?.name}
          fallback={row.name}
          to={`/users/${encodeURIComponent(row.key)}`}
          testId="human-member-link"
        />
      </td>
      {/* Org permission role (member side). */}
      <td className="px-4 py-3">{member ? <RoleBadge role={member.role} /> : EM_DASH}</td>
      {/* Team-declared role (directory side, free string). */}
      <td className="px-4 py-3 font-semibold text-brand-hover">{dir?.role || EM_DASH}</td>
      {/* Membership status (member side): joined / disabled. */}
      <td className="px-4 py-3">
        {member ? (
          <span className={member.status === 'joined' ? 'text-success' : 'text-text-muted'} data-testid="human-membership">
            {member.status === 'joined' ? tm('humans.status.joined') : tm('humans.status.disabled')}
          </span>
        ) : (
          EM_DASH
        )}
      </td>
      {/* Invite status (directory side): Joined / Invited. */}
      <td className="px-4 py-3">
        {dir ? (
          <span className={dir.status === 'Joined' ? 'font-semibold text-success' : 'font-semibold text-text-muted'} data-testid="human-invite">
            {dir.status}
          </span>
        ) : (
          EM_DASH
        )}
      </td>
      <td className="px-4 py-3">{dir ? <TeamsCell teams={dir.teams} /> : EM_DASH}</td>
      <td className="px-4 py-3 font-mono text-xs text-text-muted" data-testid="human-email">
        {member?.email || dir?.email || '—'}
      </td>
      <td className="px-4 py-3 font-mono text-xs text-text-muted" data-testid="human-created">
        {member?.created_at ? fmtDate(member.created_at) : dir?.created || '—'}
      </td>
      <td className="px-4 py-3 font-mono text-xs text-text-muted" data-testid="human-last-session">
        {member?.last_session_at ? fmtDate(member.last_session_at) : dir?.last || '—'}
      </td>
      {/* Actions kebab requires a member-side row (member.id). A directory-only
          row cannot be role-changed / disabled, so it shows no kebab. */}
      <td className="px-4 py-3 text-right">
        {member && !isSelf && <MemberActionsMenu member={member} />}
      </td>
    </tr>
  );
}

function HumanCard({ row }: { row: HumanRow }): React.ReactElement {
  const { t: tm } = useTranslation('members');
  const openDm = useOpenDm();
  const subtitle = row.member
    ? tm(`humans.role.${row.member.role}`, { defaultValue: row.member.role })
    : row.dir?.role || '';
  return (
    <li
      className="flex items-center gap-3 rounded-lg border border-border-base bg-bg-elevated p-2"
      data-testid="human-member-card"
      data-identity={row.identityRef}
    >
      <button
        type="button"
        onClick={() => openDm.open(row.identityRef)}
        disabled={openDm.pending}
        aria-label={tm('humans.card.messageAria', { name: row.name })}
        data-testid="human-card-dm"
        className="flex min-h-[44px] min-w-[44px] items-center justify-center rounded-lg disabled:opacity-50"
      >
        <Avatar name={row.name} kind="human" size="md" />
      </button>
      <Link
        to={`/users/${encodeURIComponent(row.key)}`}
        className="flex min-h-[44px] min-w-0 flex-1 items-center"
        data-testid="human-card-link"
      >
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-medium text-text-primary">{row.name}</span>
          {subtitle && <span className="block truncate text-xs text-text-muted">{subtitle}</span>}
        </span>
      </Link>
    </li>
  );
}

// MemberActionsMenu — the `···` kebab ported verbatim (behavior) from the Members
// → Humans MemberRow: Change role (owner/admin/member via useChangeMemberRole),
// Disable / Re-enable behind a confirm popover, rendered in a fixed portal so the
// table cannot clip it. Reuses the `members` namespace strings (not duplicated).
function MemberActionsMenu({ member }: { member: MemberResult }): React.ReactElement {
  const { t } = useTranslation('members');
  const [menuOpen, setMenuOpen] = useState(false);
  const [confirmAction, setConfirmAction] = useState<null | 'disable' | 'reenable'>(null);
  const [rolePickerOpen, setRolePickerOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const [popoverPos, setPopoverPos] = useState<{
    right: number;
    top?: number;
    bottom?: number;
    maxHeight: number;
  } | null>(null);
  const changeRole = useChangeMemberRole();
  const disable = useDisableMember();
  const reEnable = useReEnableMember();
  const popoverOpen = menuOpen || rolePickerOpen || confirmAction !== null;

  const closePopovers = useCallback(() => {
    setMenuOpen(false);
    setRolePickerOpen(false);
    setConfirmAction(null);
  }, []);

  const computePopoverPos = useCallback(() => {
    const el = triggerRef.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const margin = 4;
    const viewportPad = 8;
    const spaceBelow = window.innerHeight - r.bottom - viewportPad;
    const spaceAbove = r.top - viewportPad;
    const below = spaceBelow >= 160 || spaceBelow >= spaceAbove;
    setPopoverPos({
      right: Math.max(viewportPad, window.innerWidth - r.right),
      maxHeight: Math.max(120, (below ? spaceBelow : spaceAbove) - margin),
      ...(below ? { top: r.bottom + margin } : { bottom: window.innerHeight - r.top + margin }),
    });
  }, []);

  useLayoutEffect(() => {
    if (!popoverOpen) return;
    computePopoverPos();
    const onMove = () => computePopoverPos();
    window.addEventListener('scroll', onMove, true);
    window.addEventListener('resize', onMove);
    return () => {
      window.removeEventListener('scroll', onMove, true);
      window.removeEventListener('resize', onMove);
    };
  }, [popoverOpen, computePopoverPos]);

  useEffect(() => {
    if (!popoverOpen) return;
    const onPointerDown = (e: MouseEvent) => {
      const target = e.target as Node;
      if (!rootRef.current?.contains(target) && !popoverRef.current?.contains(target)) closePopovers();
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closePopovers();
    };
    document.addEventListener('mousedown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('mousedown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [popoverOpen, closePopovers]);

  return (
    <div className="relative inline-block" ref={rootRef}>
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setMenuOpen((v) => !v)}
        className="rounded px-2 py-1 text-sm text-text-muted hover:bg-bg-subtle"
        aria-label={t('humans.actions.menuLabel')}
      >
        ···
      </button>
      {popoverOpen && popoverPos && createPortal(
        <div
          ref={popoverRef}
          className="fixed z-50 overflow-y-auto rounded-md border border-border-base bg-bg-elevated shadow-[var(--shadow-2)]"
          style={{
            right: popoverPos.right,
            maxHeight: Math.min(popoverPos.maxHeight, 384),
            ...(popoverPos.top !== undefined ? { top: popoverPos.top } : { bottom: popoverPos.bottom }),
          }}
          data-testid="member-actions-popover"
        >
          {menuOpen && (
            <div className="w-36" role="menu">
              <button
                type="button"
                role="menuitem"
                onClick={() => { setMenuOpen(false); setRolePickerOpen(true); }}
                className="flex w-full px-3 py-2 text-sm text-text-primary hover:bg-bg-subtle"
              >
                {t('humans.actions.changeRole')}
              </button>
              {member.status === 'joined' ? (
                <button
                  type="button"
                  role="menuitem"
                  onClick={() => { setMenuOpen(false); setConfirmAction('disable'); }}
                  className="flex w-full px-3 py-2 text-sm text-danger hover:bg-bg-subtle"
                >
                  {t('humans.actions.disable')}
                </button>
              ) : (
                <button
                  type="button"
                  role="menuitem"
                  onClick={() => { setMenuOpen(false); setConfirmAction('reenable'); }}
                  className="flex w-full px-3 py-2 text-sm text-success hover:bg-bg-subtle"
                >
                  {t('humans.actions.reEnable')}
                </button>
              )}
            </div>
          )}
          {rolePickerOpen && (
            <div className="w-36">
              {(['owner', 'admin', 'member'] as const).map((r) => (
                <button
                  key={r}
                  type="button"
                  onClick={() => {
                    changeRole.mutate({ id: member.id, role: r });
                    setRolePickerOpen(false);
                  }}
                  className={`flex w-full px-3 py-2 text-sm hover:bg-bg-subtle ${
                    member.role === r ? 'font-semibold text-brand' : 'text-text-primary'
                  }`}
                >
                  {t(`humans.role.${r}`, { defaultValue: r })}
                </button>
              ))}
              <button
                type="button"
                onClick={() => setRolePickerOpen(false)}
                className="flex w-full px-3 py-2 text-xs text-text-muted hover:bg-bg-subtle"
              >
                {t('humans.actions.cancel')}
              </button>
            </div>
          )}
          {confirmAction && (
            <div className="w-48 p-3">
              <p className="text-sm text-text-primary mb-2">
                {confirmAction === 'disable' ? t('humans.confirm.disable') : t('humans.confirm.reEnable')}
              </p>
              <div className="flex gap-2 justify-end">
                <button
                  type="button"
                  onClick={() => {
                    disable.reset();
                    reEnable.reset();
                    setConfirmAction(null);
                  }}
                  className="rounded px-2 py-1 text-xs text-text-secondary hover:bg-bg-subtle"
                >
                  {t('humans.actions.cancel')}
                </button>
                <button
                  type="button"
                  disabled={disable.isPending || reEnable.isPending}
                  onClick={() => {
                    // Never fail silently: pass a non-empty reason (the backend
                    // rejects an empty reason with HTTP 500) and only close the
                    // popover once the mutation SUCCEEDS — on error we keep it open
                    // and render the message below.
                    if (confirmAction === 'disable') {
                      disable.mutate(
                        { id: member.id, reason: t('humans.disable.reason') },
                        { onSuccess: () => setConfirmAction(null) },
                      );
                    } else {
                      reEnable.mutate(member.id, { onSuccess: () => setConfirmAction(null) });
                    }
                  }}
                  className={`rounded px-2 py-1 text-xs text-white disabled:opacity-50 ${
                    confirmAction === 'disable' ? 'bg-danger' : 'bg-success'
                  }`}
                >
                  {t('humans.actions.confirm')}
                </button>
              </div>
              {confirmAction === 'disable' && disable.isError && (
                <p className="mt-2 text-xs text-danger" data-testid="member-disable-error" role="alert">
                  {t('humans.disable.error')}
                </p>
              )}
              {confirmAction === 'reenable' && reEnable.isError && (
                <p className="mt-2 text-xs text-danger" data-testid="member-reenable-error" role="alert">
                  {t('humans.disable.reEnableError')}
                </p>
              )}
            </div>
          )}
        </div>,
        document.body,
      )}
    </div>
  );
}

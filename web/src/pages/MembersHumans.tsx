import React, { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import {
  useMembers,
  useChangeMemberRole,
  useDisableMember,
  useReEnableMember,
  normalizeIdentityRef,
  type MemberResult,
} from '@/api/members';
import { Link } from 'react-router-dom';
import { EntityRef } from '@/components/EntityRef';
import { Avatar } from '@/components/Avatar';
import { MembersSegmentControl } from '@/components/MembersSegmentControl';
import { useOpenDm } from '@/components/useOpenDm';

// v2.7.1 #193: short date for the Humans list columns (empty → em dash).
function fmtDate(v?: string): string {
  if (!v) return '—';
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleDateString();
}

function RoleBadge({ role }: { role: string }): React.ReactElement {
  const colors: Record<string, string> = {
    owner: 'bg-brand/10 text-brand',
    admin: 'bg-accent/10 text-accent',
    member: 'bg-bg-subtle text-text-secondary',
  };
  return (
    <span className={`rounded px-1.5 py-0.5 text-xs font-medium ${colors[role] ?? colors.member}`}>
      {role}
    </span>
  );
}

function MemberRow({
  member,
  currentIdentityId,
}: {
  member: MemberResult;
  currentIdentityId?: string;
}): React.ReactElement {
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
  const isSelf = member.identity_id === currentIdentityId;
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
    <tr className="border-b border-border-base last:border-0">
      {/* v2.7 #192: display name, raw id on hover. v2.7.1 #193: links to UserDetail
          by member-id (rename-safe). */}
      <td className="py-2 px-3 text-sm text-text-primary">
        <EntityRef
          id={member.identity_id}
          name={member.display_name}
          fallback={normalizeIdentityRef(member.identity_id)}
          to={`/users/${encodeURIComponent(normalizeIdentityRef(member.identity_id))}`}
          testId="human-member-link"
        />
      </td>
      <td className="py-2 px-3">
        <RoleBadge role={member.role} />
      </td>
      <td className="py-2 px-3 text-sm">
        <span
          className={member.status === 'joined' ? 'text-success' : 'text-text-muted'}
        >
          {member.status === 'joined' ? 'Joined' : 'Disabled'}
        </span>
      </td>
      {/* v2.7.1 #193: email / created / last-active columns (nullable → em dash). */}
      <td className="py-2 px-3 text-sm text-text-secondary" data-testid="human-email">
        {member.email || '—'}
      </td>
      <td className="py-2 px-3 text-sm text-text-muted" data-testid="human-created">
        {fmtDate(member.created_at)}
      </td>
      <td className="py-2 px-3 text-sm text-text-muted" data-testid="human-last-session">
        {fmtDate(member.last_session_at)}
      </td>
      <td className="py-2 px-3 text-right">
        {!isSelf && (
          <div className="relative inline-block" ref={rootRef}>
            <button
              ref={triggerRef}
              type="button"
              onClick={() => setMenuOpen((v) => !v)}
              className="rounded px-2 py-1 text-sm text-text-muted hover:bg-bg-subtle"
              aria-label="Member actions"
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
                      Change role
                    </button>
                    {member.status === 'joined' ? (
                      <button
                        type="button"
                        role="menuitem"
                        onClick={() => { setMenuOpen(false); setConfirmAction('disable'); }}
                        className="flex w-full px-3 py-2 text-sm text-danger hover:bg-bg-subtle"
                      >
                        Disable
                      </button>
                    ) : (
                      <button
                        type="button"
                        role="menuitem"
                        onClick={() => { setMenuOpen(false); setConfirmAction('reenable'); }}
                        className="flex w-full px-3 py-2 text-sm text-success hover:bg-bg-subtle"
                      >
                        Re-enable
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
                        {r}
                      </button>
                    ))}
                    <button
                      type="button"
                      onClick={() => setRolePickerOpen(false)}
                      className="flex w-full px-3 py-2 text-xs text-text-muted hover:bg-bg-subtle"
                    >
                      Cancel
                    </button>
                  </div>
                )}
                {confirmAction && (
                  <div className="w-48 p-3">
                    <p className="text-sm text-text-primary mb-2">
                      {confirmAction === 'disable' ? 'Disable this member?' : 'Re-enable this member?'}
                    </p>
                    <div className="flex gap-2 justify-end">
                      <button
                        type="button"
                        onClick={() => setConfirmAction(null)}
                        className="rounded px-2 py-1 text-xs text-text-secondary hover:bg-bg-subtle"
                      >
                        Cancel
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          if (confirmAction === 'disable') disable.mutate({ id: member.id });
                          else reEnable.mutate(member.id);
                          setConfirmAction(null);
                        }}
                        className={`rounded px-2 py-1 text-xs text-white ${
                          confirmAction === 'disable' ? 'bg-danger' : 'bg-success'
                        }`}
                      >
                        Confirm
                      </button>
                    </div>
                  </div>
                )}
              </div>,
              document.body,
            )}
          </div>
        )}
      </td>
    </tr>
  );
}

export default function MembersHumans(): React.ReactElement {
  const members = useMembers();
  const openDm = useOpenDm();

  // Use `kind` field from v2.6 member response; fall back to identity_id prefix for compatibility.
  const humanMembers = (members.data ?? []).filter(
    (m) => m.kind === 'user' || m.identity_id.startsWith('user-') || m.identity_id.startsWith('user:'),
  );

  return (
    <section className="space-y-4" data-testid="page-MembersHumans">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-text-primary">Humans</h1>
      </div>

      {/* Mobile (col② is hidden <md): segmented Humans/Agents switch. */}
      <MembersSegmentControl active="humans" />

      {members.isLoading && <p className="text-sm text-text-muted">Loading…</p>}
      {members.isError && (
        <p className="text-sm text-danger">Failed to load: {String(members.error)}</p>
      )}

      {!members.isLoading && humanMembers.length === 0 && (
        <p className="text-sm text-text-muted">No human members yet</p>
      )}

      {/* Mobile (<md): card rows — avatar (tap → DM) + name + role; tap row → UserDetail. */}
      {humanMembers.length > 0 && (
        <ul className="space-y-2 md:hidden" data-testid="members-humans-cards">
          {humanMembers.map((m) => {
            const name = m.display_name || normalizeIdentityRef(m.identity_id);
            return (
              <li
                key={m.id}
                className="flex items-center gap-3 rounded-lg border border-border bg-bg-elevated p-2"
                data-testid="human-member-card"
                data-identity={m.identity_id}
              >
                <button
                  type="button"
                  onClick={() => openDm.open(m.identity_id)}
                  disabled={openDm.pending}
                  aria-label={`Message ${name}`}
                  data-testid="human-card-dm"
                  className="flex min-h-[44px] min-w-[44px] items-center justify-center rounded-lg disabled:opacity-50"
                >
                  <Avatar name={name} kind="human" size="md" />
                </button>
                <Link
                  to={`/users/${encodeURIComponent(normalizeIdentityRef(m.identity_id))}`}
                  className="flex min-h-[44px] min-w-0 flex-1 items-center"
                  data-testid="human-card-link"
                >
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium text-text-primary">{name}</span>
                    <span className="block truncate text-xs text-text-muted">{m.role}</span>
                  </span>
                </Link>
              </li>
            );
          })}
        </ul>
      )}

      {/* Desktop (≥md): the full table. */}
      {humanMembers.length > 0 && (
        <div className="hidden overflow-x-auto md:block">
          <table className="w-full text-left">
            <thead>
              <tr className="border-b border-border">
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">Identity</th>
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">Role</th>
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">Status</th>
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">Email</th>
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">Created</th>
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">Last active</th>
                <th className="py-2 px-3 w-12" />
              </tr>
            </thead>
            <tbody>
              {humanMembers.map((m) => (
                <MemberRow key={m.id} member={m} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

import React, { useState } from 'react';
import {
  useMembers,
  useAddMember,
  useChangeMemberRole,
  useDisableMember,
  useReEnableMember,
  normalizeIdentityRef,
  type MemberResult,
} from '@/api/members';
import { Link } from 'react-router-dom';
import { ApiError } from '@/api/client';
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
  const changeRole = useChangeMemberRole();
  const disable = useDisableMember();
  const reEnable = useReEnableMember();
  const isSelf = member.identity_id === currentIdentityId;

  return (
    <tr className="border-b border-border last:border-0">
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
          <div className="relative inline-block">
            <button
              type="button"
              onClick={() => setMenuOpen((v) => !v)}
              className="rounded px-2 py-1 text-sm text-text-muted hover:bg-bg-subtle"
              aria-label="Member actions"
            >
              ···
            </button>
            {menuOpen && (
              <div
                className="absolute right-0 top-full z-20 mt-1 w-36 rounded-md border border-border bg-bg-elevated shadow-[var(--shadow-2)]"
                role="menu"
              >
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
          </div>
        )}
        {/* Role picker inline */}
        {rolePickerOpen && (
          <div className="absolute right-0 z-20 mt-1 w-36 rounded-md border border-border bg-bg-elevated shadow-[var(--shadow-2)]">
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
        {/* Confirm dialog */}
        {confirmAction && (
          <div className="absolute right-0 z-20 mt-1 w-48 rounded-md border border-border bg-bg-elevated p-3 shadow-[var(--shadow-2)]">
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
      </td>
    </tr>
  );
}

function AddUserModal({ onClose }: { onClose: () => void }): React.ReactElement {
  const [displayName, setDisplayName] = useState('');
  const [role, setRole] = useState('member');
  const [error, setError] = useState('');
  const [tempPasscode, setTempPasscode] = useState('');
  const [createdName, setCreatedName] = useState('');
  const add = useAddMember();

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    add.mutate(
      { display_name: displayName.trim(), role },
      {
        onSuccess: (res) => {
          if (res.temp_passcode) {
            setTempPasscode(res.temp_passcode);
            setCreatedName(res.display_name ?? displayName.trim());
          } else {
            onClose();
          }
        },
        onError: (err) => {
          if (err instanceof ApiError) setError(err.message);
          else setError('Failed to add user');
        },
      },
    );
  };

  // Success view — show temp passcode (once).
  if (tempPasscode) {
    return (
      <div
        className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
        role="dialog"
        aria-modal="true"
      >
        <div className="w-full max-w-sm rounded-xl bg-bg-elevated border border-border p-6 shadow-[var(--shadow-3)]">
          <h2 className="text-base font-semibold text-text-primary mb-2">User created</h2>
          <p className="text-sm text-text-secondary mb-3">
            Temporary passcode for <strong>{createdName}</strong> (shown once — hand it over now):
          </p>
          <div className="rounded bg-bg-subtle border border-border-strong px-3 py-3 mb-4 text-center">
            <code className="text-2xl font-mono tracking-widest text-text-primary">{tempPasscode}</code>
          </div>
          <p className="text-xs text-text-muted mb-4">
            The user should change their password at /me right after first sign-in. This passcode cannot be viewed again after closing this window.
          </p>
          <div className="flex justify-end">
            <button
              type="button"
              onClick={onClose}
              className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
            >
              I've saved it, close
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
      role="dialog"
      aria-modal="true"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div className="w-full max-w-sm rounded-xl bg-bg-elevated border border-border p-6 shadow-[var(--shadow-3)]">
        <h2 className="text-base font-semibold text-text-primary mb-4">Add user</h2>
        <p className="text-xs text-text-muted mb-3">A new user identity will be created with a temporary passcode.</p>
        {error && (
          <div role="alert" className="mb-3 rounded bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
            {error}
          </div>
        )}
        <form onSubmit={handleSubmit} noValidate className="space-y-3">
          <div className="space-y-1">
            <label htmlFor="add-user-name" className="block text-sm text-text-primary">Display name</label>
            <input
              id="add-user-name"
              type="text"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder="The user's display name"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="add-user-role" className="block text-sm text-text-primary">Role</label>
            <select
              id="add-user-role"
              value={role}
              onChange={(e) => setRole(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary"
            >
              <option value="member">member</option>
              <option value="admin">admin</option>
              <option value="owner">owner</option>
            </select>
          </div>
          <div className="flex gap-2 justify-end pt-1">
            <button type="button" onClick={onClose} className="rounded px-4 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle">Cancel</button>
            <button
              type="submit"
              disabled={add.isPending || !displayName.trim()}
              className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
            >
              {add.isPending ? 'Creating…' : 'Create user'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

export default function MembersHumans(): React.ReactElement {
  const members = useMembers();
  const openDm = useOpenDm();
  const [addModalOpen, setAddModalOpen] = useState(false);

  // Use `kind` field from v2.6 member response; fall back to identity_id prefix for compatibility.
  const humanMembers = (members.data ?? []).filter(
    (m) => m.kind === 'user' || m.identity_id.startsWith('user-') || m.identity_id.startsWith('user:'),
  );

  return (
    <section className="space-y-4" data-testid="page-MembersHumans">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-text-primary">Humans</h1>
        <button
          type="button"
          onClick={() => setAddModalOpen(true)}
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
        >
          Add user
        </button>
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

      {addModalOpen && <AddUserModal onClose={() => setAddModalOpen(false)} />}
    </section>
  );
}

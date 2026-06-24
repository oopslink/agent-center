import type React from 'react';
import { useEffect, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { ApiError } from '@/api/client';
import { orgApi, useOrgs } from '@/api/auth';
import {
  useMembers,
  useChangeMemberRole,
  useDisableMember,
  useReEnableMember,
  useDropMember,
  normalizeIdentityRef,
  type MemberResult,
} from '@/api/members';
import { invitationAcceptUrl, useCancelInvitation, useCreateInvitation, useInvitations } from '@/api/invitations';
import { useAgents, useBatchAgentLifecycle, type AgentBatchAction } from '@/api/agents';
import { AgentCreateModal } from '@/components/AgentCreateModal';
import { EmptyState } from '@/components/EmptyState';
import { useOptionalOrgContext } from '@/OrgContext';
import { formatLocalTime } from '@/utils/time';

type Section = 'profile' | 'humans' | 'agents' | 'invitations' | 'danger';

const navItems: Array<{ id: Section; label: string }> = [
  { id: 'profile', label: 'Profile' },
  { id: 'humans', label: 'Humans' },
  { id: 'agents', label: 'Agents' },
  { id: 'invitations', label: 'Invitations' },
  { id: 'danger', label: 'Danger Zone' },
];

export default function OrganizationSettings(): React.ReactElement {
  const [section, setSection] = useState<Section>('profile');
  return (
    <section className="min-h-full bg-bg-subtle px-4 py-5 text-text-primary md:px-6">
      <div className="mx-auto flex max-w-6xl flex-col gap-4 md:flex-row">
        <nav className="rounded border border-border-base bg-bg-elevated p-2 md:w-56 md:shrink-0">
          {navItems.map((item) => (
            <button
              key={item.id}
              type="button"
              onClick={() => setSection(item.id)}
              className={[
                'block w-full rounded px-3 py-2 text-left text-sm',
                section === item.id
                  ? 'bg-bg-subtle font-semibold text-text-primary'
                  : 'text-text-secondary hover:bg-bg-subtle',
              ].join(' ')}
            >
              {item.label}
            </button>
          ))}
        </nav>
        <div className="min-w-0 flex-1 rounded border border-border-base bg-bg-elevated p-5 shadow-[var(--shadow-1)]">
          {section === 'profile' && <ProfileSection />}
          {section === 'humans' && <HumansSection onOpenInvitations={() => setSection('invitations')} />}
          {section === 'agents' && <AgentsSection />}
          {section === 'invitations' && <InvitationsSection />}
          {section === 'danger' && <DangerSection />}
        </div>
      </div>
    </section>
  );
}

function ProfileSection(): React.ReactElement {
  const orgCtx = useOptionalOrgContext();
  const orgs = useOrgs();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const org = (orgs.data ?? []).find((o) => o.slug === orgCtx?.slug);
  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [description, setDescription] = useState('');
  const [slugEditable, setSlugEditable] = useState(false);
  const update = useMutation({
    mutationFn: async () => {
      if (!org) return;
      await orgApi.update(org.id, {
        name: name.trim(),
        slug: slugEditable ? slug.trim() : undefined,
        description: description.trim(),
      });
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['orgs'] });
      if (slugEditable && slug.trim() && slug.trim() !== org?.slug) {
        navigate(`/organizations/${slug.trim()}/organization-settings`, { replace: true });
      }
    },
  });

  useEffect(() => {
    if (!org) return;
    setName(org.name);
    setSlug(org.slug);
    setDescription(org.description ?? '');
  }, [org?.id]);

  return (
    <div className="max-w-2xl space-y-4">
      <h1 className="text-xl font-semibold">Organization Profile</h1>
      <Field label="Organization Name">
        <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} />
      </Field>
      <Field label="Organization Slug">
        <div className="flex gap-2">
          <input
            className={inputClass}
            value={slug}
            disabled={!slugEditable}
            onChange={(e) => setSlug(e.target.value)}
          />
          <button
            type="button"
            onClick={() => setSlugEditable((v) => !v)}
            className="rounded border border-border-base px-3 text-sm hover:bg-bg-subtle"
          >
            Change
          </button>
        </div>
        <p className="mt-1 text-xs text-amber-700">Modifying slug affects URLs, caches, and SSE scopes</p>
      </Field>
      <Field label="Description">
        <textarea className={inputClass} rows={4} value={description} onChange={(e) => setDescription(e.target.value)} />
      </Field>
      <Field label="Logo / Avatar">
        <div className="flex h-28 items-center justify-center rounded border border-dashed border-border-strong text-sm text-text-muted">
          Upload logo
        </div>
      </Field>
      {update.isError && <p className="text-sm text-danger">{(update.error as Error).message}</p>}
      <button
        type="button"
        disabled={!org || update.isPending || !name.trim()}
        onClick={() => update.mutate()}
        className="rounded bg-brand px-4 py-2 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
      >
        {update.isPending ? 'Saving...' : 'Save Changes'}
      </button>
    </div>
  );
}

function HumansSection({ onOpenInvitations }: { onOpenInvitations: () => void }): React.ReactElement {
  const members = useMembers();
  const changeRole = useChangeMemberRole();
  const disable = useDisableMember();
  const reEnable = useReEnableMember();
  const drop = useDropMember();
  const humans = (members.data ?? []).filter((m) => m.kind === 'user' || m.identity_id.startsWith('user-'));
  const dropMember = (m: MemberResult): void => {
    if (!window.confirm(`Drop ${m.display_name ?? m.identity_id} from this organization?`)) return;
    drop.mutate(m.id);
  };
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-xl font-semibold">Humans</h1>
        <button type="button" onClick={onOpenInvitations} className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover">
          Invite
        </button>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[48rem] text-left text-sm">
          <thead className="text-xs uppercase text-text-muted">
            <tr className="border-b border-border-base">
              <th className="px-3 py-2">Name</th>
              <th className="px-3 py-2">Role</th>
              <th className="px-3 py-2">Joined</th>
              <th className="px-3 py-2">Status</th>
              <th className="px-3 py-2 text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {humans.map((m) => (
              <tr key={m.id} className="border-b border-border-base last:border-0">
                <td className="px-3 py-2 font-medium">{m.display_name ?? normalizeIdentityRef(m.identity_id)}</td>
                <td className="px-3 py-2">
                  <select
                    value={m.role}
                    onChange={(e) => changeRole.mutate({ id: m.id, role: e.target.value })}
                    className="rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm"
                  >
                    <option value="owner">owner</option>
                    <option value="admin">admin</option>
                    <option value="member">member</option>
                  </select>
                </td>
                <td className="px-3 py-2 text-text-secondary">{formatDate(m.joined_at)}</td>
                <td className="px-3 py-2">
                  <StatusPill status={m.status === 'joined' ? 'active' : 'disabled'} />
                </td>
                <td className="px-3 py-2 text-right">
                  {m.status === 'joined' ? (
                    <button type="button" onClick={() => disable.mutate({ id: m.id })} className="mr-3 text-danger hover:underline">Disable</button>
                  ) : (
                    <button type="button" onClick={() => reEnable.mutate(m.id)} className="mr-3 text-success hover:underline">Re-enable</button>
                  )}
                  <button type="button" onClick={() => dropMember(m)} className="text-danger hover:underline">Drop</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {humans.length === 0 && <EmptyState title="No humans" body="Invite self-registered users to join this organization." />}
      <div className="rounded border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900">
        Disable = keeps member identity & history but marks inactive. Drop = removes from org membership.
      </div>
    </div>
  );
}

function AgentsSection(): React.ReactElement {
  const agents = useAgents();
  const [createOpen, setCreateOpen] = useState(false);
  const batch = useBatchAgentLifecycle();
  const run = (ids: string[], action: AgentBatchAction): void => {
    void batch.run(ids, action);
  };
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-xl font-semibold">Agents</h1>
        <button type="button" onClick={() => setCreateOpen(true)} className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover">
          Create Agent
        </button>
      </div>
      {createOpen && <AgentCreateModal onClose={() => setCreateOpen(false)} />}
      <div className="overflow-x-auto">
        <table className="w-full min-w-[42rem] text-left text-sm">
          <thead className="text-xs uppercase text-text-muted">
            <tr className="border-b border-border-base">
              <th className="px-3 py-2">Name</th>
              <th className="px-3 py-2">Lifecycle</th>
              <th className="px-3 py-2">Availability</th>
              <th className="px-3 py-2">Worker</th>
              <th className="px-3 py-2 text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {(agents.data ?? []).map((a) => (
              <tr key={a.id} className="border-b border-border-base last:border-0">
                <td className="px-3 py-2 font-medium">{a.name}</td>
                <td className="px-3 py-2">{a.lifecycle}</td>
                <td className="px-3 py-2">{a.availability}</td>
                <td className="px-3 py-2 text-text-secondary">{a.worker_id || 'Not bound'}</td>
                <td className="px-3 py-2 text-right">
                  <button type="button" onClick={() => run([a.id], 'start')} className="mr-3 text-accent hover:underline">Start</button>
                  <button type="button" onClick={() => run([a.id], 'stop')} className="mr-3 text-accent hover:underline">Stop</button>
                  <button type="button" onClick={() => run([a.id], 'restart')} className="mr-3 text-accent hover:underline">Restart</button>
                  <button type="button" onClick={() => run([a.id], 'reset')} className="text-danger hover:underline">Reset</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {agents.isSuccess && agents.data.length === 0 && <EmptyState title="No agents" body="Create an agent and bind it to a worker." />}
    </div>
  );
}

function InvitationsSection(): React.ReactElement {
  const invitations = useInvitations();
  const [open, setOpen] = useState(false);
  const cancel = useCancelInvitation();
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-xl font-semibold">Invitations</h1>
        <button type="button" onClick={() => setOpen(true)} className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover">
          New Invitation
        </button>
      </div>
      {open && <InvitationModal onClose={() => setOpen(false)} />}
      <div className="space-y-3">
        {(invitations.data ?? []).map((inv) => {
          const url = invitationAcceptUrl(inv.token);
          return (
            <div key={inv.id} className="rounded border border-border-base p-4">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <div className="font-medium">{inv.invitee_display_name ?? inv.invitee_user_id}</div>
                  <div className="mt-1 text-sm text-text-secondary">Role: {inv.role} · Created by {inv.invited_by_display_name ?? inv.invited_by_identity_id}</div>
                  <div className="mt-1 text-xs text-text-muted">Expires {formatDate(inv.expires_at)}</div>
                </div>
                <StatusPill status={inv.status} />
              </div>
              <div className="mt-3 flex gap-2">
                <input readOnly value={url} className={`${inputClass} font-mono text-xs`} />
                <button type="button" onClick={() => void navigator.clipboard?.writeText(url)} className="rounded border border-border-base px-3 text-sm hover:bg-bg-subtle">
                  Copy
                </button>
              </div>
              <div className="mt-2 flex justify-between text-xs text-text-muted">
                <span>{inv.accepted_at ? `Accepted ${formatDate(inv.accepted_at)}` : ' '}</span>
                {inv.status === 'pending' && (
                  <button type="button" onClick={() => cancel.mutate(inv.id)} className="text-danger hover:underline">Cancel</button>
                )}
              </div>
            </div>
          );
        })}
      </div>
      {invitations.isSuccess && invitations.data.length === 0 && <EmptyState title="No invitations" body="Invite existing users by their user identity id." />}
    </div>
  );
}

function InvitationModal({ onClose }: { onClose: () => void }): React.ReactElement {
  const [inviteeUserId, setInviteeUserId] = useState('');
  const [role, setRole] = useState('member');
  const create = useCreateInvitation();
  const submit = async (e: React.FormEvent): Promise<void> => {
    e.preventDefault();
    try {
      await create.mutateAsync({ invitee_user_id: inviteeUserId.trim(), role });
      onClose();
    } catch {
      // surfaced below
    }
  };
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" role="dialog" aria-modal="true">
      <form onSubmit={submit} className="w-full max-w-md rounded bg-bg-elevated p-5 shadow-[var(--shadow-3)]">
        <h2 className="mb-4 text-lg font-semibold">New Invitation</h2>
        <Field label="Invitee user id">
          <input className={inputClass} value={inviteeUserId} onChange={(e) => setInviteeUserId(e.target.value)} placeholder="user-..." />
        </Field>
        <Field label="Role">
          <select className={inputClass} value={role} onChange={(e) => setRole(e.target.value)}>
            <option value="member">member</option>
            <option value="admin">admin</option>
            <option value="owner">owner</option>
          </select>
        </Field>
        {create.isError && <p className="mb-3 text-sm text-danger">{create.error instanceof ApiError ? create.error.message : 'Create invitation failed'}</p>}
        <div className="flex justify-end gap-2">
          <button type="button" onClick={onClose} className="rounded px-3 py-1.5 text-sm hover:bg-bg-subtle">Cancel</button>
          <button type="submit" disabled={!inviteeUserId.trim() || create.isPending} className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50">
            {create.isPending ? 'Creating...' : 'Create'}
          </button>
        </div>
      </form>
    </div>
  );
}

function DangerSection(): React.ReactElement {
  const orgCtx = useOptionalOrgContext();
  const orgs = useOrgs();
  const org = (orgs.data ?? []).find((o) => o.slug === orgCtx?.slug);
  const del = useMutation({
    mutationFn: () => (org ? orgApi.delete(org.id) : Promise.resolve()),
    onSuccess: () => {
      window.location.href = '/';
    },
  });
  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold text-danger">Danger Zone</h1>
      <DangerCard title="Disable Organization" body="Members cannot access this organization, but data is preserved." button="Disable Org" onClick={() => {
        if (window.confirm(`Disable organization ${org?.name ?? ''}?`)) del.mutate();
      }} />
      <DangerCard title="Force-Delete Organization" body="Permanently delete this organization and all associated data. This action cannot be undone." button="Force Delete" disabled />
      <DangerCard title="Audit Log" body="View all organization changes, member actions, and system events." button="View Audit Log" disabled />
    </div>
  );
}

function DangerCard({ title, body, button, disabled, onClick }: { title: string; body: string; button: string; disabled?: boolean; onClick?: () => void }): React.ReactElement {
  return (
    <div className="rounded border border-danger/30 bg-danger/5 p-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="font-semibold text-danger">{title}</h2>
          <p className="mt-1 text-sm text-text-secondary">{body}</p>
        </div>
        <button type="button" disabled={disabled} onClick={onClick} className="rounded bg-danger px-3 py-1.5 text-sm font-medium text-white hover:brightness-95 disabled:cursor-not-allowed disabled:opacity-45">
          {button}
        </button>
      </div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }): React.ReactElement {
  return (
    <label className="block">
      <span className="mb-1 block text-sm font-medium">{label}</span>
      {children}
    </label>
  );
}

function StatusPill({ status }: { status: string }): React.ReactElement {
  const cls = status === 'active' || status === 'accepted'
    ? 'bg-success/10 text-success'
    : status === 'pending'
      ? 'bg-amber-100 text-amber-800'
      : 'bg-bg-subtle text-text-muted';
  return <span className={`inline-flex rounded px-2 py-0.5 text-xs font-medium ${cls}`}>{status}</span>;
}

function formatDate(v?: string): string {
  if (!v) return '-';
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return '-';
  return formatLocalTime(v);
}

const inputClass =
  'w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary outline-none focus:border-accent';

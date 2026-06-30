import type React from 'react';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Navigate, useNavigate, useParams } from 'react-router-dom';
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
import {
  invitationAcceptUrl,
  useCancelInvitation,
  useCreateInvitation,
  useDeleteInvitation,
  useInvitations,
  type InvitationResult,
} from '@/api/invitations';
import { ConfirmModal } from '@/components/ConfirmModal';
import { EmptyState } from '@/components/EmptyState';
import { useOptionalOrgContext } from '@/OrgContext';
import { formatLocalTime } from '@/utils/time';
import Agents from './Agents';

type Section = 'profile' | 'humans' | 'agents' | 'invitations' | 'danger';

const SECTIONS: ReadonlyArray<Section> = ['profile', 'humans', 'agents', 'invitations', 'danger'];

// I41 (T470): the 5 sections now live in the shell's col② secondary nav
// (OrgSettingsSecondaryNav), reached via routed sub-paths
// organization-settings/<section>. This page renders ONLY the selected
// section's panel in col③ — no page-internal nav (the rejected card/tab form).
export default function OrganizationSettings(): React.ReactElement {
  const navigate = useNavigate();
  const params = useParams<{ section?: string }>();
  const section = params.section as Section | undefined;
  // Unknown/missing section → canonical Profile (the index route also redirects,
  // but a hand-typed bad section lands here).
  if (!section || !SECTIONS.includes(section)) {
    return <Navigate to="profile" replace />;
  }
  // Block body discards navigate()'s `void | Promise<void>` return so the
  // arrow's `: void` annotation type-checks under `tsc -b` (the build gate).
  const goToInvitations = (): void => {
    navigate('../invitations');
  };
  return (
    <section className="min-h-full text-text-primary">
      {section === 'profile' && <ProfileSection />}
      {section === 'humans' && <HumansSection onOpenInvitations={goToInvitations} />}
      {section === 'agents' && <Agents />}
      {section === 'invitations' && <InvitationsSection />}
      {section === 'danger' && <DangerSection />}
    </section>
  );
}

function ProfileSection(): React.ReactElement {
  const { t } = useTranslation('admin');
  const orgCtx = useOptionalOrgContext();
  const orgs = useOrgs();
  const qc = useQueryClient();
  const org = (orgs.data ?? []).find((o) => o.slug === orgCtx?.slug);
  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [description, setDescription] = useState('');
  const update = useMutation({
    mutationFn: async () => {
      if (!org) return;
      await orgApi.update(org.id, {
        name: name.trim(),
        description: description.trim(),
      });
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['orgs'] });
    },
  });

  useEffect(() => {
    if (!org) return;
    setName(org.name);
    setSlug(org.slug);
    setDescription(org.description ?? '');
  }, [org?.id]);

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">{t('orgSettings.profile.title')}</h1>
      <Field label={t('orgSettings.profile.nameLabel')}>
        <input data-testid="org-settings-name" className={inputClass} value={name} onChange={(e) => setName(e.target.value)} />
      </Field>
      <Field label={t('orgSettings.profile.idLabel')}>
        <span className="inline-block rounded bg-bg-subtle px-2 py-1 font-mono text-sm text-text-muted" data-testid="org-settings-slug">{slug}</span>
      </Field>
      <Field label={t('orgSettings.profile.descriptionLabel')}>
        <textarea className={inputClass} rows={4} value={description} onChange={(e) => setDescription(e.target.value)} />
      </Field>
      <Field label={t('orgSettings.profile.logoLabel')}>
        <div className="flex h-28 items-center justify-center rounded border border-dashed border-border-strong text-sm text-text-muted">
          {t('orgSettings.profile.uploadLogo')}
        </div>
      </Field>
      {update.isError && <p className="text-sm text-danger">{(update.error as Error).message}</p>}
      <button
        type="button"
        disabled={!org || update.isPending || !name.trim()}
        onClick={() => update.mutate()}
        className="rounded bg-brand px-4 py-2 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
      >
        {update.isPending ? t('orgSettings.profile.saving') : t('orgSettings.profile.save')}
      </button>
    </div>
  );
}

function HumansSection({ onOpenInvitations }: { onOpenInvitations: () => void }): React.ReactElement {
  const { t } = useTranslation('admin');
  const members = useMembers();
  const changeRole = useChangeMemberRole();
  const disable = useDisableMember();
  const reEnable = useReEnableMember();
  const drop = useDropMember();
  const humans = (members.data ?? []).filter((m) => m.kind === 'user' || m.identity_id.startsWith('user-'));
  // I41 (T470): Drop HARD-removes the membership (no tombstone) — confirm via the
  // accessible ConfirmModal (native window.confirm is banned, #169).
  const [pendingDrop, setPendingDrop] = useState<MemberResult | null>(null);
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-xl font-semibold">{t('orgSettings.humans.title')}</h1>
        <button type="button" onClick={onOpenInvitations} className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover">
          {t('orgSettings.humans.invite')}
        </button>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[48rem] text-left text-sm">
          <thead className="text-xs uppercase text-text-muted">
            <tr className="border-b border-border-base">
              <th className="px-3 py-2">{t('orgSettings.humans.colName')}</th>
              <th className="px-3 py-2">{t('orgSettings.humans.colRole')}</th>
              <th className="px-3 py-2">{t('orgSettings.humans.colJoined')}</th>
              <th className="px-3 py-2">{t('orgSettings.humans.colStatus')}</th>
              <th className="px-3 py-2 text-right">{t('orgSettings.humans.colActions')}</th>
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
                    <button type="button" onClick={() => disable.mutate({ id: m.id })} className="mr-3 text-danger hover:underline">{t('orgSettings.humans.disable')}</button>
                  ) : (
                    <button type="button" onClick={() => reEnable.mutate(m.id)} className="mr-3 text-success hover:underline">{t('orgSettings.humans.reEnable')}</button>
                  )}
                  <button type="button" onClick={() => setPendingDrop(m)} className="text-danger hover:underline">{t('orgSettings.humans.drop')}</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {humans.length === 0 && <EmptyState title={t('orgSettings.humans.emptyTitle')} body={t('orgSettings.humans.emptyBody')} />}
      <div className="rounded border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900">
        {t('orgSettings.humans.note')}
      </div>
      <ConfirmModal
        open={pendingDrop !== null}
        danger
        busy={drop.isPending}
        title={t('orgSettings.humans.dropConfirmTitle')}
        message={
          pendingDrop
            ? t('orgSettings.humans.dropConfirmMessage', { name: pendingDrop.display_name ?? normalizeIdentityRef(pendingDrop.identity_id) })
            : undefined
        }
        confirmLabel={t('orgSettings.humans.dropConfirmLabel')}
        onCancel={() => {
          if (drop.isPending) return;
          setPendingDrop(null);
          drop.reset();
        }}
        onConfirm={() => {
          if (!pendingDrop) return;
          drop.mutate(pendingDrop.id, { onSettled: () => setPendingDrop(null) });
        }}
      />
    </div>
  );
}

function InvitationsSection(): React.ReactElement {
  const { t } = useTranslation('admin');
  const invitations = useInvitations();
  const [open, setOpen] = useState(false);
  const cancel = useCancelInvitation();
  const del = useDeleteInvitation();
  const [pendingDelete, setPendingDelete] = useState<InvitationResult | null>(null);
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-xl font-semibold">{t('orgSettings.invitations.title')}</h1>
        <button type="button" onClick={() => setOpen(true)} className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover">
          {t('orgSettings.invitations.new')}
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
                  <div className="mt-1 text-sm text-text-secondary">{t('orgSettings.invitations.roleCreatedBy', { role: inv.role, creator: inv.invited_by_display_name ?? inv.invited_by_identity_id })}</div>
                  <div className="mt-1 text-xs text-text-muted">{t('orgSettings.invitations.expires', { date: formatDate(inv.expires_at) })}</div>
                </div>
                <StatusPill status={inv.status} />
              </div>
              <div className="mt-3 flex gap-2">
                <input readOnly value={url} className={`${inputClass} font-mono text-xs`} />
                <button type="button" onClick={() => void navigator.clipboard?.writeText(url)} className="rounded border border-border-base px-3 text-sm hover:bg-bg-subtle">
                  {t('orgSettings.invitations.copy')}
                </button>
              </div>
              <div className="mt-2 flex justify-between text-xs text-text-muted">
                <span>{inv.accepted_at ? t('orgSettings.invitations.accepted', { date: formatDate(inv.accepted_at) }) : ' '}</span>
                <div className="flex items-center gap-3">
                  {inv.status === 'pending' && (
                    <button type="button" onClick={() => cancel.mutate(inv.id)} className="text-danger hover:underline">{t('orgSettings.invitations.cancel')}</button>
                  )}
                  <button type="button" onClick={() => setPendingDelete(inv)} className="text-danger hover:underline">{t('orgSettings.invitations.delete')}</button>
                </div>
              </div>
            </div>
          );
        })}
      </div>
      {invitations.isSuccess && invitations.data.length === 0 && <EmptyState title={t('orgSettings.invitations.emptyTitle')} body={t('orgSettings.invitations.emptyBody')} />}
      <ConfirmModal
        open={pendingDelete !== null}
        danger
        busy={del.isPending}
        title={t('orgSettings.invitations.deleteConfirmTitle')}
        message={
          pendingDelete
            ? t('orgSettings.invitations.deleteConfirmMessage', { name: pendingDelete.invitee_display_name ?? pendingDelete.invitee_user_id })
            : undefined
        }
        confirmLabel={t('orgSettings.invitations.deleteConfirmLabel')}
        onCancel={() => {
          if (del.isPending) return;
          setPendingDelete(null);
          del.reset();
        }}
        onConfirm={() => {
          if (!pendingDelete) return;
          del.mutate(pendingDelete.id, { onSettled: () => setPendingDelete(null) });
        }}
      />
    </div>
  );
}

function InvitationModal({ onClose }: { onClose: () => void }): React.ReactElement {
  const { t } = useTranslation('admin');
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
        <h2 className="mb-4 text-lg font-semibold">{t('orgSettings.invitationModal.title')}</h2>
        <Field label={t('orgSettings.invitationModal.inviteeLabel')}>
          <input className={inputClass} value={inviteeUserId} onChange={(e) => setInviteeUserId(e.target.value)} placeholder={t('orgSettings.invitationModal.inviteePlaceholder')} />
        </Field>
        <Field label={t('orgSettings.invitationModal.roleLabel')}>
          <select className={inputClass} value={role} onChange={(e) => setRole(e.target.value)}>
            <option value="member">member</option>
            <option value="admin">admin</option>
            <option value="owner">owner</option>
          </select>
        </Field>
        {create.isError && <p className="mb-3 text-sm text-danger">{create.error instanceof ApiError ? create.error.message : t('orgSettings.invitationModal.createError')}</p>}
        <div className="flex justify-end gap-2">
          <button type="button" onClick={onClose} className="rounded px-3 py-1.5 text-sm hover:bg-bg-subtle">{t('orgSettings.invitationModal.cancel')}</button>
          <button type="submit" disabled={!inviteeUserId.trim() || create.isPending} className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50">
            {create.isPending ? t('orgSettings.invitationModal.creating') : t('orgSettings.invitationModal.create')}
          </button>
        </div>
      </form>
    </div>
  );
}

function DangerSection(): React.ReactElement {
  const { t } = useTranslation('admin');
  const orgCtx = useOptionalOrgContext();
  const orgs = useOrgs();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const org = (orgs.data ?? []).find((o) => o.slug === orgCtx?.slug);
  const isDisabled = org?.disabled ?? false;
  // I41 (T470): "Disable" is a REVERSIBLE login gate (non-owner members can't
  // enter; the owner keeps full access) — NOT a delete. It toggles to "Enable".
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const toggle = useMutation({
    mutationFn: () => {
      if (!org) return Promise.resolve();
      return isDisabled ? orgApi.enable(org.id) : orgApi.disable(org.id);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ['orgs'] }),
    onSettled: () => setConfirmOpen(false),
  });
  const forceDelete = useMutation({
    mutationFn: () => {
      if (!org) return Promise.resolve();
      return orgApi.delete(org.id);
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['orgs'] });
      navigate('/');
    },
    onSettled: () => setDeleteConfirmOpen(false),
  });
  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold text-danger">{t('orgSettings.danger.title')}</h1>
      {isDisabled ? (
        <DangerCard
          title={t('orgSettings.danger.enableTitle')}
          body={t('orgSettings.danger.enableBody')}
          button={toggle.isPending ? t('orgSettings.danger.enabling') : t('orgSettings.danger.enableButton')}
          disabled={!org || toggle.isPending}
          onClick={() => setConfirmOpen(true)}
        />
      ) : (
        <DangerCard
          title={t('orgSettings.danger.disableTitle')}
          body={t('orgSettings.danger.disableBody')}
          button={toggle.isPending ? t('orgSettings.danger.disabling') : t('orgSettings.danger.disableButton')}
          disabled={!org || toggle.isPending}
          onClick={() => setConfirmOpen(true)}
        />
      )}
      {toggle.isError && <p className="text-sm text-danger">{(toggle.error as Error).message}</p>}
      {forceDelete.isError && <p className="text-sm text-danger">{(forceDelete.error as Error).message}</p>}
      <DangerCard
        title={t('orgSettings.danger.forceDeleteTitle')}
        body={t('orgSettings.danger.forceDeleteBody')}
        button={forceDelete.isPending ? t('orgSettings.danger.deleting') : t('orgSettings.danger.forceDeleteButton')}
        disabled={!org || forceDelete.isPending}
        onClick={() => setDeleteConfirmOpen(true)}
      />
      <DangerCard title={t('orgSettings.danger.auditLogTitle')} body={t('orgSettings.danger.auditLogBody')} button={t('orgSettings.danger.auditLogButton')} disabled />
      <ConfirmModal
        open={confirmOpen}
        danger={!isDisabled}
        busy={toggle.isPending}
        title={isDisabled ? t('orgSettings.danger.enableConfirmTitle') : t('orgSettings.danger.disableConfirmTitle')}
        message={
          isDisabled
            ? t('orgSettings.danger.enableConfirmMessage', { name: org?.name ?? '' })
            : t('orgSettings.danger.disableConfirmMessage', { name: org?.name ?? '' })
        }
        confirmLabel={isDisabled ? t('orgSettings.danger.enableConfirmLabel') : t('orgSettings.danger.disableConfirmLabel')}
        onCancel={() => {
          if (toggle.isPending) return;
          setConfirmOpen(false);
          toggle.reset();
        }}
        onConfirm={() => toggle.mutate()}
      />
      <ConfirmModal
        open={deleteConfirmOpen}
        danger
        busy={forceDelete.isPending}
        title={t('orgSettings.danger.forceDeleteConfirmTitle')}
        message={t('orgSettings.danger.forceDeleteConfirmMessage', { name: org?.name ?? '' })}
        confirmLabel={t('orgSettings.danger.forceDeleteConfirmLabel')}
        onCancel={() => {
          if (forceDelete.isPending) return;
          setDeleteConfirmOpen(false);
          forceDelete.reset();
        }}
        onConfirm={() => forceDelete.mutate()}
      />
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

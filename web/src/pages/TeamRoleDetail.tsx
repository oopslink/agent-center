import type React from 'react';
import { useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ApiError } from '@/api/client';
import { type RAMRole, useRAMRoles } from '@/api/access';
import {
  useDeleteTeamRole,
  usePreviewDeleteTeamRole,
  usePreviewTeamRoleRAMMapping,
  useReplaceTeamRoleRAMMapping,
  useTeam,
  useTeamMembers,
  useTeamProjects,
  useTeamRoleRAMMappings,
} from '@/api/teams';
import { hasEffectivePermission, useCurrentSubjectEffectivePermissions } from '@/api/permissions';
import { AccessMetaPill, AccessRiskBadge } from '@/components/access/kit';
import { ConfirmModal } from '@/components/ConfirmModal';
import { EntityMultiSelect } from '@/components/EntityMultiSelect';
import { EmptyState } from '@/components/EmptyState';
import { IconClose } from '@/components/icons';
import { Skeleton } from '@/components/Skeleton';
import { useOptionalOrgContext } from '@/OrgContext';

type Banner = { tone: 'success' | 'danger' | 'warning'; text: string } | null;

export default function TeamRoleDetail(): React.ReactElement {
  const { teamId = '', role = '' } = useParams();
  const teamRole = decodeURIComponent(role);
  const org = useOptionalOrgContext();
  const team = useTeam(teamId);
  const members = useTeamMembers(teamId);
  const projects = useTeamProjects(teamId);
  const ramRoles = useRAMRoles();
  const mappingQueries = useTeamRoleRAMMappings(teamId, teamRole ? [teamRole] : []);
  const saved = mappingQueries[0];
  const permissions = useCurrentSubjectEffectivePermissions({ kind: 'org', id: org?.orgId ?? 'org' });
  const canEdit = hasEffectivePermission(permissions.data, 'org.member.role.manage');
  const orgBase = org ? `/organizations/${org.slug}` : '';
  const [editing, setEditing] = useState(false);
  const [banner, setBanner] = useState<Banner>(null);

  if (team.isLoading) return <PageLoading />;
  if (team.isError || !team.data) return <PageError title="Team Role unavailable" error={team.error} onRetry={() => void team.refetch()} />;
  const currentRole = team.data.roles.find((item) => item.role === teamRole);
  if (!currentRole) return <PageError title="Team Role not found" error={new Error(`${teamRole} is not declared by ${team.data.name}.`)} onRetry={() => void team.refetch()} />;

  const affectedMembers = (members.data ?? []).filter((member) => (member.roles?.length ? member.roles : [member.role]).includes(teamRole)).length;
  const savedRoles = saved.data?.ram_role_ids ?? [];
  const effectivePermissions = new Set((ramRoles.data?.roles ?? []).filter((item) => savedRoles.includes(item.id)).flatMap((item) => item.permissions)).size;

  return (
    <section className="min-w-0 space-y-4" data-testid="page-TeamRoleDetail">
      <nav className="flex flex-wrap items-center gap-1 text-xs text-text-muted" aria-label="Breadcrumb">
        <Link className="hover:text-text-primary" to={`${orgBase}/teams`}>Teams</Link><span>/</span>
        <Link className="hover:text-text-primary" to={`${orgBase}/teams/${teamId}`}>{team.data.name}</Link><span>/</span>
        <Link className="hover:text-text-primary" to={`${orgBase}/teams/${teamId}`}>Roles</Link><span>/</span>
        <span className="font-semibold text-text-primary">{teamRole}</span>
      </nav>

      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">{teamRole}</h1>
          <p className="mt-1 text-sm text-text-secondary">Team Role properties for {team.data.name}.</p>
        </div>
        <div className="flex gap-2">
          <Link className="rounded border border-border-base px-3 py-1.5 text-sm font-semibold text-text-primary hover:bg-bg-subtle" to={`${orgBase}/teams/${teamId}`}>Back to roles</Link>
          <button
            type="button"
            className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-semibold text-btn-primary-fg disabled:cursor-not-allowed disabled:opacity-50"
            disabled={!canEdit || saved.isLoading || saved.isError}
            title={!canEdit ? 'Requires org.member.role.manage' : undefined}
            onClick={() => setEditing(true)}
            data-testid="team-role-edit"
          >Edit Team Role</button>
        </div>
      </header>

      {banner && <div className={`rounded border px-4 py-3 text-sm ${bannerClass(banner.tone)}`} role="status" data-testid={`team-role-${banner.tone}`}><strong>{banner.text}</strong></div>}
      {permissions.isSuccess && !canEdit && (
        <div className="rounded border border-warning/40 bg-warning/10 px-4 py-3 text-sm text-warning" data-testid="team-role-permission-gate">
          Read-only. Editing requires <span className="font-mono">org.member.role.manage</span>.
        </div>
      )}

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <Stat label="Members" value={affectedMembers} />
        <Stat label="RAM Roles" value={savedRoles.length} />
        <Stat label="Effective permissions" value={effectivePermissions} />
        <Stat label="Projects" value={projects.data?.length ?? team.data.projects_count} />
        <Stat label="Version" value={saved.data?.version ?? '…'} />
      </div>

      <div className="grid min-w-0 gap-4 xl:grid-cols-[18rem_minmax(0,1fr)]">
        <section className="rounded border border-border-base bg-bg-elevated" data-testid="team-role-list">
          <div className="border-b border-border-base px-4 py-3"><h2 className="text-sm font-semibold text-text-primary">Team Roles</h2><p className="mt-1 text-xs text-text-muted">Select a role to inspect all of its properties.</p></div>
          <div className="divide-y divide-border-base">
            {team.data.roles.map((item) => (
              <Link key={item.role} to={`${orgBase}/teams/${teamId}/roles/${encodeURIComponent(item.role)}`} className={`block px-4 py-3 hover:bg-bg-subtle ${item.role === teamRole ? 'bg-brand/10' : ''}`} aria-current={item.role === teamRole ? 'page' : undefined}>
                <div className="flex items-center justify-between gap-2"><span className="font-semibold text-text-primary">{item.role}</span><AccessMetaPill>{item.count ?? 0} members</AccessMetaPill></div>
                <div className="mt-1 font-mono text-xs text-text-muted">{item.cli} · {item.model} · {item.max_concurrency}</div>
              </Link>
            ))}
          </div>
        </section>

        <section className="rounded border border-border-base bg-bg-elevated" aria-label={`${teamRole} Team Role details`}>
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border-base px-4 py-3">
            <div><h2 className="text-sm font-semibold text-text-primary">{teamRole} properties</h2><p className="mt-1 text-xs text-text-muted">Saved configuration · server version {saved.data?.version ?? '…'}</p></div>
            <AccessMetaPill>{saved.data?.updated_at ?? 'authoritative read'}</AccessMetaPill>
          </div>

          <DetailSection title="Work configuration" description="Execution defaults carried by this Team Role." testId="team-role-work-configuration">
            <div className="grid gap-3 sm:grid-cols-3"><Spec label="CLI" value={currentRole.cli} /><Spec label="Model" value={currentRole.model} /><Spec label="Concurrency" value={currentRole.max_concurrency} /></div>
          </DetailSection>

          <DetailSection title="Access configuration" description="RAM Roles this Team Role grants directly to its members." testId="team-role-access-configuration">
            {saved.isLoading && <Skeleton height="5rem" />}
            {saved.isError && <InlineError text={(saved.error as Error).message} action="Retry" onAction={() => void saved.refetch()} testId="team-role-access-error" />}
            {saved.isSuccess && <RAMRoleList ids={savedRoles} roles={ramRoles.data?.roles ?? []} />}
            <div className="mt-3 rounded border border-border-base bg-bg-subtle p-3 text-xs text-text-secondary">
              <strong>{affectedMembers} members inherit these permissions.</strong> Scope follows {team.data.name} and its {projects.data?.length ?? team.data.projects_count} linked projects.
            </div>
          </DetailSection>

          <DetailSection title="Capabilities" description="Declarative scheduling and supervision hints.">
            <div className="flex flex-wrap gap-1">{currentRole.capability_tags.length ? currentRole.capability_tags.map((tag) => <span key={tag} className="rounded border border-border-base bg-bg-subtle px-2 py-1 text-xs">{tag}</span>) : <span className="text-xs text-text-muted">No capability tags</span>}</div>
          </DetailSection>

          <DetailSection title="Audit" description="Latest authoritative Team Role access change." testId="team-role-audit">
            <div className="flex items-center justify-between gap-3 rounded border border-border-base p-3 text-sm"><div><strong>Team Role access updated</strong><div className="mt-1 text-xs text-text-muted">{saved.data?.updated_by ?? 'server'} · {saved.data?.updated_at ?? 'latest readback'}</div></div><span className="font-mono text-xs">v{saved.data?.version ?? '…'}</span></div>
          </DetailSection>
        </section>
      </div>

      {editing && (
        <TeamRoleEditor
          team={team.data}
          teamRole={teamRole}
          role={currentRole}
          saved={saved}
          roles={ramRoles.data?.roles ?? []}
          projectNames={(projects.data ?? []).map((project) => project.name)}
          onClose={() => setEditing(false)}
          onSaved={(next) => { setBanner({ tone: 'success', text: `RAM Roles saved. Server readback confirmed version ${next.version}: ${next.names.join(', ') || 'none'}.` }); setEditing(false); }}
          onDeleted={() => { setBanner({ tone: 'success', text: `Team Role ${teamRole} deleted. Audit readback confirmed the change.` }); }}
        />
      )}
    </section>
  );
}

function TeamRoleEditor({ team, teamRole, role, saved, roles, projectNames, onClose, onSaved, onDeleted }: {
  team: NonNullable<ReturnType<typeof useTeam>['data']>;
  teamRole: string;
  role: NonNullable<ReturnType<typeof useTeam>['data']>['roles'][number];
  saved: ReturnType<typeof useTeamRoleRAMMappings>[number];
  roles: RAMRole[];
  projectNames: string[];
  onClose: () => void;
  onSaved: (value: { version: number; names: string[] }) => void;
  onDeleted: () => void;
}): React.ReactElement {
  const navigate = useNavigate();
  const org = useOptionalOrgContext();
  const preview = usePreviewTeamRoleRAMMapping();
  const replace = useReplaceTeamRoleRAMMapping();
  const previewDelete = usePreviewDeleteTeamRole();
  const deleteRole = useDeleteTeamRole();
  const current = saved.data;
  const [draft, setDraft] = useState(current?.ram_role_ids ?? []);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [confirmName, setConfirmName] = useState('');
  const [reassignRole, setReassignRole] = useState(team.roles.find((item) => item.role !== teamRole)?.role ?? '');
  const conflict = replace.error instanceof ApiError && replace.error.status === 409;
  const changed = sorted(draft).join('|') !== sorted(current?.ram_role_ids ?? []).join('|');

  useEffect(() => { setDraft(current?.ram_role_ids ?? []); preview.reset(); replace.reset(); }, [current?.version]);

  const updateDraft = (values: string[]) => {
    const next = sorted(values);
    setDraft(next);
    replace.reset();
    preview.mutate({ team_id: team.id, role: teamRole, ram_role_ids: next });
  };
  const save = () => {
    if (!current || !preview.data) return;
    replace.mutate({ team_id: team.id, role: teamRole, ram_role_ids: draft, expected_version: current.version }, {
      onSuccess: async () => {
        const readback = await saved.refetch();
        const authoritative = readback.data;
        if (!authoritative) return;
        onSaved({ version: authoritative.version, names: authoritative.ram_role_ids.map((id) => roles.find((item) => item.id === id)?.name ?? id) });
      },
    });
  };
  const refresh = async () => {
    const readback = await saved.refetch();
    setDraft(readback.data?.ram_role_ids ?? []);
    preview.reset(); replace.reset();
  };
  const openDelete = async () => { await previewDelete.mutateAsync({ team_id: team.id, role: teamRole }); setDeleteOpen(true); };
  const applyDelete = () => {
    if (!previewDelete.data) return;
    deleteRole.mutate({ team_id: team.id, role: teamRole, expected_version: team.version, reassign_role: reassignRole, confirm_name: confirmName }, {
      onSuccess: async () => {
        onDeleted();
        navigate(`${org ? `/organizations/${org.slug}` : ''}/teams/${team.id}`);
      },
    });
  };
  const impact = preview.data;
  const addCount = impact?.added_ram_role_ids.length ?? 0;
  const removeCount = impact?.removed_ram_role_ids.length ?? 0;

  return (
    <div className="fixed inset-0 z-30 bg-black/45" data-testid="team-role-editor-layer">
      <aside className="ml-auto flex h-full w-full max-w-xl flex-col border-l border-border-base bg-bg-elevated shadow-2xl" role="dialog" aria-modal="true" aria-labelledby="team-role-editor-title" data-testid="team-role-editor">
        <div className="flex items-start justify-between gap-3 border-b border-border-base px-5 py-4"><div><h2 id="team-role-editor-title" className="text-lg font-semibold text-text-primary">Edit Team Role</h2><p className="mt-1 text-xs text-text-muted">{team.name} / {teamRole} · server version {current?.version ?? '…'}</p></div><button type="button" aria-label="Close editor" onClick={onClose}><IconClose /></button></div>
        <div className="min-h-0 flex-1 space-y-5 overflow-y-auto p-5">
          <section className="rounded border border-border-base bg-bg-subtle p-3"><h3 className="text-sm font-semibold text-text-primary">Work configuration</h3><div className="mt-3 grid grid-cols-3 gap-2"><Spec label="CLI" value={role.cli} /><Spec label="Model" value={role.model} /><Spec label="Concurrency" value={role.max_concurrency} /></div></section>
          <section data-testid="team-role-editor-access"><h3 className="text-sm font-semibold text-text-primary">Access configuration</h3><p className="mt-1 text-xs text-text-muted">Choose the complete saved set of RAM Roles for this Team Role.</p>
            <div className="mt-3"><label className="mb-1 block text-xs font-semibold uppercase text-text-muted">Add RAM Role</label><EntityMultiSelect testId="team-role-ram-roles" options={roles.filter((item) => !item.revoked_at).map((item) => ({ value: item.id, label: item.name, hint: `${item.permissions.length} permissions · ${item.risk} risk · v${item.version}` }))} values={draft} onChange={updateDraft} placeholder="Add RAM Role" searchPlaceholder="Search RAM Roles" emptyLabel="No available RAM Roles" ariaLabel="RAM Roles" /></div>
            <div className="mt-3 space-y-2" data-testid="team-role-selected-ram-roles">
              {draft.length === 0 && <p className="rounded border border-dashed border-border-base p-3 text-xs text-text-muted">No RAM Roles selected.</p>}
              {draft.map((id) => { const item = roles.find((candidate) => candidate.id === id); return <div key={id} className="flex items-center justify-between gap-3 rounded border border-border-base p-3"><div><div className="font-semibold text-text-primary">{item?.name ?? id}</div><div className="mt-1 text-xs text-text-muted">{item?.permissions.length ?? 0} permissions · {item?.risk ?? 'unknown'} risk</div></div><button type="button" className="rounded border border-danger/30 px-2 py-1 text-xs font-semibold text-danger" onClick={() => updateDraft(draft.filter((value) => value !== id))}>Remove</button></div>; })}
            </div>
          </section>

          <section className="rounded border border-border-base p-3" data-testid="team-role-impact">
            <div className="flex items-center justify-between gap-2"><h3 className="text-sm font-semibold text-text-primary">Change preview</h3><AccessMetaPill>expected v{current?.version ?? '…'}</AccessMetaPill></div>
            {!changed && <p className="mt-2 text-xs text-text-muted">No changes.</p>}
            {changed && preview.isPending && <p className="mt-2 text-xs text-text-muted">Calculating affected members and projects…</p>}
            {impact && <div className="mt-3 space-y-2 text-sm"><div className="grid grid-cols-2 gap-2"><Stat label="Affected members" value={impact.affected_members} /><Stat label="Linked projects" value={impact.affected_project_ids.length} /></div><p><strong>RAM Roles:</strong> +{addCount} / −{removeCount}</p><p><strong>Projects:</strong> {projectNames.length ? projectNames.join(', ') : impact.affected_project_ids.join(', ') || 'none'}</p>{removeCount > 0 && <p className="rounded border border-warning/40 bg-warning/10 p-2 text-xs text-warning">Removed permissions fail closed immediately after save. Independent direct grants remain.</p>}</div>}
          </section>

          {conflict && <InlineError text="Team Role changed on the server (409). Your changes were not saved. Refresh the latest version, review the RAM Roles, then try again." action="Refresh latest" onAction={() => void refresh()} testId="team-role-conflict" />}
          {replace.isError && !conflict && <InlineError text={(replace.error as Error).message} action="Try again" onAction={save} testId="team-role-save-error" />}
          {preview.isError && <InlineError text={(preview.error as Error).message} action="Retry preview" onAction={() => preview.mutate({ team_id: team.id, role: teamRole, ram_role_ids: draft })} testId="team-role-preview-error" />}

          <section className="border-t border-border-base pt-4"><h3 className="text-sm font-semibold text-danger">Delete Team Role</h3><p className="mt-1 text-xs text-text-muted">Deletion is protected by server impact review, member reassignment, exact-name confirmation, version validation, and audit.</p><button type="button" className="mt-3 rounded border border-danger/40 px-3 py-1.5 text-xs font-semibold text-danger disabled:opacity-50" disabled={previewDelete.isPending} onClick={() => void openDelete()} data-testid="team-role-delete">Review deletion</button>{previewDelete.isError && <p className="mt-2 text-xs text-danger">{(previewDelete.error as Error).message}</p>}</section>
        </div>
        <div className="flex items-center justify-end gap-2 border-t border-border-base px-5 py-4"><button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm" onClick={onClose}>Cancel</button><button type="button" className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-semibold text-btn-primary-fg disabled:opacity-50" disabled={!changed || !impact || preview.isPending || replace.isPending || conflict} onClick={save} data-testid="team-role-save">Save changes</button></div>
      </aside>

      <ConfirmModal open={deleteOpen} title={`Delete ${teamRole} Team Role`} danger busy={deleteRole.isPending} confirmLabel="Delete Team Role" confirmDisabled={Boolean(previewDelete.data?.protected) || confirmName !== teamRole || !reassignRole} onCancel={() => setDeleteOpen(false)} onConfirm={applyDelete} message={<div className="space-y-3" data-testid="team-role-delete-impact"><p>This affects {previewDelete.data?.affected_members ?? 0} members and {previewDelete.data?.affected_project_ids?.length ?? 0} linked projects.</p>{previewDelete.data?.protected ? <p className="font-semibold text-danger">This Team Role is protected and cannot be deleted.</p> : <><label className="block text-xs font-semibold">Reassign affected members to<select className="mt-1 w-full rounded border border-border-base bg-bg-base p-2" value={reassignRole} onChange={(event) => setReassignRole(event.target.value)}>{team.roles.filter((item) => item.role !== teamRole).map((item) => <option key={item.role}>{item.role}</option>)}</select></label><label className="block text-xs font-semibold">Type {teamRole} to confirm<input className="mt-1 w-full rounded border border-border-base bg-bg-base p-2" value={confirmName} onChange={(event) => setConfirmName(event.target.value)} /></label></>}</div>} />
    </div>
  );
}

function PageLoading(): React.ReactElement { return <section className="space-y-4" data-testid="team-role-loading" aria-busy="true"><div><p className="text-xs text-text-muted">Teams / Team / Roles / Team Role</p><h1 className="mt-1 text-2xl font-semibold">Team Role</h1></div><Skeleton height="5rem" /><Skeleton height="24rem" /></section>; }
function PageError({ title, error, onRetry }: { title: string; error: unknown; onRetry: () => void }): React.ReactElement { return <section className="rounded border border-danger/30 bg-danger/10 p-5" role="alert" data-testid="team-role-error"><h1 className="text-lg font-semibold text-danger">{title}</h1><p className="mt-2 text-sm text-danger">{error instanceof Error ? error.message : 'Unknown error'}</p><button type="button" className="mt-4 rounded border border-danger/40 px-3 py-1.5 text-sm font-semibold text-danger" onClick={onRetry}>Retry</button></section>; }
function Stat({ label, value }: { label: string; value: React.ReactNode }): React.ReactElement { return <div className="rounded border border-border-base bg-bg-elevated p-3"><div className="text-[0.6875rem] font-semibold uppercase text-text-muted">{label}</div><div className="mt-1 text-xl font-semibold text-text-primary">{value}</div></div>; }
function Spec({ label, value }: { label: string; value: React.ReactNode }): React.ReactElement { return <div><div className="text-[0.6875rem] font-semibold uppercase text-text-muted">{label}</div><div className="mt-1 break-words font-mono text-sm text-text-primary">{value}</div></div>; }
function DetailSection({ title, description, children, testId }: { title: string; description: string; children: React.ReactNode; testId?: string }): React.ReactElement { return <section className="border-b border-border-base p-4 last:border-0" data-testid={testId}><div><h3 className="text-sm font-semibold text-text-primary">{title}</h3><p className="mt-1 text-xs text-text-muted">{description}</p></div><div className="mt-3">{children}</div></section>; }
function RAMRoleList({ ids, roles }: { ids: string[]; roles: RAMRole[] }): React.ReactElement { if (!ids.length) return <EmptyState title="No RAM Roles" body="This Team Role does not grant a RAM Role." testId="team-role-ram-roles-empty" />; return <div className="grid gap-2 sm:grid-cols-2">{ids.map((id) => { const role = roles.find((item) => item.id === id); return <div key={id} className="rounded border border-border-base p-3"><div className="flex items-center justify-between gap-2"><strong className="text-sm text-text-primary">{role?.name ?? id}</strong>{role && <AccessRiskBadge risk={role.risk} />}</div><p className="mt-1 text-xs text-text-muted">{role?.permissions.length ?? 0} permissions · {role?.scope ?? 'team'} scope</p></div>; })}</div>; }
function InlineError({ text, action, onAction, testId }: { text: string; action: string; onAction: () => void; testId: string }): React.ReactElement { return <div className="rounded border border-danger/40 bg-danger/10 p-3 text-sm text-danger" role="alert" data-testid={testId}><p>{text}</p><button type="button" className="mt-2 rounded border border-danger/40 px-2 py-1 text-xs font-semibold" onClick={onAction}>{action}</button></div>; }
function bannerClass(tone: NonNullable<Banner>['tone']): string { return tone === 'success' ? 'border-success/30 bg-success/10 text-success' : tone === 'warning' ? 'border-warning/40 bg-warning/10 text-warning' : 'border-danger/40 bg-danger/10 text-danger'; }
function sorted(values: string[]): string[] { return [...new Set(values)].sort(); }

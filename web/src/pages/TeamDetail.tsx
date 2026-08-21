// Team detail (/organizations/:slug/teams/:teamId) — 4 tabs:
// Overview / Members / Linked projects / Team Memory. Team Memory is the only
// product surface for team entries/rules; templates are not a routed product.
import { useEffect, useState } from 'react';
import type React from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Trans, useTranslation } from 'react-i18next';
import { useOptionalOrgContext } from '@/OrgContext';
import {
  useAssociateProject,
  useDisassociateProject,
  useDirectoryAgents,
  useDeleteTeamRole,
  useRemoveMember,
  usePreviewDeleteTeamRole,
  usePreviewTeamRoleRAMMapping,
  useReadTeam,
  useReplaceTeamRoleRAMMapping,
  useTeam,
  useTeamRoleRAMMappings,
  useTeamMemoryIndex,
  useTeamMemorySettings,
  useTeamMembers,
  useTeamProjects,
  useUpdateTeamMemorySettings,
  useUpdateTeamRoles,
  type TeamMemoryPolicy,
  type RoleInput,
  type MemberView,
  type TeamRoleDeleteImpact,
  type TeamRAMRoleMappingImpact,
} from '@/api/teams';
import { useProjects } from '@/api/projects';
import { ConfirmModal } from '@/components/ConfirmModal';
import { EntityMultiSelect } from '@/components/EntityMultiSelect';
import { EmptyState } from '@/components/EmptyState';
import { isSelectableRuntimePair, useRuntimeSelectorCatalog } from '@/components/RuntimeSelectors';
import { Skeleton } from '@/components/Skeleton';
import { AddMemberModal } from '@/components/teams/AddMemberModal';
import { MemoryPane } from '@/components/teams/MemoryPane';
import { RoleBuilder } from '@/components/teams/RoleBuilder';
import {
  btnGhost,
  btnSm,
  btnSmDanger,
  btnSmPrimary,
  Card,
  Field,
  inputCls,
  ModalShell,
  Note,
  SectionHead,
  SpecLine,
  Tabs,
} from '@/components/teams/kit';
import {
  Glyph,
  KindTag,
  RoleBar,
  RoleLegend,
  StatusChip,
  roleColorChip,
} from '@/components/teams/teamsUi';
import { roleColor, type TeamView } from '@/api/teams';

export default function TeamDetail(): React.ReactElement {
  const { t } = useTranslation('teams');
  const { teamId = '' } = useParams();
  const team = useTeam(teamId);
  const navigate = useNavigate();
  const org = useOptionalOrgContext();
  const orgBase = org ? `/organizations/${org.slug}` : '';
  const [tab, setTab] = useState<TabKey>('ov');

  const TABS = [
    { key: 'ov', label: t('teamDetail.tabs.overview') },
    { key: 'mm', label: t('teamDetail.tabs.members') },
    { key: 'pj', label: t('teamDetail.tabs.projects') },
    { key: 'tm', label: t('teamDetail.tabs.memory') },
    { key: 'st', label: t('teamDetail.tabs.settings') },
  ] as const;

  if (team.isLoading) {
    return (
      <section className="space-y-4" data-testid="page-TeamDetail">
        <Skeleton height="5rem" />
        <Skeleton height="16rem" />
      </section>
    );
  }
  if (team.isError || !team.data) {
    return (
      <section data-testid="page-TeamDetail">
        <button type="button" className={btnGhost} onClick={() => navigate(`${orgBase}/teams`)}>
          {t('teamDetail.backToTeams')}
        </button>
        <p className="mt-4 text-sm text-danger" data-testid="team-detail-error">
          {(team.error as Error)?.message ?? t('teamDetail.notFound')}
        </p>
      </section>
    );
  }

  const tv = team.data;
  const roleNames = tv.roles.map((r) => r.role);

  return (
    <section className="space-y-2" data-testid="page-TeamDetail">
      <div className="flex items-start gap-4">
        <Glyph text={tv.glyph} size="lg" />
        <div>
          <div className="flex items-center gap-3">
            <h1 className="font-heading text-xl font-semibold text-text-primary">{tv.name}</h1>
            <StatusChip status={tv.status} />
          </div>
          <div className="mt-1.5 flex flex-wrap gap-x-3.5 gap-y-1 text-xs text-text-muted">
            <span className="font-mono">{tv.id}</span>
            <span>{t('teamDetail.membersRoles', { members: tv.members_count, roles: tv.roles.length })}</span>
            <span>{t('teamDetail.projectsCount', { count: tv.projects_count })}</span>
            <span>{t('teamDetail.createdAt', { date: tv.created })}</span>
          </div>
        </div>
      </div>

      <Tabs tabs={TABS} active={tab} onChange={setTab} testId="team-tabs" />

      <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`}>
        {tab === 'ov' && <OverviewPane team={tv} />}
        {tab === 'mm' && <MembersPane teamId={tv.id} roleOptions={roleNames} team={tv} />}
        {/* panes below */}
        {tab === 'pj' && <ProjectsPane teamId={tv.id} />}
        {tab === 'tm' && <MemoryPane teamId={tv.id} heading={t('teamDetail.memoryHeading')} team={tv} currentUserRole={org?.role} />}
        {tab === 'st' && <TeamSettingsPane team={tv} />}
      </div>

    </section>
  );
}

const TABS_KEYS = ['ov', 'mm', 'pj', 'tm', 'st'] as const;
type TabKey = (typeof TABS_KEYS)[number];

function OverviewPane({ team: tv }: { team: TeamView }): React.ReactElement {
  // Only the structural facts (roles / members / projects) and the team-memory
  // index are truthful data sources in Phase-1. Live task/agent telemetry has no
  // team-scoped facade aggregate yet — show an honest "—" placeholder rather than
  // a fabricated constant (a fresh empty team must not read as "3 running tasks").
  const { t } = useTranslation('teams');
  const memory = useTeamMemoryIndex(tv.id);
  const memoryEntries = memory.data?.filter((node) => node.slug).length;
  const [editingRoles, setEditingRoles] = useState(false);
  const [roleSaveMessage, setRoleSaveMessage] = useState('');
  const NA = <span className="text-text-muted">—</span>;
  return (
    <div className="grid gap-3.5 md:grid-cols-2">
      <Card>
        <SectionHead title={t('teamDetail.overview.roleMix')} hint={t('teamDetail.overview.roleMixHint')} action={
          <button type="button" className={btnSm} data-testid="team-edit-roles" onClick={() => setEditingRoles(true)}>
            {t('teamDetail.roles.edit')}
          </button>
        } />
        <RoleBar roles={tv.roles} className="w-full" showCount={false} />
        <RoleLegend roles={tv.roles} showCount={false} />
        <div className="mt-3.5">
          {tv.roles.map((r) => (
            <div key={r.role} className="border-b border-border-base py-2 text-xs last:border-0" data-testid={`team-role-used-by-${r.role}`}>
              <div className="flex items-center justify-between gap-3">
                <span className="flex items-center gap-1.5 text-text-muted">
                  <span className="h-2 w-2 rounded-sm" style={{ background: roleColor(r.role) }} aria-hidden="true" />
                  {r.role}
                </span>
                <span className="font-mono text-text-primary">{t('teamDetail.overview.roleSpec', { cli: r.cli, model: r.model, conc: r.max_concurrency })}</span>
              </div>
              <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[0.6875rem] text-text-muted">
                <span>{t('teamDetail.overview.ramUsedBy', { count: r.count ?? 0 })}</span>
                {(r.ram_role_keys ?? []).map((ramRole) => (
                  <span key={ramRole} className="rounded border border-border-base bg-bg-subtle px-1.5 py-0.5 font-mono">{ramRole}</span>
                ))}
                {(r.ram_role_keys ?? []).length === 0 && <span>{t('teamDetail.overview.noRamRoles')}</span>}
              </div>
            </div>
          ))}
        </div>
      </Card>
      <Card>
        <SectionHead title={t('teamDetail.overview.summary')} hint={t('teamDetail.overview.summaryHint')} />
        <SpecLine k={t('teamDetail.overview.enrolledMembers')} v={`${tv.members_count}`} />
        <SpecLine k={t('teamDetail.overview.declaredRoles')} v={`${tv.roles.length}`} />
        <SpecLine k={t('teamDetail.overview.linkedProjects')} v={`${tv.projects_count}`} />
        <SpecLine
          k={t('teamDetail.overview.teamMemory')}
          v={memory.isLoading ? NA : memoryEntries != null ? t('teamDetail.overview.memoryEntries', { count: memoryEntries }) : NA}
        />
        <SpecLine k={t('teamDetail.overview.runningTasks')} v={NA} />
        <SpecLine k={t('teamDetail.overview.blockedTasks')} v={NA} />
        <Note testId="team-health-note">{t('teamDetail.overview.healthNote')}</Note>
        {roleSaveMessage && (
          <div role="status" data-testid="team-role-save-success" className="mt-3 rounded-md border border-success/30 bg-success/10 px-3 py-2 text-sm text-success">
            {roleSaveMessage}
          </div>
        )}
      </Card>
      {editingRoles && <EditRolesModal team={tv} onClose={() => setEditingRoles(false)} onSaved={(message) => setRoleSaveMessage(message)} />}
    </div>
  );
}

function EditRolesModal({ team, onClose, onSaved, initialPanel = 'runtime' }: { team: TeamView; onClose: () => void; onSaved?: (message: string) => void; initialPanel?: 'runtime' | 'access' }): React.ReactElement {
  const { t } = useTranslation('teams');
  const update = useUpdateTeamRoles();
  const readTeam = useReadTeam();
  const previewRAMRoles = usePreviewTeamRoleRAMMapping();
  const replaceRAMRoles = useReplaceTeamRoleRAMMapping();
  const previewDeleteRole = usePreviewDeleteTeamRole();
  const deleteRole = useDeleteTeamRole();
  const runtimeCatalog = useRuntimeSelectorCatalog();
  const members = useTeamMembers(team.id);
  const mappingQueries = useTeamRoleRAMMappings(team.id, team.roles.map((role) => role.role));
  const [roles, setRoles] = useState<RoleInput[]>(() => team.roles.map((role) => ({
    role: role.role,
    cli: role.cli,
    model: role.model,
    max_concurrency: role.max_concurrency,
    count: role.count ?? 1,
    tags: role.capability_tags.join(', '),
    ram_role_keys: role.ram_role_keys ?? [],
    access_requirements: role.access_requirements ?? [],
    access_lint: role.access_lint ?? [],
  })));
  const [panel, setPanel] = useState<'runtime' | 'access'>(initialPanel);
  const [pendingAccess, setPendingAccess] = useState<{
    roles: RoleInput[];
    impacts: TeamRAMRoleMappingImpact[];
  } | null>(null);
  const [deleteState, setDeleteState] = useState<{
    role: RoleInput;
    impact: TeamRoleDeleteImpact;
    reassignRole: string;
    confirmName: string;
  } | null>(null);
  useEffect(() => {
    if (mappingQueries.some((query) => !query.isSuccess)) return;
    const byRole = new Map(mappingQueries.map((query) => [query.data!.team_role, query.data!]));
    setRoles((current) => current.map((role) => {
      const mapping = byRole.get(role.role);
      if (!mapping) return role;
      return {
        ...role,
        ram_role_ids: mapping.ram_role_ids,
        ram_role_version: mapping.version,
      };
    }));
  }, [mappingQueries.map((query) => `${query.data?.team_role ?? ''}:${query.data?.version ?? ''}:${query.data?.ram_role_ids.join('|') ?? ''}`).join('\n')]);
  const originalRoles = team.roles.map((role) => ({
    role: role.role,
    cli: role.cli,
    model: role.model,
    max_concurrency: role.max_concurrency,
    count: role.count ?? 1,
    tags: role.capability_tags.join(', '),
    ram_role_keys: role.ram_role_keys ?? [],
    ram_role_ids: mappingQueries.find((query) => query.data?.team_role === role.role)?.data?.ram_role_ids ?? [],
    ram_role_version: mappingQueries.find((query) => query.data?.team_role === role.role)?.data?.version,
    access_requirements: role.access_requirements ?? [],
  }));
  const runtimeChangedRoles = roleDiff(originalRoles, roles, 'runtime');
  const accessChangedRoles = roleDiff(originalRoles, roles, 'access');
  const changedRoles = panel === 'runtime' ? runtimeChangedRoles : accessChangedRoles;
  const affectedMembers = affectedMembersForRoles(members.data ?? [], changedRoles.map((change) => change.role));
  const names = roles.map((role) => role.role.trim());
  const invalid = names.some((name) => !name) || new Set(names).size !== names.length;
  const hasRuntimeRoles = roles.length > 0;
  const runtimeSelectionValid = !hasRuntimeRoles || roles.every((role) =>
    isSelectableRuntimePair(runtimeCatalog.catalog, role.cli, role.model, 'model-key'),
  );
  const runtimeInvalid = hasRuntimeRoles && (runtimeCatalog.isLoading || Boolean(runtimeCatalog.error) || !runtimeSelectionValid);
  const mappingLoading = mappingQueries.some((query) => query.isLoading);
  const mappingError = mappingQueries.find((query) => query.isError)?.error as Error | undefined;
  const buildPatchRoles = (normalizedRoles: RoleInput[], mode: 'runtime' | 'access') => normalizedRoles.map((role) => {
    const original = originalRoles.find((r) => r.role === role.role);
    return {
      role: role.role,
      cli: mode === 'runtime' ? role.cli : original?.cli ?? role.cli,
      model: mode === 'runtime' ? role.model : original?.model ?? role.model,
      max_concurrency: mode === 'runtime' ? role.max_concurrency : original?.max_concurrency ?? role.max_concurrency,
      count: mode === 'runtime' ? role.count : original?.count ?? role.count,
      tags: mode === 'runtime' ? role.tags : original?.tags ?? role.tags,
      description: role.description,
      ram_role_keys: mode === 'access' ? role.ram_role_keys : original?.ram_role_keys ?? role.ram_role_keys,
      access_requirements: mode === 'access' ? role.access_requirements : original?.access_requirements ?? role.access_requirements,
      access_lint: role.access_lint,
    };
  });
  const saveRuntime = async () => {
    if (invalid || runtimeInvalid || mappingLoading) return;
    const normalizedRoles = roles.map((role) => ({ ...role, role: role.role.trim(), tags: role.tags.trim() }));
    await update.mutateAsync({
      team_id: team.id,
      roles: buildPatchRoles(normalizedRoles, 'runtime'),
      expected_version: team.version,
    });
    await readTeam(team.id);
    onSaved?.(t('teamDetail.roles.saveSuccess'));
    onClose();
  };
  const previewAccess = async () => {
    if (invalid || mappingLoading) return;
    const normalizedRoles = roles.map((role) => ({ ...role, role: role.role.trim(), tags: role.tags.trim() }));
    const impacts: TeamRAMRoleMappingImpact[] = [];
    for (const role of normalizedRoles) {
      const original = originalRoles.find((r) => r.role === role.role);
      const nextIds = uniqueSorted(role.ram_role_ids ?? []);
      const originalIds = uniqueSorted(original?.ram_role_ids ?? []);
      if (original && originalIds.join('|') === nextIds.join('|')) continue;
      if (!original && nextIds.length === 0) continue;
      impacts.push(await previewRAMRoles.mutateAsync({ team_id: team.id, role: role.role, ram_role_ids: nextIds }));
    }
    setPendingAccess({ roles: normalizedRoles, impacts });
  };
  const applyAccess = async () => {
    if (!pendingAccess) return;
    await update.mutateAsync({
      team_id: team.id,
      roles: buildPatchRoles(pendingAccess.roles, 'access'),
      expected_version: team.version,
    });
    for (const role of pendingAccess.roles) {
      const impact = pendingAccess.impacts.find((item) => item.team_role === role.role);
      if (!impact) continue;
      const nextIds = uniqueSorted(role.ram_role_ids ?? []);
      await replaceRAMRoles.mutateAsync({
        team_id: team.id,
        role: role.role,
        ram_role_ids: nextIds,
        expected_version: impact.version,
      });
    }
    await readTeam(team.id);
    setPendingAccess(null);
    onSaved?.(t('teamDetail.roles.saveSuccess'));
    onClose();
  };
  const duplicateRoleAt = (index: number) => {
    const source = roles[index];
    const names = new Set(roles.map((role) => role.role.trim()));
    let copyName = `${source.role.trim() || 'role'}-copy`;
    let n = 2;
    while (names.has(copyName)) {
      copyName = `${source.role.trim() || 'role'}-copy-${n}`;
      n += 1;
    }
    setRoles([...roles.slice(0, index + 1), { ...source, role: copyName, count: 1 }, ...roles.slice(index + 1)]);
  };
  const removeRoleAt = async (index: number) => {
    const target = roles[index];
    const targetName = target.role.trim();
    const existed = originalRoles.some((role) => role.role === targetName);
    if (!existed) {
      setRoles(roles.filter((_, i) => i !== index));
      return;
    }
    const impact = await previewDeleteRole.mutateAsync({ team_id: team.id, role: targetName });
    const fallback = roles.find((role) => role.role.trim() !== targetName)?.role.trim() ?? '';
    setDeleteState({ role: target, impact, reassignRole: fallback, confirmName: '' });
  };
  const applyDeleteRole = async () => {
    if (!deleteState) return;
    await deleteRole.mutateAsync({
      team_id: team.id,
      role: deleteState.role.role.trim(),
      expected_version: deleteState.impact.version,
      reassign_role: deleteState.reassignRole,
      confirm_name: deleteState.confirmName,
    });
    await readTeam(team.id);
    onSaved?.(t('teamDetail.roles.deleteSuccess', { role: deleteState.role.role.trim() }));
    setDeleteState(null);
    onClose();
  };
  const deleteReassignOptions = roles.map((role) => role.role.trim()).filter((name) => name && name !== deleteState?.role.role.trim());
  return <ModalShell open onClose={onClose} wide testId="edit-team-roles-modal" title={t('teamDetail.roles.title')}
    subtitle={t('teamDetail.roles.subtitle')} footer={<div className="ml-auto flex gap-2.5">
      <button type="button" className={btnGhost} onClick={onClose}>{t('common.cancel')}</button>
      {panel === 'runtime' ? (
        <button type="button" className={btnSmPrimary} disabled={invalid || runtimeInvalid || mappingLoading || update.isPending} data-testid="team-save-roles" onClick={() => void saveRuntime()}>{t('teamDetail.roles.saveRuntime')}</button>
      ) : (
        <button type="button" className={btnSmPrimary} disabled={invalid || mappingLoading || update.isPending || previewRAMRoles.isPending || replaceRAMRoles.isPending} data-testid="team-preview-access" onClick={() => void previewAccess()}>{t('teamDetail.roles.previewAccess')}</button>
      )}
    </div>}>
    <div className="mb-3 inline-flex rounded border border-border-base bg-bg-elevated p-0.5" role="tablist" aria-label="Team Role editor sections">
      <button type="button" role="tab" aria-selected={panel === 'runtime'} className={panel === 'runtime' ? btnSmPrimary : btnSm} data-testid="team-role-runtime-panel" onClick={() => setPanel('runtime')}>
        {t('teamDetail.roles.runtimePanel')}
      </button>
      <button type="button" role="tab" aria-selected={panel === 'access'} className={panel === 'access' ? btnSmPrimary : btnSm} data-testid="team-role-access-panel" onClick={() => setPanel('access')}>
        {t('teamDetail.roles.accessPanel')}
      </button>
    </div>
    {panel === 'runtime' ? (
      <RoleBuilder
        roles={roles}
        onChange={setRoles}
        showCount={false}
        idPrefix="edit-team"
        ramRoleMode="ids"
        showAccess={false}
        onDuplicateRole={duplicateRoleAt}
        onRemoveRole={(index) => void removeRoleAt(index)}
        canRemoveRole={() => roles.length > 1}
      />
    ) : (
      <RoleBuilder roles={roles} onChange={setRoles} showCount={false} idPrefix="edit-team-access" ramRoleMode="ids" showRuntime={false} showTags={false} showAccess allowStructureEdit={false} />
    )}
    <div className="mt-3 rounded border border-border-base bg-bg-subtle p-3" data-testid="team-role-save-preview">
      <div className="mb-2 flex items-center justify-between gap-2">
        <span className="text-xs font-semibold text-text-primary">{t('teamDetail.roles.previewTitle')}</span>
        <span className="text-[0.6875rem] text-text-muted">{t('teamDetail.roles.affectedSummary', { roles: changedRoles.length, members: affectedMembers.length })}</span>
      </div>
      <p className="mb-2 text-[0.6875rem] leading-5 text-text-secondary" data-testid="team-role-effective-hint">
        {t('teamDetail.roles.effectiveHint')}
      </p>
      {changedRoles.length === 0 ? (
        <p className="text-xs text-text-muted" data-testid="team-role-diff-empty">{t('teamDetail.roles.diffEmpty')}</p>
      ) : (
        <ul className="space-y-1" data-testid="team-role-diff-list">
          {changedRoles.map((change) => (
            <li key={change.role} className="text-xs text-text-secondary">
              <span className="font-semibold text-text-primary">{change.role}</span>
              <span className="ml-2 font-mono text-[0.6875rem]">{change.details.join(' / ')}</span>
            </li>
          ))}
        </ul>
      )}
      <div className="mt-2 flex flex-wrap gap-1" data-testid="team-role-affected-members">
        {affectedMembers.length === 0 ? (
          <span className="text-[0.6875rem] text-text-muted">{t('teamDetail.roles.noAffectedMembers')}</span>
        ) : affectedMembers.map((member) => (
          <span key={member.member_ref} className="rounded border border-border-base bg-bg-base px-1.5 py-0.5 text-[0.6875rem] text-text-secondary">
            {member.name || member.member_ref}
          </span>
        ))}
      </div>
    </div>
    {invalid && <p className="mt-3 text-xs text-danger" role="alert">{t('teamDetail.roles.invalid')}</p>}
    {runtimeInvalid && (
      <p className="mt-3 text-xs text-danger" role="alert" data-testid="edit-team-runtime-validation-error">
        {runtimeCatalog.isLoading
          ? t('roleBuilder.runtimeCatalogLoading')
          : runtimeCatalog.error
            ? t('roleBuilder.runtimeCatalogUnavailable')
            : t('roleBuilder.runtimeSelectionRequired')}
      </p>
    )}
    {update.isError && <p className="mt-3 text-xs text-danger" role="alert">{(update.error as Error).message}</p>}
    {mappingError && <p className="mt-3 text-xs text-danger" role="alert">{mappingError.message}</p>}
    {previewRAMRoles.isError && <p className="mt-3 text-xs text-danger" role="alert">{(previewRAMRoles.error as Error).message}</p>}
    {replaceRAMRoles.isError && <p className="mt-3 text-xs text-danger" role="alert">{(replaceRAMRoles.error as Error).message}</p>}
    {previewDeleteRole.isError && <p className="mt-3 text-xs text-danger" role="alert">{(previewDeleteRole.error as Error).message}</p>}
    {deleteRole.isError && <p className="mt-3 text-xs text-danger" role="alert">{(deleteRole.error as Error).message}</p>}
    <ConfirmModal
      open={pendingAccess !== null}
      title={t('teamDetail.roles.confirmAccessTitle')}
      message={
        <div className="space-y-2" data-testid="team-access-confirm-diff">
          <p>{t('teamDetail.roles.confirmAccessMessage', { roles: accessChangedRoles.length, members: affectedMembers.length })}</p>
          <ul className="space-y-1">
            {(pendingAccess?.impacts ?? []).map((impact) => (
              <li key={impact.team_role} className="font-mono text-xs">
                {impact.team_role}: {impact.current_ram_role_ids.join(', ') || 'none'} → {impact.next_ram_role_ids.join(', ') || 'none'} · {impact.affected_members} members · {impact.affected_project_ids.length} projects
              </li>
            ))}
          </ul>
        </div>
      }
      confirmLabel={t('teamDetail.roles.confirmAccessApply')}
      danger
      busy={update.isPending || replaceRAMRoles.isPending}
      onCancel={() => setPendingAccess(null)}
      onConfirm={() => void applyAccess()}
    />
    <ConfirmModal
      open={deleteState !== null}
      title={t('teamDetail.roles.deleteTitle', { role: deleteState?.role.role })}
      message={deleteState ? (
        <div className="space-y-3" data-testid="team-role-delete-confirm">
          <p>{t('teamDetail.roles.deleteImpact', { members: deleteState.impact.affected_members, projects: deleteState.impact.affected_project_ids.length })}</p>
          {deleteState.impact.protected && <p className="font-semibold text-danger">{t('teamDetail.roles.deleteProtected')}</p>}
          <Field label={t('teamDetail.roles.reassignLabel')}>
            <select
              className={inputCls}
              value={deleteState.reassignRole}
              data-testid="team-role-delete-reassign"
              onChange={(event) => setDeleteState({ ...deleteState, reassignRole: event.target.value })}
              disabled={deleteState.impact.protected}
            >
              {deleteReassignOptions.map((role) => <option key={role} value={role}>{role}</option>)}
            </select>
          </Field>
          <Field label={t('teamDetail.roles.confirmNameLabel', { role: deleteState.role.role })}>
            <input
              className={inputCls}
              value={deleteState.confirmName}
              data-testid="team-role-delete-confirm-name"
              onChange={(event) => setDeleteState({ ...deleteState, confirmName: event.target.value })}
              disabled={deleteState.impact.protected}
            />
          </Field>
        </div>
      ) : undefined}
      confirmLabel={t('teamDetail.roles.deleteConfirm')}
      danger
      busy={deleteRole.isPending}
      confirmDisabled={Boolean(deleteState?.impact.protected) || !deleteState?.reassignRole || deleteState?.confirmName.trim() !== deleteState?.role.role.trim()}
      onCancel={() => setDeleteState(null)}
      onConfirm={() => void applyDeleteRole()}
    />
  </ModalShell>;
}

function MembersPane({
  teamId,
  roleOptions,
  team,
}: {
  teamId: string;
  roleOptions: string[];
  team: TeamView;
}): React.ReactElement {
  const { t } = useTranslation('teams');
  const members = useTeamMembers(teamId);
  const remove = useRemoveMember();
  const [adding, setAdding] = useState(false);
  const [configuringAccess, setConfiguringAccess] = useState(false);
  const [removingRef, setRemovingRef] = useState<string | null>(null);
  const [removeError, setRemoveError] = useState('');

  return (
    <div>
      <SectionHead
        title={t('teamDetail.members.title')}
        action={
          <div className="flex flex-wrap gap-2">
            <button type="button" className={btnSm} data-testid="members-configure-access" onClick={() => setConfiguringAccess(true)}>
              {t('teamDetail.members.configureAccess')}
            </button>
            <button type="button" className={btnSmPrimary} data-testid="members-add" onClick={() => setAdding(true)}>
              {t('teamDetail.members.addMember')}
            </button>
          </div>
        }
      />
      <Note testId="members-exclusivity-note">
        <Trans i18nKey="teamDetail.members.exclusivityNote" ns="teams" components={{ b: <b /> }} />
      </Note>

      {members.isLoading && <Skeleton height="8rem" />}
      {members.isSuccess && members.data.length === 0 && (
        <EmptyState title={t('teamDetail.members.emptyTitle')} body={t('teamDetail.members.emptyBody')} testId="members-empty" />
      )}
      {members.isSuccess && members.data.length > 0 && (
        <div className="overflow-hidden rounded-lg border border-border-base">
          <table className="w-full text-sm" data-testid="members-table">
            <thead>
              <tr className="border-b border-border-base text-left text-[0.6875rem] uppercase tracking-wide text-text-muted">
                <th className="px-4 py-3 font-semibold">{t('teamDetail.members.colMember')}</th>
                <th className="px-4 py-3 font-semibold">{t('teamDetail.members.colKind')}</th>
                <th className="px-4 py-3 font-semibold">{t('teamDetail.members.colDeclaredRole')}</th>
                <th className="px-4 py-3 font-semibold">{t('teamDetail.members.colCliModel')}</th>
                <th className="px-4 py-3 font-semibold">{t('teamDetail.members.colCapabilities')}</th>
                <th className="px-4 py-3 font-semibold">{t('teamDetail.members.colConcurrency')}</th>
                <th className="px-4 py-3 font-semibold">{t('teamDetail.members.colPermissionSource')}</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {members.data.map((m) => {
                const memberRoles = m.roles?.length ? m.roles : [m.role];
                const inheritedPermissions = uniqueSorted(memberRoles.flatMap((role) =>
                  team.roles.find((r) => r.role === role)?.access_requirements ?? [],
                ));
                const inheritedRamRoles = uniqueSorted(memberRoles.flatMap((role) =>
                  team.roles.find((r) => r.role === role)?.ram_role_keys ?? [],
                ));
                return (
                  <tr key={m.member_ref} data-testid={`member-row-${m.member_ref}`} className="border-b border-border-base last:border-0">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2.5">
                        <Glyph text={m.name[0]?.toUpperCase() ?? '?'} size="sm" kind={m.kind} />
                        <div>
                          <div className="font-semibold text-text-primary">{m.name}</div>
                          <div className="font-mono text-[0.6875rem] text-text-muted">{m.member_ref}</div>
                        </div>
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <KindTag kind={m.kind} />
                      {m.exclusive && <span className="ml-1.5 text-[0.625rem] font-semibold text-warning">{t('teamDetail.members.exclusiveTag')}</span>}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex flex-wrap gap-1.5">
                        {memberRoles.map((role) => (
                          <span key={role} className="rounded border border-border-base bg-bg-subtle px-2 py-0.5 text-xs font-semibold" style={roleColorChip(role)}>
                            {role}
                          </span>
                        ))}
                      </div>
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-text-muted">
                      {m.cli} · {m.model}
                    </td>
                    <td className="px-4 py-3">
                      <CapabilityPills capabilities={m.tags} />
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-text-muted">{m.concurrency}</td>
                    <td className="px-4 py-3">
                      <div className="max-w-[18rem] text-xs text-text-secondary" data-testid={`member-permission-source-${m.member_ref}`}>
                        <div className="font-semibold text-text-primary">{t('teamDetail.members.sourceTeamRole')}</div>
                        <div className="font-mono text-[0.6875rem] text-text-muted">{t('teamDetail.members.scopeTeam', { teamId })}</div>
                        <div className="mt-1 flex flex-wrap gap-1">
                          {inheritedRamRoles.length > 0
                            ? inheritedRamRoles.map((ramRole) => <span key={ramRole} className="rounded border border-border-base bg-bg-subtle px-1.5 py-0.5 font-mono text-[0.625rem]">{ramRole}</span>)
                            : inheritedPermissions.slice(0, 3).map((permission) => <span key={permission} className="rounded border border-border-base bg-bg-subtle px-1.5 py-0.5 font-mono text-[0.625rem]">{permission}</span>)}
                          {inheritedRamRoles.length === 0 && inheritedPermissions.length === 0 && <span className="text-text-muted">—</span>}
                        </div>
                      </div>
                    </td>
                    <td className="px-4 py-3 text-right">
                      <button type="button" className={btnSmDanger} data-testid={`member-remove-${m.member_ref}`} onClick={() => { setRemoveError(''); setRemovingRef(m.member_ref); }}>
                        {t('teamDetail.members.remove')}
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {adding && (
        <AddMemberModal team={team} roleOptions={roleOptions} onClose={() => setAdding(false)} onAdded={() => setAdding(false)} />
      )}
      {configuringAccess && (
        <EditRolesModal team={team} initialPanel="access" onClose={() => setConfiguringAccess(false)} />
      )}
      <ConfirmModal
        open={removingRef !== null}
        title={t('teamDetail.members.removeTitle')}
        message={removingRef ? t('teamDetail.members.removeMessage', { ref: removingRef }) : undefined}
        confirmLabel={t('teamDetail.members.removeConfirm')}
        danger
        busy={remove.isPending}
        onCancel={() => { setRemovingRef(null); setRemoveError(''); }}
        onConfirm={async () => {
          if (!removingRef) return;
          try {
            await remove.mutateAsync({ team_id: teamId, member_ref: removingRef });
            setRemovingRef(null);
            setRemoveError('');
          } catch (err) {
            setRemoveError((err as Error).message);
          }
        }}
      />
      {removeError && (
        <p className="mt-3 text-xs text-danger" role="alert" data-testid="member-remove-error">
          {removeError}
        </p>
      )}
    </div>
  );
}

function CapabilityPills({ capabilities }: { capabilities: string[] }): React.ReactElement {
  if (capabilities.length === 0) {
    return <span className="text-text-muted">—</span>;
  }
  const visible = capabilities.slice(0, 3);
  const hidden = capabilities.slice(3);
  return (
    <div className="flex max-w-[34rem] flex-wrap gap-1" data-testid="member-capabilities">
      {visible.map((capability) => (
        <span
          key={capability}
          title={capability}
          className="max-w-[13rem] truncate rounded border border-border-base bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] text-text-secondary"
        >
          {capability}
        </span>
      ))}
      {hidden.length > 0 && (
        <span
          title={hidden.join(' / ')}
          className="rounded border border-border-base bg-bg-muted px-1.5 py-0.5 text-[0.625rem] font-semibold text-text-secondary"
        >
          +{hidden.length}
        </span>
      )}
    </div>
  );
}

function roleSignature(role: RoleInput, mode: 'runtime' | 'access' | 'all' = 'all'): string {
  return JSON.stringify({
    role: role.role.trim(),
    ...(mode !== 'access' ? {
      cli: role.cli,
      model: role.model,
      max_concurrency: role.max_concurrency,
      count: role.count,
      tags: uniqueSorted(role.tags.split(/[\s,]+/)),
    } : {}),
    ...(mode !== 'runtime' ? {
      ram_role_keys: uniqueSorted(role.ram_role_keys ?? []),
      ram_role_ids: uniqueSorted(role.ram_role_ids ?? []),
      access_requirements: uniqueSorted(role.access_requirements ?? []),
    } : {}),
  });
}

function roleDiff(before: RoleInput[], after: RoleInput[], mode: 'runtime' | 'access' | 'all' = 'all'): Array<{ role: string; details: string[] }> {
  const beforeByName = new Map(before.map((role) => [role.role.trim(), role]));
  const afterByName = new Map(after.map((role) => [role.role.trim(), role]));
  const names = uniqueSorted([...beforeByName.keys(), ...afterByName.keys()]);
  return names.flatMap((name) => {
    const prev = beforeByName.get(name);
    const next = afterByName.get(name);
    if (!prev && next) return [{ role: name, details: ['added'] }];
    if (prev && !next) return [{ role: name, details: ['removed'] }];
    if (!prev || !next || roleSignature(prev, mode) === roleSignature(next, mode)) return [];
    const details: string[] = [];
    if (mode !== 'access' && (prev.cli !== next.cli || prev.model !== next.model || prev.max_concurrency !== next.max_concurrency || prev.count !== next.count)) details.push('runtime');
    if (mode !== 'access' && uniqueSorted(prev.tags.split(/[\s,]+/)).join('|') !== uniqueSorted(next.tags.split(/[\s,]+/)).join('|')) details.push('capabilities');
    if (mode !== 'runtime' && uniqueSorted(prev.ram_role_ids ?? []).join('|') !== uniqueSorted(next.ram_role_ids ?? []).join('|')) details.push('RAM roles');
    if (mode !== 'runtime' && uniqueSorted(prev.access_requirements ?? []).join('|') !== uniqueSorted(next.access_requirements ?? []).join('|')) details.push('permissions');
    return [{ role: name, details }];
  });
}

function affectedMembersForRoles(members: MemberView[], roles: string[]): MemberView[] {
  if (roles.length === 0) return [];
  const changed = new Set(roles);
  return members.filter((member) => (member.roles?.length ? member.roles : [member.role]).some((role) => changed.has(role)));
}

function uniqueSorted(values: string[]): string[] {
  return Array.from(new Set(values.map((value) => value.trim()).filter(Boolean))).sort();
}

function ProjectsPane({ teamId }: { teamId: string }): React.ReactElement {
  const { t } = useTranslation('teams');
  const projects = useTeamProjects(teamId);
  const disassociate = useDisassociateProject();
  const [unlinkId, setUnlinkId] = useState<string | null>(null);
  const [picking, setPicking] = useState(false);

  return (
    <div>
      <SectionHead
        title={t('teamDetail.projects.title')}
        action={
          <button
            type="button"
            className={btnSm}
            data-testid="associate-project"
            onClick={() => setPicking(true)}
          >
            {t('teamDetail.projects.associate')}
          </button>
        }
      />
      {projects.isLoading && <Skeleton height="6rem" />}
      {projects.isSuccess && projects.data.length === 0 && (
        <EmptyState title={t('teamDetail.projects.emptyTitle')} body={t('teamDetail.projects.emptyBody')} testId="projects-empty" />
      )}
      {(projects.data ?? []).map((p) => (
        <div key={p.project_id} data-testid={`assoc-${p.project_id}`} className="mb-2.5 flex items-center gap-3 rounded-lg border border-border-base bg-bg-elevated px-3.5 py-3 shadow-1">
          <Glyph text={p.glyph} kind="human" />
          <div className="flex-1">
            <b className="font-semibold text-text-primary">{p.name}</b>
            <span className="block font-mono text-[0.6875rem] text-text-muted">
              {p.project_id} · {p.repo}
            </span>
          </div>
          <span
            className={[
              'rounded px-2 py-0.5 text-[0.65rem] font-semibold',
              p.relation === 'primary' ? 'bg-success/15 text-success' : 'border border-border-base bg-bg-subtle text-text-muted',
            ].join(' ')}
          >
            {p.relation}
          </span>
          <button type="button" className={btnSm} data-testid={`unlink-${p.project_id}`} onClick={() => setUnlinkId(p.project_id)}>
            {t('teamDetail.projects.unlink')}
          </button>
        </div>
      ))}
      {projects.isSuccess && projects.data.length > 0 && (
        <Note>{t('teamDetail.projects.note')}</Note>
      )}
      <ConfirmModal
        open={unlinkId !== null}
        title={t('teamDetail.projects.unlinkTitle')}
        message={t('teamDetail.projects.unlinkMessage')}
        confirmLabel={t('teamDetail.projects.unlinkConfirm')}
        danger
        busy={disassociate.isPending}
        onCancel={() => setUnlinkId(null)}
        onConfirm={async () => {
          if (unlinkId) await disassociate.mutateAsync({ team_id: teamId, project_id: unlinkId });
          setUnlinkId(null);
        }}
      />
      {picking && (
        <ProjectPickerModal
          teamId={teamId}
          linkedIds={new Set((projects.data ?? []).map((p) => p.project_id))}
          onClose={() => setPicking(false)}
        />
      )}
    </div>
  );
}

// Real associate-project picker: lists the org's active projects (GET /projects)
// minus the ones already linked to this team, and associates the SELECTED project
// with its true {project_id, name} — no fabricated `project-N` / 'new-project'.
function ProjectPickerModal({
  teamId,
  linkedIds,
  onClose,
}: {
  teamId: string;
  linkedIds: Set<string>;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('teams');
  const all = useProjects();
  const associate = useAssociateProject();
  const candidates = (all.data ?? []).filter((p) => !linkedIds.has(p.id));
  const [selected, setSelected] = useState('');
  const chosen = candidates.find((p) => p.id === selected);

  const submit = async () => {
    if (!chosen) return;
    try {
      await associate.mutateAsync({ team_id: teamId, project_id: chosen.id, name: chosen.name });
      onClose();
    } catch {
      /* surfaced via error */
    }
  };

  return (
    <ModalShell
      open
      onClose={onClose}
      testId="associate-project-modal"
      title={t('teamDetail.projects.pickerTitle')}
      subtitle={t('teamDetail.projects.pickerSubtitle')}
      footer={
        <>
          <span />
          <div className="flex gap-2.5">
            <button type="button" className={btnGhost} onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button
              type="button"
              className={btnSmPrimary}
              data-testid="associate-project-submit"
              disabled={!chosen || associate.isPending}
              onClick={submit}
            >
              {associate.isPending ? t('teamDetail.projects.associating') : t('teamDetail.projects.associateAction')}
            </button>
          </div>
        </>
      }
    >
      {all.isLoading && <Skeleton height="4rem" />}
      {all.isSuccess && candidates.length === 0 && (
        <EmptyState
          title={t('teamDetail.projects.pickerEmptyTitle')}
          body={t('teamDetail.projects.pickerEmptyBody')}
          testId="associate-project-empty"
        />
      )}
      {candidates.length > 0 && (
        <Field label={t('teamDetail.projects.selectLabel')} required>
          <select
            className={inputCls}
            value={selected}
            data-testid="associate-project-select"
            onChange={(e) => setSelected(e.target.value)}
          >
            <option value="">{t('teamDetail.projects.selectPlaceholder')}</option>
            {candidates.map((p) => (
              <option key={p.id} value={p.id}>
                {t('teamDetail.projects.selectOption', { name: p.name, id: p.id })}
              </option>
            ))}
          </select>
        </Field>
      )}
      {associate.isError && <p className="mt-2 text-xs text-danger">{(associate.error as Error).message}</p>}
    </ModalShell>
  );
}

function TeamSettingsPane({ team }: { team: TeamView }): React.ReactElement {
  const { t } = useTranslation('teams');
  const settings = useTeamMemorySettings(team.id);
  const agents = useDirectoryAgents();
  const update = useUpdateTeamMemorySettings(team.id);
  const canManage = team.memory_permissions?.can_manage === true;
  const [policy, setPolicy] = useState<TeamMemoryPolicy>('owner_admin_review');
  const [curators, setCurators] = useState<string[]>([]);
  const [saveSucceeded, setSaveSucceeded] = useState(false);
  const curatorPolicyActive = policy === 'curator_review';

  useEffect(() => {
    if (!settings.data) return;
    setPolicy(settings.data.policy);
    setCurators(settings.data.curator_agents ?? []);
  }, [settings.data]);

  const clearSaveFeedback = () => {
    setSaveSucceeded(false);
    update.reset();
  };

  const save = async () => {
    setSaveSucceeded(false);
    try {
      await update.mutateAsync({
        policy,
        curator_agents: curatorPolicyActive ? curators : [],
      });
      setSaveSucceeded(true);
    } catch {
      setSaveSucceeded(false);
    }
  };
  const curatorOptions = (agents.data ?? []).map((agent) => ({
    value: agent.ref,
    label: `${agent.name} · ${agent.ref}`,
  }));

  return (
    <div className="grid gap-3.5 lg:grid-cols-[minmax(0,1fr)_minmax(18rem,24rem)]">
      <Card testId="team-memory-settings">
        <SectionHead title={t('teamDetail.settings.title')} hint={canManage ? t('teamDetail.settings.manageHint') : t('teamDetail.settings.readOnlyHint')} />
        <Note testId="team-settings-effect-hint">
          {settings.data?.effect_hint || t('memoryPane.effectHint')}
        </Note>
        {settings.isLoading && <Skeleton height="8rem" />}
        {settings.isSuccess && (
          <>
            <Field label={t('teamDetail.settings.policy')}>
              <select
                className={inputCls}
                value={policy}
                disabled={!canManage}
                onChange={(e) => {
                  clearSaveFeedback();
                  setPolicy(e.target.value as TeamMemoryPolicy);
                }}
                data-testid="team-memory-policy"
                aria-describedby="team-memory-policy-description"
              >
                <option value="owner_admin_review">{t('teamDetail.settings.policyOwnerAdmin')}</option>
                <option value="curator_review">{t('teamDetail.settings.policyCurator')}</option>
              </select>
              <p id="team-memory-policy-description" data-testid="team-memory-policy-description" className="mt-2 text-xs leading-5 text-text-secondary">
                {t(`teamDetail.settings.policyDescriptions.${policy}`)}
              </p>
            </Field>
            <Field label={t('teamDetail.settings.curators')}>
              {agents.isLoading && <Skeleton height="4rem" />}
              {agents.isSuccess && agents.data.length === 0 && (
                <div className="rounded border border-border-base bg-bg-subtle p-3 text-sm text-text-muted">{t('teamDetail.settings.noAgents')}</div>
              )}
              {agents.isSuccess && agents.data.length > 0 && (
                <EntityMultiSelect
                  testId="team-memory-curator-picker"
                  options={curatorOptions}
                  values={curators}
                  onChange={(values) => {
                    clearSaveFeedback();
                    setCurators([...values].sort());
                  }}
                  ariaLabel={t('teamDetail.settings.curators')}
                  placeholder={t('teamDetail.settings.curators')}
                  disabled={!canManage || !curatorPolicyActive}
                />
              )}
              <p className="mt-2 text-xs leading-5 text-text-muted" data-testid="team-memory-curators-help">
                {curatorPolicyActive ? t('teamDetail.settings.curatorsHelpActive') : t('teamDetail.settings.curatorsHelpInactive')}
              </p>
            </Field>
            {saveSucceeded && (
              <div role="status" data-testid="team-memory-settings-success" className="mt-4 rounded-md border border-success/30 bg-success/10 px-3 py-2 text-sm text-success">
                {t('teamDetail.settings.saveSuccess')}
              </div>
            )}
            {update.isError && (
              <div role="alert" data-testid="team-memory-settings-error" className="mt-4 rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">
                {t('teamDetail.settings.saveError', { message: (update.error as Error).message })}
              </div>
            )}
            <div className="mt-4 flex items-center justify-between gap-3">
              <div className="text-xs text-text-muted" data-testid="team-memory-settings-meta">
                {settings.data?.commit ? t('teamDetail.settings.commit', { commit: settings.data.commit.slice(0, 12) }) : t('teamDetail.settings.noCommit')}
              </div>
              <button type="button" className={btnSmPrimary} disabled={!canManage || update.isPending} onClick={() => void save()} data-testid="team-memory-settings-save">
                {t('teamDetail.settings.save')}
              </button>
            </div>
          </>
        )}
        {settings.isError && <p className="text-sm text-danger">{(settings.error as Error).message}</p>}
      </Card>
      <Card>
        <SectionHead title={t('teamDetail.settings.guardrails')} />
        <div className="space-y-2 text-sm text-text-secondary">
          <p>{t('teamDetail.settings.agentSelfGrant')}</p>
          <p>{t('teamDetail.settings.reviewSurface')}</p>
        </div>
      </Card>
    </div>
  );
}

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
  useRemoveMember,
  useTeam,
  useTeamMemoryIndex,
  useTeamMemorySettings,
  useTeamMembers,
  useTeamProjects,
  useUpdateTeamMemorySettings,
  useUpdateTeamRoles,
  type RoleInput,
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
            <SpecLine
              key={r.role}
              k={
                <span className="flex items-center gap-1.5">
                  <span className="h-2 w-2 rounded-sm" style={{ background: roleColor(r.role) }} aria-hidden="true" />
                  {r.role}
                </span>
              }
              v={t('teamDetail.overview.roleSpec', { cli: r.cli, model: r.model, conc: r.max_concurrency })}
            />
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
      </Card>
      {editingRoles && <EditRolesModal team={tv} onClose={() => setEditingRoles(false)} />}
    </div>
  );
}

function EditRolesModal({ team, onClose }: { team: TeamView; onClose: () => void }): React.ReactElement {
  const { t } = useTranslation('teams');
  const update = useUpdateTeamRoles();
  const runtimeCatalog = useRuntimeSelectorCatalog();
  const [roles, setRoles] = useState<RoleInput[]>(() => team.roles.map((role) => ({
    role: role.role,
    cli: role.cli,
    model: role.model,
    max_concurrency: role.max_concurrency,
    count: role.count ?? 1,
    tags: role.capability_tags.join(', '),
  })));
  const names = roles.map((role) => role.role.trim());
  const invalid = names.some((name) => !name) || new Set(names).size !== names.length;
  const hasRuntimeRoles = roles.length > 0;
  const runtimeSelectionValid = !hasRuntimeRoles || roles.every((role) =>
    isSelectableRuntimePair(runtimeCatalog.catalog, role.cli, role.model, 'model-key'),
  );
  const runtimeInvalid = hasRuntimeRoles && (runtimeCatalog.isLoading || Boolean(runtimeCatalog.error) || !runtimeSelectionValid);
  const save = async () => {
    if (invalid || runtimeInvalid) return;
    await update.mutateAsync({ team_id: team.id, roles: roles.map((role) => ({ ...role, role: role.role.trim(), tags: role.tags.trim() })) });
    onClose();
  };
  return <ModalShell open onClose={onClose} wide testId="edit-team-roles-modal" title={t('teamDetail.roles.title')}
    subtitle={t('teamDetail.roles.subtitle')} footer={<div className="ml-auto flex gap-2.5">
      <button type="button" className={btnGhost} onClick={onClose}>{t('common.cancel')}</button>
      <button type="button" className={btnSmPrimary} disabled={invalid || runtimeInvalid || update.isPending} data-testid="team-save-roles" onClick={() => void save()}>{t('teamDetail.roles.save')}</button>
    </div>}>
    <RoleBuilder roles={roles} onChange={setRoles} showCount={false} idPrefix="edit-team" />
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
  const [removingRef, setRemovingRef] = useState<string | null>(null);

  return (
    <div>
      <SectionHead
        title={t('teamDetail.members.title')}
        action={
          <button type="button" className={btnSmPrimary} data-testid="members-add" onClick={() => setAdding(true)}>
            {t('teamDetail.members.addMember')}
          </button>
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
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {members.data.map((m) => {
                const memberRoles = m.roles?.length ? m.roles : [m.role];
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
                    <td className="px-4 py-3 text-right">
                      <button type="button" className={btnSmDanger} data-testid={`member-remove-${m.member_ref}`} onClick={() => setRemovingRef(m.member_ref)}>
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
      <ConfirmModal
        open={removingRef !== null}
        title={t('teamDetail.members.removeTitle')}
        message={removingRef ? t('teamDetail.members.removeMessage', { ref: removingRef }) : undefined}
        confirmLabel={t('teamDetail.members.removeConfirm')}
        danger
        busy={remove.isPending}
        onCancel={() => setRemovingRef(null)}
        onConfirm={async () => {
          if (removingRef) await remove.mutateAsync({ team_id: teamId, member_ref: removingRef });
          setRemovingRef(null);
        }}
      />
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
  const [policy, setPolicy] = useState<'owner_admin_review' | 'curator_review' | 'read_only'>('owner_admin_review');
  const [curators, setCurators] = useState<string[]>([]);

  useEffect(() => {
    if (!settings.data) return;
    setPolicy(settings.data.policy);
    setCurators(settings.data.curator_agents ?? []);
  }, [settings.data]);

  const save = async () => {
    await update.mutateAsync({ policy, curator_agents: curators });
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
                onChange={(e) => setPolicy(e.target.value as typeof policy)}
                data-testid="team-memory-policy"
              >
                <option value="owner_admin_review">{t('teamDetail.settings.policyOwnerAdmin')}</option>
                <option value="curator_review">{t('teamDetail.settings.policyCurator')}</option>
                <option value="read_only">{t('teamDetail.settings.policyReadOnly')}</option>
              </select>
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
                  onChange={(values) => setCurators([...values].sort())}
                  ariaLabel={t('teamDetail.settings.curators')}
                  placeholder={t('teamDetail.settings.curators')}
                  disabled={!canManage}
                />
              )}
            </Field>
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
        {update.isError && <p className="mt-3 text-sm text-danger" role="alert">{(update.error as Error).message}</p>}
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

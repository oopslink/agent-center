import type React from 'react';
import { Fragment, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ApiError } from '@/api/client';
import {
  type AccessBatchItem,
  type AccessBatchGrantEntry,
  type AccessBatchPreview,
  type AccessBatchRequest,
  type AccessBatchResult,
  type AccessDecision,
  type AccessGrant,
  type AccessRole,
  type AccessResourceKind,
  type RAMRole,
  type RAMRoleDetail,
  type AccessPermissionDefinition,
  type AccessPermissionTemplate,
  type AccessResourceScope,
  type AccessRisk,
  type AccessStatus,
  type AccessSubject,
  type AccessSubjectKind,
  useAccessBatchApply,
  useAccessBatchPreview,
  useAccessBulkRevoke,
  useAccessOverview,
  useRAMRole,
  useRAMRoleCreate,
  useRAMRoleDelete,
  useRAMRoleNewVersion,
  useRAMRoleUpdate,
  useRAMRoles,
  useAccessRevokePreview,
} from '@/api/access';
import {
  hasEffectivePermission,
  type PermissionAuditEvent,
  type ResourceScope,
  useCurrentSubjectEffectivePermissions,
  usePermissionAudit,
  usePermissionExplain,
} from '@/api/permissions';
import { IconCalendar, IconClose, IconSearch, IconTrash } from '@/components/icons';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { useModalA11y } from '@/components/useModalA11y';
import { useAppStore } from '@/store/app';
import { OrgLink, useOptionalOrgContext } from '@/OrgContext';
import {
  useAllTeamMembers,
  useAllTeamRoleRAMMappings,
  useTeams,
  type TeamRAMRoleMapping,
  type TeamView,
} from '@/api/teams';
import { useProjects, type Project } from '@/api/projects';
import {
  AccessMetaPill,
  AccessRiskBadge,
  AccessStatusBadge,
  accessResourceKey,
  accessResourceLabel,
  displayAccessDate,
} from '@/components/access/kit';

export type AccessPage = 'ram-roles' | 'subject-access' | 'grant-access';
type AccessToast = { tone: 'success' | 'danger' | 'warning'; message: string } | null;
type SelectOption = string | { value: string; label: string };
type DirectGrantTemplate = {
  id: string;
  resource: string;
  scope: string;
  action: string;
  permissionKey: string;
  permissionKeyByResourceKind?: Partial<Record<AccessResourceKind, string>>;
  backendResourceKind: AccessResourceKind;
  compatibleKinds?: AccessResourceKind[];
  description: string;
  risk: AccessRisk;
};

type GrantEntry = {
  id: string;
  kind: 'role' | 'permission';
  roleId?: string;
  roleName?: string;
  permissionKey?: string;
  template?: DirectGrantTemplate;
  resource: AccessResourceScope;
  risk: AccessRisk;
};

type SemanticPermission = {
  label: string;
  resource: string;
  scope: string;
  action: string;
  implementationKey: string;
  backendResourceKind: string;
  description?: string;
};

type PermissionPickerRow = {
  id: string;
  kind: 'role' | 'permission';
  label: string;
  resource: string;
  permission: string;
  scope: string;
  action: string;
  detail: string;
  risk: AccessRisk;
  compatibleKinds: string[];
  role?: AccessRole;
  template?: DirectGrantTemplate;
};

const STATUS_OPTIONS: Array<AccessStatus | 'all'> = ['all', 'allowed', 'denied', 'unauthorized', 'not_applicable'];
const RISK_OPTIONS: Array<AccessRisk | 'all'> = ['all', 'high', 'medium', 'low'];
const SUBJECT_OPTIONS: Array<AccessSubjectKind | 'all'> = ['all', 'human', 'agent', 'team_role', 'worker', 'system'];
function emptyBatchRequest(subjectRef = ''): AccessBatchRequest {
  return {
    subject_refs: subjectRef ? [subjectRef] : [],
    role_ids: [],
    permission_keys: [],
    resources: [],
    entries: [],
    expires_at: '',
    reason: '',
  };
}

function uniqueResources(decisions: AccessDecision[], grants: AccessGrant[]): AccessResourceScope[] {
  const byKey = new Map<string, AccessResourceScope>();
  for (const d of decisions) byKey.set(accessResourceKey(d.resource), d.resource);
  for (const g of grants) byKey.set(accessResourceKey(g.resource), g.resource);
  return [...byKey.values()].sort((a, b) => accessResourceLabel(a).localeCompare(accessResourceLabel(b)));
}

function buildAccessResourceCatalog({
  decisions,
  grants,
  projects,
  teams,
  orgId,
  orgName,
}: {
  decisions: AccessDecision[];
  grants: AccessGrant[];
  projects: Project[];
  teams: TeamView[];
  orgId: string;
  orgName: string;
}): AccessResourceScope[] {
  const projectNameByID = new Map(projects.map((project) => [project.id, project.name]));
  const teamByID = new Map(teams.map((team) => [team.id, team]));
  const byKey = new Map<string, AccessResourceScope>();
  const add = (resource: AccessResourceScope): void => {
    const enriched = enrichAccessResourceLabel(resource, { orgId, orgName, projectNameByID, teamByID });
    byKey.set(accessResourceKey(enriched), enriched);
  };

  add({ kind: 'org', id: orgId, org_id: orgId, label: orgName });
  for (const project of projects) {
    add({
      kind: 'project',
      id: project.id,
      org_id: project.organization_id || orgId,
      project_id: project.id,
      label: project.name,
    });
  }
  for (const team of teams) {
    add({ kind: 'team', id: team.id, org_id: team.org_id || orgId, label: team.name });
  }
  for (const resource of uniqueResources(decisions, grants)) add(resource);

  return [...byKey.values()].sort((a, b) => {
    const kind = compareAccessResourceKind(a.kind, b.kind);
    return kind === 0 ? accessResourceLabel(a).localeCompare(accessResourceLabel(b)) : kind;
  });
}

function enrichAccessResourceLabel(
  resource: AccessResourceScope,
  context: {
    orgId: string;
    orgName: string;
    projectNameByID: Map<string, string>;
    teamByID: Map<string, TeamView>;
  },
): AccessResourceScope {
  if (resource.kind === 'org') {
    const label = resource.label && resource.label !== resource.id && resource.label !== resource.org_id
      ? resource.label
      : context.orgName;
    return {
      ...resource,
      id: resource.id || context.orgId,
      org_id: resource.org_id || context.orgId,
      label,
    };
  }
  const projectID = projectIDForAccessResource(resource);
  if (projectID) {
    const projectName = context.projectNameByID.get(projectID);
    return {
      ...resource,
      project_id: resource.project_id || projectID,
      label: resource.kind === 'project' ? (projectName || resource.label) : (resource.label || projectName),
    };
  }
  if (resource.kind === 'team') {
    const team = context.teamByID.get(resource.id);
    return {
      ...resource,
      org_id: resource.org_id || team?.org_id || context.orgId,
      label: team?.name || resource.label,
    };
  }
  return resource;
}

function uniqueAccessResources(resources: AccessResourceScope[]): AccessResourceScope[] {
  const byKey = new Map<string, AccessResourceScope>();
  for (const resource of resources) byKey.set(accessResourceKey(resource), resource);
  return [...byKey.values()];
}

function accessTestIDToken(value: string): string {
  return value.replace(/[^a-zA-Z0-9_-]+/g, '-').replace(/^-+|-+$/g, '');
}

function projectIDForAccessResource(resource: AccessResourceScope): string {
  if (resource.project_id) return resource.project_id;
  return resource.kind === 'project' ? resource.id : '';
}

function ramRoleScope(role: AccessRole): string {
  return role.scope_kind ?? 'mixed';
}

function assignableRAMRoles(roles: AccessRole[]): AccessRole[] {
  return roles.filter((role) => role.source === 'custom_role' || role.id.startsWith('sys-'));
}

function buildDirectGrantTemplates(permissions: AccessPermissionDefinition[]): DirectGrantTemplate[] {
  const templates: DirectGrantTemplate[] = [];
  const seen = new Set<string>();
  const addTemplate = (template: DirectGrantTemplate): void => {
    if (seen.has(template.id)) return;
    seen.add(template.id);
    templates.push(template);
  };

  for (const permission of permissions) {
    const workItem = workItemPermissionTemplate(permission);
    if (workItem) {
      addTemplate(workItem);
      continue;
    }

    const semantic = semanticPermissionFromDefinition(permission);
    for (const kind of permission.resource_kinds) {
      addTemplate({
        id: `direct:${permission.key}:${kind}`,
        resource: semantic.resource,
        scope: kind === semantic.backendResourceKind ? semantic.scope : `This ${accessResourceKindLabel(kind).toLowerCase()}`,
        action: semantic.action,
        permissionKey: permission.key,
        backendResourceKind: kind,
        description: semantic.description || permission.label,
        risk: permission.risk,
      });
    }
  }

  return templates.sort((a, b) => `${a.resource}:${a.scope}:${a.action}`.localeCompare(`${b.resource}:${b.scope}:${b.action}`));
}

function workItemPermissionTemplate(permission: AccessPermissionDefinition): DirectGrantTemplate | null {
  const [kind, rawAction] = permission.key.split('.');
  if (!['task', 'issue', 'plan'].includes(kind) || !['read', 'write'].includes(rawAction ?? '')) return null;
  const resourceKind = kind as Extract<AccessResourceKind, 'task' | 'issue' | 'plan'>;
  const action = rawAction === 'write' ? 'Write' : 'Read';
  const resource = accessResourceKindLabel(resourceKind);
  const projectPermissionKey = rawAction === 'write' ? 'project.write' : 'project.read';
  return {
    id: `capability:${resourceKind}:${rawAction}`,
    resource,
    scope: `Project or ${resource.toLowerCase()}`,
    action,
    permissionKey: permission.key,
    permissionKeyByResourceKind: {
      project: projectPermissionKey,
      [resourceKind]: permission.key,
    },
    backendResourceKind: resourceKind,
    compatibleKinds: ['project', resourceKind],
    description: `${action} ${resource.toLowerCase()} items in a selected project or on a selected ${resource.toLowerCase()}.`,
    risk: permission.risk,
  };
}

function accessResourceKindLabel(kind: string): string {
  const labels: Record<string, string> = {
    org: 'Organization',
    project: 'Project',
    team: 'Team',
    task: 'Task',
    issue: 'Issue',
    plan: 'Plan',
    conversation: 'Conversation',
    file: 'File',
    agent: 'Agent',
    worker: 'Worker',
    admin_token: 'Admin token',
  };
  return labels[kind] ?? kind;
}

const ACCESS_RESOURCE_KIND_ORDER: Record<string, number> = {
  org: 0,
  Organization: 0,
  team: 1,
  Team: 1,
  project: 2,
  Project: 2,
  issue: 3,
  Issue: 3,
  plan: 4,
  Plan: 4,
  task: 5,
  Task: 5,
};

function accessResourceKindRank(kind: string): number {
  return ACCESS_RESOURCE_KIND_ORDER[kind] ?? 100;
}

function compareAccessResourceKind(a: string, b: string): number {
  const rank = accessResourceKindRank(a) - accessResourceKindRank(b);
  return rank === 0 ? accessResourceKindLabel(a).localeCompare(accessResourceKindLabel(b)) : rank;
}

function comparePickerResource(a: string, b: string): number {
  const rank = accessResourceKindRank(a) - accessResourceKindRank(b);
  return rank === 0 ? a.localeCompare(b) : rank;
}

function accessResourceMetaLabel(resource: AccessResourceScope): string {
  if (resource.kind === 'project') return 'Project';
  if (resource.kind === 'org') return 'Organization';
  return accessResourceKindLabel(resource.kind);
}

function accessActionLabel(permission: AccessPermissionDefinition): string {
  const raw = permission.actions[0] || permission.key.split('.').at(-1) || 'use';
  const normalized = raw === 'update' ? 'write' : raw;
  return normalized.charAt(0).toUpperCase() + normalized.slice(1).replace(/[_-]/g, ' ');
}

function accessRoleRiskForUI(role: AccessRole, permissions: AccessPermissionDefinition[]): AccessRisk {
  const byKey = new Map(permissions.map((permission) => [permission.key, permission]));
  let risk: AccessRisk = 'low';
  for (const key of role.permissions) {
    const permissionRisk = byKey.get(key)?.risk;
    if (permissionRisk === 'high') return 'high';
    if (permissionRisk === 'medium') risk = 'medium';
  }
  return role.high_risk ? 'high' : risk;
}

function roleTemplateDetail(role: AccessRole): string {
  const scope = ramRoleScope(role);
  const param = scope === 'mixed' ? 'resource' : accessResourceKindLabel(scope).toLowerCase();
  return `Template · parameter: ${param} · ${role.permissions.length} permissions`;
}

function directTemplateLabel(template: DirectGrantTemplate): string {
  return `${template.resource} · ${template.scope} · ${template.action}`;
}

function templateCompatibleKinds(template: DirectGrantTemplate): AccessResourceKind[] {
  return template.compatibleKinds ?? [template.backendResourceKind];
}

function permissionKeyForTemplateResource(template: DirectGrantTemplate, resource: AccessResourceScope): string {
  return template.permissionKeyByResourceKind?.[resource.kind] ?? template.permissionKey;
}

function directTemplateLabelForResource(template: DirectGrantTemplate, resource?: AccessResourceScope): string {
  const scope = resource && template.compatibleKinds?.includes(resource.kind)
    ? (resource.kind === 'project' ? 'Project' : `This ${accessResourceKindLabel(resource.kind).toLowerCase()}`)
    : template.scope;
  return `${template.resource} · ${scope} · ${template.action}`;
}

function semanticPermissionFromDefinition(permission: AccessPermissionDefinition): SemanticPermission {
  return semanticPermission(permission.key, permission.template, permission.resource_kinds[0], permission.actions[0], permission.description || permission.label);
}

function semanticPermissionForDecision(
  permissionKey: string,
  template: AccessPermissionTemplate | undefined,
  resource: AccessResourceScope,
  permissionByKey?: Map<string, AccessPermissionDefinition>,
): SemanticPermission {
  const definition = permissionByKey?.get(permissionKey);
  return semanticPermission(
    permissionKey,
    template ?? definition?.template,
    resource.kind,
    definition?.actions[0],
    definition?.description ?? definition?.label,
  );
}

function semanticPermission(
  permissionKey: string,
  template: AccessPermissionTemplate | undefined,
  fallbackResourceKind: string | undefined,
  fallbackAction: string | undefined,
  fallbackDescription?: string,
): SemanticPermission {
  const resource = template?.resource || accessResourceKindLabel(fallbackResourceKind || permissionKey.split('.')[0] || 'resource');
  const scope = template?.scope || (fallbackResourceKind === 'org' ? 'Org-wide' : `This ${accessResourceKindLabel(fallbackResourceKind || 'resource').toLowerCase()}`);
  const action = template?.action || accessActionLabel({ key: permissionKey, actions: fallbackAction ? [fallbackAction] : [], resource_kinds: [], label: '', description: '', risk: 'low', category: 'access', legacy_sources: [] });
  return {
    label: `${resource} · ${scope} · ${action}`,
    resource,
    scope,
    action,
    implementationKey: template?.permission_key || permissionKey,
    backendResourceKind: template?.backend_resource_kind || fallbackResourceKind || 'resource',
    description: template?.description || fallbackDescription,
  };
}

export default function Access({ page = 'ram-roles' }: { page?: AccessPage }): React.ReactElement {
  const org = useOptionalOrgContext();
  const navigate = useNavigate();
  const subjectRef = useAppStore((s) => s.currentUserId);
  const isGrantAccess = page === 'grant-access';
  const isSubjectAccess = page === 'subject-access' || isGrantAccess;
  const orgID = org?.orgId ?? 'org-test';
  const orgName = org?.orgName ?? 'Organization';
  const orgResource = useMemo<ResourceScope>(() => ({ kind: 'org', id: orgID, org_id: orgID, label: orgName }), [orgID, orgName]);
  const currentPermissions = useCurrentSubjectEffectivePermissions(orgResource);
  const canManageAccess = hasEffectivePermission(currentPermissions.data, 'org.member.role.manage');
  const explainAccess = usePermissionExplain(
    subjectRef,
    'org.member.role.manage',
    orgResource,
    currentPermissions.isSuccess && !canManageAccess,
  );
  const [query, setQuery] = useState('');
  const [subjectKind, setSubjectKind] = useState<AccessSubjectKind | 'all'>('all');
  const [projectID, setProjectID] = useState('all');
  const [permission, setPermission] = useState('all');
  const [risk, setRisk] = useState<AccessRisk | 'all'>('all');
  const [status, setStatus] = useState<AccessStatus | 'all'>('all');
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [selectedSubjectRef, setSelectedSubjectRef] = useState('');
  const [toast, setToast] = useState<AccessToast>(null);
  const overview = useAccessOverview({
    q: isSubjectAccess ? query : '',
    subject_kind: isSubjectAccess ? subjectKind : 'all',
    project_id: isSubjectAccess ? projectID : 'all',
    permission: isSubjectAccess ? permission : 'all',
    risk: isSubjectAccess ? risk : 'all',
    status: isSubjectAccess ? status : 'all',
  }, currentPermissions.isSuccess && canManageAccess);
  const data = overview.data;
  const projects = useProjects();
  const teams = useTeams();
  const mappingEntries = useAllTeamRoleRAMMappings(teams.data ?? []);
  const memberEntries = useAllTeamMembers(teams.data ?? []);
  const resources = useMemo(
    () => buildAccessResourceCatalog({
      decisions: data?.decisions ?? [],
      grants: data?.grants ?? [],
      projects: projects.data ?? [],
      teams: teams.data ?? [],
      orgId: orgID,
      orgName,
    }),
    [data?.decisions, data?.grants, orgID, orgName, projects.data, teams.data],
  );
  const projectNameByID = useMemo(() => new Map((projects.data ?? []).map((project) => [project.id, project.name])), [projects.data]);
  const projectOptions = useMemo(() => {
    const byID = new Map<string, string>();
    for (const project of projects.data ?? []) {
      byID.set(project.id, project.name);
    }
    for (const decision of data?.decisions ?? []) {
      const id = projectIDForAccessResource(decision.resource);
      if (!id) continue;
      byID.set(id, projectNameByID.get(id) ?? decision.resource.label ?? id);
    }
    for (const grant of data?.grants ?? []) {
      const id = projectIDForAccessResource(grant.resource);
      if (!id) continue;
      byID.set(id, projectNameByID.get(id) ?? grant.resource.label ?? id);
    }
    return [
      { value: 'all', label: 'All project' },
      ...[...byID.entries()]
        .sort((a, b) => a[1].localeCompare(b[1]))
        .map(([value, label]) => ({ value, label })),
    ];
  }, [data?.decisions, data?.grants, projectNameByID, projects.data]);
  const permissionOptions = useMemo(() => ['all', ...(data?.catalog ?? []).map((entry) => entry.key).sort()], [data?.catalog]);

  const subjectByRef = useMemo(() => {
    const byRef = new Map<string, AccessSubject>();
    for (const subject of data?.subjects ?? []) byRef.set(subject.ref, subject);
    return byRef;
  }, [data?.subjects]);
  const permissionByKey = useMemo(() => {
    const byKey = new Map<string, AccessPermissionDefinition>();
    for (const permission of data?.catalog ?? []) byKey.set(permission.key, permission);
    return byKey;
  }, [data?.catalog]);
  const title = page === 'ram-roles' ? 'RAM Roles' : isGrantAccess ? 'Grant access' : 'Subject access';
  const titleId = `access-${page}-title`;

  return (
    <section
      className={isGrantAccess ? 'flex h-full min-h-0 min-w-0 flex-col gap-4 overflow-hidden' : 'min-w-0 space-y-4'}
      data-testid="page-Access"
      aria-labelledby={titleId}
      data-access-page={page}
    >
      {toast && <AccessToastNotice toast={toast} onDismiss={() => setToast(null)} />}
      <header className="flex shrink-0 flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs font-semibold uppercase tracking-wide text-text-muted">Access</p>
          <h1 id={titleId} className="font-heading text-2xl font-semibold text-text-primary">{title}</h1>
        </div>
        {page === 'subject-access' && <div className="flex flex-wrap gap-2">
          {!canManageAccess || (data?.subjects.length ?? 0) === 0 ? (
            <button
              type="button"
              className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-medium text-btn-primary-fg opacity-50"
              disabled
              title={!canManageAccess ? 'Requires org.member.role.manage' : 'No subjects available'}
              data-testid="access-open-batch"
            >
              Grant access
            </button>
          ) : (
            <OrgLink
              to="/access/grant-access"
              className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-medium text-btn-primary-fg hover:opacity-90"
              onClick={() => setDrawerOpen(true)}
              data-testid="access-open-batch"
            >
              Grant access
            </OrgLink>
          )}
        </div>}
      </header>

      {currentPermissions.isLoading && <AccessPageSkeleton page={page} />}
      {currentPermissions.isError && (
        <AccessForbidden
          page={page}
          reason={(currentPermissions.error as Error).message}
          status={currentPermissions.error instanceof ApiError ? currentPermissions.error.status : 403}
        />
      )}
      {currentPermissions.isSuccess && !canManageAccess && (
        <AccessForbidden
          page={page}
          reason={explainAccess.data?.decision.reason ?? (explainAccess.error as Error | undefined)?.message ?? 'Current subject lacks org.member.role.manage'}
          status={403}
        />
      )}

      {currentPermissions.isSuccess && canManageAccess && (
      <>
      {page === 'subject-access' && <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <SummaryTile label="Allowed" value={data?.summary.allowed ?? 0} tone="success" />
        <SummaryTile label="High risk" value={data?.summary.high_risk ?? 0} tone="danger" />
        <SummaryTile label="Expiring" value={data?.summary.expiring ?? 0} tone="warning" />
        <SummaryTile label="No access" value={data?.summary.denied ?? 0} tone="warning" />
        <SummaryTile label="Not applicable" value={data?.summary.not_applicable ?? 0} tone="muted" />
      </div>}

      {page === 'subject-access' && <div className="flex flex-wrap items-end gap-2" data-testid="subject-access-filters">
        <label className="relative min-w-0 flex-[1_1_14rem] md:max-w-xs">
          <span className="sr-only">Search access</span>
          <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-muted">
            <IconSearch />
          </span>
          <input
            className="w-full rounded border border-border-base bg-bg-elevated py-2 pl-9 pr-3 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
            placeholder="Subject, name, email, or ID"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            data-testid="access-search"
          />
        </label>
        <div className="flex flex-[1_1_100%] flex-wrap gap-2 md:flex-auto">
          <Select label="Type" value={subjectKind} onChange={(v) => setSubjectKind(v as AccessSubjectKind | 'all')} options={SUBJECT_OPTIONS} />
          <Select label="Project" value={projectID} onChange={setProjectID} options={projectOptions} />
          <Select label="Permission" value={permission} onChange={setPermission} options={permissionOptions} />
          <Select label="Risk" value={risk} onChange={(v) => setRisk(v as AccessRisk | 'all')} options={RISK_OPTIONS} />
          <Select label="Status" value={status} onChange={(v) => setStatus(v as AccessStatus | 'all')} options={STATUS_OPTIONS} />
        </div>
      </div>}

      {overview.isLoading && <AccessPageSkeleton page={page} />}
      {overview.isError && (
        <p className="rounded border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger" role="alert" data-testid={`access-${page}-error`}>
          {(overview.error as Error).message}
        </p>
      )}

      {!overview.isLoading && !overview.isError && data && (
        isGrantAccess ? (
        <BatchGrantDrawer
          subjects={data.subjects}
          roles={data.roles}
          permissions={data.catalog}
          resources={resources}
          decisions={data.decisions}
          teams={teams.data ?? []}
          canManageAccess={canManageAccess}
          contextSubjectRef={selectedSubjectRef}
          onToast={setToast}
          onClose={() => navigate(org?.slug ? `/organizations/${org.slug}/access/subject-access` : '/access/subject-access')}
          mode="page"
        />
        ) : (
        <div className={page === 'subject-access' ? 'grid min-w-0 gap-4 2xl:grid-cols-[minmax(0,1fr)_22rem]' : 'grid min-w-0 gap-4'}>
          <div className="space-y-4">
            {page === 'ram-roles' && (
                <RAMRolesView
                  catalog={data.catalog}
                  mappingEntries={mappingEntries}
                  canManageAccess={canManageAccess}
                  onToast={setToast}
                />
            )}
            {page === 'subject-access' && (
              <SubjectDecisionView
                decisions={data.decisions}
                grants={data.grants}
                subjects={data.subjects}
                subjectByRef={subjectByRef}
                permissionByKey={permissionByKey}
                memberEntries={memberEntries}
                mappingEntries={mappingEntries}
                onSelectSubject={setSelectedSubjectRef}
              />
            )}
          </div>
          {page === 'subject-access' && <aside className="min-w-0 space-y-4" aria-label="Trace and audit">
            <SubjectAccessSidebar
                decisions={data.decisions}
                grants={data.grants}
                subjects={data.subjects}
                selectedSubjectRef={selectedSubjectRef}
                permissionByKey={permissionByKey}
                memberEntries={memberEntries}
                mappingEntries={mappingEntries}
              />
            <GrantRevoke key={selectedSubjectRef} grants={data.grants} subjectRef={selectedSubjectRef} permissionByKey={permissionByKey} canManageAccess={canManageAccess} onToast={setToast} />
          </aside>}
        </div>
        )
      )}

      {drawerOpen && data && (
        <BatchGrantDrawer
          subjects={data.subjects}
          roles={data.roles}
          permissions={data.catalog}
          resources={resources}
          decisions={data.decisions}
          teams={teams.data ?? []}
          canManageAccess={canManageAccess}
          contextSubjectRef={selectedSubjectRef}
          onToast={setToast}
          onClose={() => setDrawerOpen(false)}
          mode="dialog"
        />
      )}
      </>
      )}
    </section>
  );
}

function AccessPageSkeleton({ page }: { page: AccessPage }): React.ReactElement {
  return (
    <div className="space-y-3" aria-busy="true" aria-label={`Loading ${page === 'ram-roles' ? 'RAM Roles' : 'Subject access'}`} data-testid={`access-${page}-loading`}>
      <span className="sr-only">Loading {page === 'ram-roles' ? 'RAM Roles' : 'Subject access'}</span>
      <Skeleton height="4rem" />
      <Skeleton height="18rem" />
    </div>
  );
}

function AccessToastNotice({ toast, onDismiss }: { toast: NonNullable<AccessToast>; onDismiss: () => void }): React.ReactElement {
  const toneClass = toast.tone === 'success'
    ? 'border-status-emerald-border bg-status-emerald-bg text-status-emerald-fg'
    : toast.tone === 'warning'
      ? 'border-status-amber-border bg-status-amber-bg text-status-amber-fg'
      : 'border-danger/40 bg-danger/10 text-danger';
  return (
    <div
      role="status"
      aria-live="polite"
      data-testid="access-toast"
      className={`fixed right-4 top-4 z-50 flex max-w-md items-start gap-3 rounded border px-4 py-3 text-sm shadow-2 ${toneClass}`}
    >
      <span className="font-medium">{toast.message}</span>
      <button type="button" className="rounded px-1 text-current hover:bg-bg-subtle/50" aria-label="Dismiss notification" onClick={onDismiss}>
        <IconClose className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}

function accessToastFromError(error: unknown, fallbackMessage: string): AccessToast {
  const status = error instanceof ApiError ? error.status : undefined;
  const message = error instanceof Error ? error.message : fallbackMessage;
  if (status === 403) return { tone: 'danger', message: `403: ${message}` };
  if (status === 409) return { tone: 'warning', message: `409: ${message}` };
  return { tone: 'danger', message };
}

function AccessForbidden({ page, reason, status }: { page: AccessPage; reason: string; status: number }): React.ReactElement {
  return (
    <section className="rounded border border-danger/30 bg-danger/10 p-4" data-testid={`access-${page}-forbidden`} role="alert">
      <h2 className="text-sm font-semibold text-danger">{page === 'ram-roles' ? 'RAM Roles' : 'Subject access'} unavailable ({status})</h2>
      <p className="mt-1 text-sm text-danger">{reason}</p>
      <p className="mt-2 text-xs text-text-muted">
        The frontend hides controls from current effective permissions only; the backend remains authoritative.
      </p>
    </section>
  );
}

function SummaryTile({ label, value, tone }: { label: string; value: number; tone: 'success' | 'danger' | 'warning' | 'muted' }): React.ReactElement {
  const toneClass = {
    success: 'text-success',
    danger: 'text-danger',
    warning: 'text-warning',
    muted: 'text-text-secondary',
  }[tone];
  return (
    <div className="rounded border border-border-base bg-bg-elevated px-3 py-2">
      <div className="text-[0.6875rem] font-semibold uppercase text-text-muted">{label}</div>
      <div className={`mt-1 text-xl font-semibold tabular-nums ${toneClass}`}>{value}</div>
    </div>
  );
}

function Select({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: readonly SelectOption[];
  onChange: (value: string) => void;
}): React.ReactElement {
  return (
    <label className="text-xs font-semibold uppercase text-text-muted">
      <span className="sr-only">{label}</span>
      <select
        className="rounded border border-border-base bg-bg-elevated px-2.5 py-2 text-sm font-medium normal-case text-text-secondary focus-visible:border-accent focus-visible:outline-none"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        data-testid={`access-filter-${label.toLowerCase()}`}
      >
        {options.map((option) => {
          const value = typeof option === 'string' ? option : option.value;
          const text = typeof option === 'string' ? (option === 'all' ? `All ${label.toLowerCase()}` : option) : option.label;
          return (
            <option key={value} value={value}>
              {text}
            </option>
          );
        })}
      </select>
    </label>
  );
}

function RAMRolesView({
  catalog,
  mappingEntries,
  canManageAccess,
  onToast,
}: {
  catalog: AccessPermissionDefinition[];
  mappingEntries: MappingEntry[];
  canManageAccess: boolean;
  onToast: (toast: AccessToast) => void;
}): React.ReactElement {
  const roles = useRAMRoles();
  const newVersion = useRAMRoleNewVersion();
  const deleteRole = useRAMRoleDelete();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [roleSearch, setRoleSearch] = useState('');
  const [editName, setEditName] = useState('');
  const [editStableKey, setEditStableKey] = useState('');
  const [editDescription, setEditDescription] = useState('');
  const [editScope, setEditScope] = useState('team');
  const [draftPermissions, setDraftPermissions] = useState<string[]>([]);
  const [roleDrawer, setRoleDrawer] = useState<{ mode: 'create' | 'edit'; roleId?: string } | null>(null);
  const [deleteNameConfirm, setDeleteNameConfirm] = useState('');
  const [showReferences, setShowReferences] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const [roleRisk, setRoleRisk] = useState<AccessRisk | 'all'>('all');
  const [roleScope, setRoleScope] = useState('all');
  const [density, setDensity] = useState<'comfortable' | 'compact'>('comfortable');
  const [page, setPage] = useState(1);
  const pageSize = 10;
  const selected = selectedId ?? roles.data?.roles[0]?.id ?? null;
  const detail = useRAMRole(selected);
  const latest = detail.data?.latest;
  const versionPermissions = draftPermissions.length > 0 ? draftPermissions : latest?.permissions ?? [];
  const mappedByRAMRole = useRAMRoleReferences(mappingEntries);
  const mappedReferences = selected ? mappedByRAMRole.get(selected) ?? [] : [];
  const allRoles = roles.data?.roles ?? [];
  const customCount = allRoles.filter((role) => role.kind === 'custom').length;
  const referencedCount = allRoles.filter((role) => (role.references ?? (mappedByRAMRole.get(role.id) ?? []).length) > 0).length;
  const highRiskCount = allRoles.filter((role) => role.risk === 'high').length;
  const permissionCount = new Set(allRoles.flatMap((role) => role.permissions)).size;
  const scopeOptions = useMemo(() => ['all', ...Array.from(new Set(allRoles.map((role) => role.scope || 'team'))).sort()], [allRoles]);
  const filteredRoles = useMemo(() => {
    const q = roleSearch.trim().toLowerCase();
    return allRoles.filter((role) => {
      const matchesText = !q || [role.name, role.stable_key, role.id, role.description, role.scope].some((v) => (v ?? '').toLowerCase().includes(q));
      const matchesRisk = roleRisk === 'all' || role.risk === roleRisk;
      const matchesScope = roleScope === 'all' || (role.scope || 'team') === roleScope;
      return matchesText && matchesRisk && matchesScope;
    });
  }, [allRoles, roleRisk, roleScope, roleSearch]);
  const totalPages = Math.max(1, Math.ceil(filteredRoles.length / pageSize));
  const pageRoles = filteredRoles.slice((page - 1) * pageSize, page * pageSize);
  useEffect(() => {
    setPage(1);
  }, [roleSearch, roleRisk, roleScope]);

  const toggleDraftPermission = (permission: string): void => {
    setDraftPermissions((prev) => toggleValue(prev, permission).sort());
  };
  const resetDraft = (role?: RAMRole): void => {
    setDraftPermissions(role?.permissions ?? []);
    setDeleteNameConfirm('');
    setShowReferences(false);
  };
  useEffect(() => {
    if (!detail.data) return;
    setEditName(detail.data.name);
    setEditStableKey(detail.data.stable_key || detail.data.id);
    setEditDescription(detail.data.description);
    setEditScope(detail.data.scope || 'team');
    setDraftPermissions(detail.data.latest.permissions ?? []);
  }, [detail.data?.id, detail.data?.latest.version]);
  const selectedReferences = detail.data?.references ?? [];
  const selectedIsReferenced = selectedReferences.length > 0 || mappedReferences.length > 0;
  const selectedIsCustom = detail.data?.kind === 'custom';
  return (
    <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(24rem,1fr)]" data-testid="access-roles-view">
      <span className="sr-only" data-testid="access-runtime-sha">Runtime SHA: {import.meta.env.VITE_BUILD_SHA || 'development'}</span>
      <section className="rounded border border-border-base bg-bg-elevated">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border-base px-3 py-2.5">
          <div className="flex flex-wrap items-center justify-end gap-2">
            <button type="button" data-testid="access-role-new" className="rounded bg-btn-primary-bg px-3 py-2 text-xs font-semibold text-btn-primary-fg shadow-sm" onClick={() => setRoleDrawer({ mode: 'create' })}>＋ New RAM Role</button>
            <AccessMetaPill>{roles.data?.roles.length ?? 0} roles</AccessMetaPill>
          </div>
          <div className="flex items-center gap-2">
            <label className="relative block w-52">
              <span className="sr-only">Search RAM Roles</span>
              <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-muted"><IconSearch /></span>
              <input className="w-full rounded border border-border-base bg-bg-base py-2 pl-9 pr-3 text-xs text-text-primary placeholder:text-text-muted" placeholder="Search RAM roles" value={roleSearch} onChange={(e) => setRoleSearch(e.target.value)} data-testid="access-role-search" />
            </label>
            <Select label="RAM role risk" value={roleRisk} onChange={(v) => setRoleRisk(v as AccessRisk | 'all')} options={RISK_OPTIONS} />
            <Select label="RAM role scope" value={roleScope} onChange={setRoleScope} options={scopeOptions} />
            <label className="text-xs font-semibold uppercase text-text-muted">
              <span className="sr-only">Density</span>
              <select
                className="rounded border border-border-base bg-bg-elevated px-2.5 py-2 text-sm font-medium normal-case text-text-secondary focus-visible:border-accent focus-visible:outline-none"
                value={density}
                onChange={(event) => setDensity(event.target.value as 'comfortable' | 'compact')}
                data-testid="access-role-density"
              >
                <option value="comfortable">Comfortable</option>
                <option value="compact">Compact</option>
              </select>
            </label>
          </div>
        </div>
        {status && <div className="border-b border-success/30 bg-success/10 px-4 py-2 text-sm text-success" role="status" data-testid="access-role-success">{status}</div>}
        <div className="flex flex-wrap gap-x-4 gap-y-1 border-b border-border-base bg-bg-subtle/40 px-4 py-2 text-xs text-text-secondary" data-testid="access-role-stats">
          {customCount} custom · {highRiskCount} high risk · {permissionCount} permissions · {referencedCount} referenced
        </div>
        {roles.isLoading && <div data-testid="access-ram-roles-loading" aria-busy="true"><div className="p-4" data-testid="access-role-list-loading"><Skeleton height="14rem" /></div></div>}
        {roles.isError && (
          <div data-testid="access-ram-roles-error">
            <div className="m-4 rounded border border-danger/30 bg-danger/10 p-4 text-sm text-danger" role="alert" data-testid="access-role-list-error">
              <p className="font-semibold">RAM Roles could not be loaded.</p>
              <p className="mt-1">{(roles.error as Error).message}</p>
              <button type="button" className="mt-3 rounded border border-danger/40 px-3 py-1.5 text-xs font-semibold" onClick={() => void roles.refetch()}>Retry</button>
            </div>
          </div>
        )}
        {roles.isSuccess && roles.data.roles.length === 0 && <EmptyState title="No RAM Roles yet" body="Create the first RAM Role from the toolbar." testId="access-role-empty" />}
        {roles.isSuccess && roles.data.roles.length > 0 && <>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[42rem] text-left text-sm">
            <thead className="border-b border-border-base text-[0.6875rem] uppercase text-text-muted">
              <tr>
                <th className="px-4 py-2 font-semibold">Role</th>
                <th className="px-4 py-2 font-semibold">Risk</th>
                <th className="px-4 py-2 font-semibold">Permissions</th>
                <th className="px-4 py-2 font-semibold">Used by Team Roles</th>
                <th className="px-4 py-2 font-semibold">Updated ↓</th>
              </tr>
            </thead>
            <tbody>
              {pageRoles.map((role) => (
                <tr
                  key={role.id}
                  className={['cursor-pointer border-b border-border-base last:border-0', selected === role.id ? 'bg-brand/5' : 'hover:bg-bg-subtle'].join(' ')}
                  onClick={() => {
                    setSelectedId(role.id);
                    resetDraft(role);
                  }}
                  data-testid={`access-role-row-${role.id}`}
                >
                  <td className={`px-4 ${density === 'compact' ? 'py-1.5' : 'py-2.5'}`}>
                    <div className="font-semibold text-text-primary">{role.name}</div>
                    <div className="font-mono text-[0.6875rem] text-text-muted">{role.stable_key || role.id}</div>
                  </td>
                  <td className={`px-4 ${density === 'compact' ? 'py-1.5' : 'py-2.5'}`}><AccessRiskBadge risk={role.risk} /></td>
                  <td className={`px-4 font-mono text-xs text-text-secondary ${density === 'compact' ? 'py-1.5' : 'py-2.5'}`}>{role.permissions.length}</td>
                  <td className={`px-4 text-xs text-text-secondary ${density === 'compact' ? 'py-1.5' : 'py-2.5'}`}>{role.references ?? (mappedByRAMRole.get(role.id) ?? []).length}</td>
                  <td className={`px-4 text-xs text-text-secondary ${density === 'compact' ? 'py-1.5' : 'py-2.5'}`}>Latest<br /><span className="font-mono text-text-muted">v{role.version}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {filteredRoles.length === 0 && <EmptyState title="No matching RAM Roles" body="Change search, risk, or scope filters to widen the list." testId="access-role-no-results" />}
        <div className="flex items-center justify-between gap-2 border-t border-border-base px-4 py-3 text-xs text-text-secondary" data-testid="access-role-pagination">
          <span>Showing {filteredRoles.length === 0 ? 0 : (page - 1) * pageSize + 1} to {Math.min(page * pageSize, filteredRoles.length)} of {filteredRoles.length} RAM roles</span>
          <div className="flex items-center gap-1">
            <button type="button" className="rounded border border-border-base px-2 py-1 disabled:opacity-50" disabled={page <= 1} onClick={() => setPage((value) => Math.max(1, value - 1))}>Previous</button>
            {Array.from({ length: totalPages }, (_, index) => index + 1).map((number) => <button key={number} type="button" aria-label={`Page ${number}`} className={`min-w-7 rounded border px-2 py-1 ${number === page ? 'border-accent text-accent' : 'border-border-base'}`} onClick={() => setPage(number)}>{number}</button>)}
            <button type="button" className="rounded border border-border-base px-2 py-1 disabled:opacity-50" disabled={page >= totalPages} onClick={() => setPage((value) => Math.min(totalPages, value + 1))}>Next</button>
            <span className="ml-2 rounded border border-border-base px-2 py-1">10 / page</span>
          </div>
        </div>
        </>}
      </section>

      <aside className="min-w-0 space-y-4">
        <section className="rounded border border-border-base bg-bg-elevated" data-testid="access-role-detail">
          <div className="flex items-center justify-between gap-2 border-b border-border-base px-4 py-3">
            <div>
              <h2 className="text-base font-semibold text-text-primary">{detail.data?.name ?? 'RAM Role detail'}</h2>
              {detail.data && <p className="mt-0.5 font-mono text-xs text-text-muted">{detail.data.stable_key}</p>}
            </div>
            {detail.data && (
              <div className="flex gap-2"><button type="button" className="rounded border border-border-base px-3 py-1.5 text-xs font-semibold" data-testid="access-role-edit-open" onClick={() => setRoleDrawer({ mode: 'edit', roleId: detail.data.id })}>Edit</button>
              <button type="button" className="rounded border border-danger/30 bg-danger/5 px-3 py-1.5 text-xs font-semibold text-danger" onClick={() => document.querySelector<HTMLInputElement>('[data-testid=access-role-delete-name]')?.focus()}>Delete</button></div>
            )}
          </div>
          {detail.isLoading && <Skeleton height="8rem" />}
          {detail.isError && (
            <div className="m-4 rounded border border-danger/30 bg-danger/10 p-3 text-sm text-danger" role="alert" data-testid="access-role-detail-error">
              <p>{(detail.error as Error).message}</p>
              <button type="button" className="mt-2 rounded border border-danger/40 px-2 py-1 text-xs font-semibold" onClick={() => void detail.refetch()}>Retry detail</button>
            </div>
          )}
          {!selected && roles.isSuccess && roles.data.roles.length === 0 && (
            <EmptyState title="Select a RAM Role" body="Role details, permission summary, references, and version history appear here." testId="access-role-detail-empty" />
          )}
          {detail.data && (
            <>
              <div className="p-4">
                <div className="flex items-center gap-2 text-xs text-text-muted"><AccessRiskBadge risk={detail.data.latest.risk} /><span>Latest v{detail.data.latest.version} · {detail.data.scope}</span></div>
                <div className="mt-3 rounded border border-border-base bg-bg-subtle p-3 text-xs text-text-secondary" data-testid="access-role-used-by">
                  <div className="font-semibold text-text-primary">Used by Team Roles <span className="font-normal text-text-muted">(read-only)</span></div>
                  <p className="mt-1">Open the Team Role to change its RAM Roles.</p>
                  <div className="mt-2">
                  {mappedReferences.length > 0
                    ? mappedReferences.map((ref) => `${ref.team.name} / ${ref.role}`).join(', ')
                    : selectedReferences.length > 0
                      ? selectedReferences.map((ref) => `${ref.team_name} / ${ref.team_role}`).join(', ')
                      : 'None'}
                  </div>
                </div>
                <div className="mt-4 border-t border-border-base pt-4"><PermissionSummary role={detail.data.latest} catalog={catalog} /></div>
                {selectedIsReferenced && (
                  <div className="mt-2 space-y-2" data-testid="access-role-delete-blocked">
                    <p className="text-xs text-danger">This RAM Role cannot be deleted while Team Roles use it.</p>
                    <button type="button" data-testid="access-role-view-references" className="rounded border border-border-base px-2 py-1 text-xs" onClick={() => setShowReferences((value) => !value)}>
                      View references
                    </button>
                    {showReferences && <div className="space-y-2" data-testid="access-role-references">
                      {mappedReferences.map((ref) => (
                        <p key={`${ref.team.id}:${ref.role}`} className="text-xs">
                          {ref.team.name} / {ref.role}{' '}
                          <OrgLink className="font-semibold text-accent hover:underline" to={`/teams/${ref.team.id}/roles/${encodeURIComponent(ref.role)}`} data-testid={`access-role-open-team-role-${ref.team.id}-${ref.role}`}>
                            Open Team Role
                          </OrgLink>
                        </p>
                      ))}
                    </div>}
                  </div>
                )}
              </div>
              <div className="mx-4 mb-4 space-y-2 rounded border border-border-base p-3" data-testid="access-role-versions">
                <h3 className="text-sm font-semibold text-text-primary">Version history</h3>
                {detail.data.versions.map((version) => (
                  <div key={version.version} className="rounded border border-border-base p-2">
                    <div className="flex items-center justify-between">
                      <span className="font-mono text-xs font-semibold">v{version.version}</span>
                      <AccessRiskBadge risk={version.risk} />
                    </div>
                    <div className="mt-1 font-mono text-[0.6875rem] text-text-secondary">{version.permissions.join(', ')}</div>
                  </div>
                ))}
              </div>
              <div className="border-t border-border-base p-4">
                <h3 className="text-xs font-semibold uppercase text-text-muted">Delete RAM Role</h3>
                <p className="mt-2 text-xs text-text-secondary">Type the RAM Role name, then confirm deletion. Remove it from every Team Role first if it is in use.</p>
                <div className="sr-only">
                  <RoleTextField label="Name" value={editName} onChange={setEditName} testId="access-role-edit-name" />
                  <RoleTextField label="Stable key" value={editStableKey} onChange={setEditStableKey} testId="access-role-edit-stable-key" />
                  <RoleTextField label="Description" value={editDescription} onChange={setEditDescription} testId="access-role-edit-description" />
                  <RoleTextField label="Scope" value={editScope} onChange={setEditScope} testId="access-role-edit-scope" />
                  <PermissionChecklist catalog={catalog} selected={versionPermissions} onToggle={toggleDraftPermission} />
                  <button
                  type="button"
                  className="mt-3 rounded border border-border-base px-3 py-1.5 text-sm font-semibold text-text-primary hover:bg-bg-subtle disabled:opacity-50"
                  disabled={!canManageAccess || !latest || !selected || !selectedIsCustom || versionPermissions.length === 0 || newVersion.isPending}
                  data-testid="access-role-new-version-submit"
                  onClick={() => {
                    if (!latest || !selected) return;
                    newVersion.mutate({
                      id: selected,
                      payload: {
                        name: editName,
                        stable_key: editStableKey,
                        description: editDescription,
                        scope: editScope,
                        permissions: versionPermissions,
                        expected_latest_version: latest.version,
                      },
                    }, {
                      onSuccess: (updated) => {
                        resetDraft(updated.latest);
                        onToast({ tone: 'success', message: `Saved RAM Role ${updated.name}.` });
                      },
                      onError: (error) => onToast(accessToastFromError(error, 'Save RAM Role failed')),
                    });
                  }}
                >
                  Create version
                </button>
                </div>
                <button
                  type="button"
                  className="ml-2 rounded border border-danger/40 px-3 py-1.5 text-sm font-semibold text-danger hover:bg-danger/10 disabled:opacity-50"
                  disabled={!canManageAccess || !selected || !latest || !selectedIsCustom || selectedIsReferenced || deleteNameConfirm !== detail.data.name || deleteRole.isPending}
                  data-testid="access-role-disable-submit"
                  onClick={() => {
                    if (!selected || !latest) return;
                    deleteRole.mutate(
                      { id: selected, expected_latest_version: latest.version, confirm_unreferenced: true, reason: 'RAM role deleted after typed-name confirmation' },
                      {
                        onSuccess: () => {
                          const name = detail.data?.name ?? selected;
                          const message = `Deleted RAM Role ${name}.`;
                          setStatus(message);
                          setSelectedId(null);
                          onToast({ tone: 'success', message });
                        },
                        onError: (error) => onToast(accessToastFromError(error, 'Delete RAM Role failed')),
                      },
                    );
                  }}
                >
                  Delete
                </button>
                {selectedIsReferenced && (
                  <p className="mt-2 text-xs text-danger">Delete is blocked while Team Roles use this RAM Role. Open each Team Role to remove it.</p>
                )}
                {!selectedIsReferenced && (
                  <RoleTextField label={`Type ${detail.data.name} to delete`} value={deleteNameConfirm} onChange={setDeleteNameConfirm} testId="access-role-delete-name" />
                )}
              </div>
            </>
          )}
          {newVersion.isError && <p className="mt-2 text-xs text-danger" role="alert">{(newVersion.error as Error).message}</p>}
          {deleteRole.isError && <p className="mt-2 text-xs text-danger" role="alert">{(deleteRole.error as Error).message}</p>}
        </section>
      </aside>
      {roleDrawer && (
        <RAMRoleDrawer
          mode={roleDrawer.mode}
          role={roleDrawer.mode === 'edit' ? detail.data ?? null : null}
          catalog={catalog}
          canManageAccess={canManageAccess}
          onClose={() => setRoleDrawer(null)}
          onToast={onToast}
          onCreated={(created) => setSelectedId(created.id)}
        />
      )}
    </div>
  );
}

function PermissionSummary({ role, catalog }: { role: RAMRole; catalog: AccessPermissionDefinition[] }): React.ReactElement {
  const selected = new Set(role.permissions);
  const definitions = role.permissions.map((key) => ({ key, definition: catalog.find((permission) => permission.key === key) }));
  const byRisk = {
    high: catalog.filter((permission) => selected.has(permission.key) && permission.risk === 'high').length,
    medium: catalog.filter((permission) => selected.has(permission.key) && permission.risk === 'medium').length,
    low: catalog.filter((permission) => selected.has(permission.key) && permission.risk === 'low').length,
  };
  return (
    <div className="mt-3 rounded border border-border-base bg-bg-base p-3" data-testid="access-role-permission-summary">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-xs font-semibold uppercase text-text-muted">Permission summary</h3>
        <AccessRiskBadge risk={role.risk} />
      </div>
      <div className="mt-2 grid grid-cols-4 gap-2 text-xs">
        <AccessMetaPill>{role.permissions.length} permissions</AccessMetaPill>
        <AccessMetaPill>{byRisk.high} high</AccessMetaPill>
        <AccessMetaPill>{byRisk.medium} medium</AccessMetaPill>
        <AccessMetaPill>{byRisk.low} low</AccessMetaPill>
      </div>
      <div className="mt-3 space-y-2" aria-label="Permissions in this RAM Role">
        {definitions.map(({ key, definition }) => (
          <article key={key} className="rounded border border-border-base bg-bg-elevated p-2" data-testid={`access-role-permission-${key}`}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <div className="text-xs font-semibold text-text-primary">
                  {definition ? semanticPermissionFromDefinition(definition).label : key}
                </div>
                <div className="mt-0.5 font-mono text-[0.6875rem] text-text-muted">{key}</div>
              </div>
              {definition ? <AccessRiskBadge risk={definition.risk} /> : <span className="rounded border border-danger/30 bg-danger/10 px-2 py-0.5 text-[0.6875rem] font-semibold text-danger">Invalid registry key</span>}
            </div>
            <p className="mt-1 text-xs text-text-muted">{definition?.description ?? 'This key is not present in the authoritative permission registry and cannot be selected for new writes.'}</p>
            {definition && (
              <p className="mt-1 text-[0.6875rem] text-text-muted">
                Resource {semanticPermissionFromDefinition(definition).resource} · Scope {semanticPermissionFromDefinition(definition).scope} · Action {semanticPermissionFromDefinition(definition).action}
              </p>
            )}
          </article>
        ))}
      </div>
    </div>
  );
}

function RAMRoleDrawer({
  mode,
  role,
  catalog,
  canManageAccess,
  onClose,
  onToast,
  onCreated,
}: {
  mode: 'create' | 'edit';
  role: RAMRoleDetail | null;
  catalog: AccessPermissionDefinition[];
  canManageAccess: boolean;
  onClose: () => void;
  onToast: (toast: AccessToast) => void;
  onCreated: (role: RAMRoleDetail) => void;
}): React.ReactElement {
  const containerRef = useModalA11y({ open: true, onClose });
  const create = useRAMRoleCreate();
  const update = useRAMRoleUpdate();
  const base = role?.latest;
  const [name, setName] = useState(base?.name ?? '');
  const [stableKey, setStableKey] = useState(base?.stable_key || role?.stable_key || role?.id || '');
  const [description, setDescription] = useState(base?.description ?? '');
  const [scope, setScope] = useState(base?.scope ?? 'team');
  const [risk, setRisk] = useState<AccessRisk>(base?.risk ?? 'medium');
  const [permissions, setPermissions] = useState<string[]>(base?.permissions ?? []);
  const [ack, setAck] = useState(false);
  const hasHighRisk = risk === 'high' || catalog.some((permission) => permissions.includes(permission.key) && permission.risk === 'high');
  const title = mode === 'create' ? 'Create RAM Role' : 'Edit RAM Role';
  const canSubmit = Boolean(canManageAccess && name.trim() && stableKey.trim() && permissions.length > 0 && (!hasHighRisk || ack));
  const togglePermission = (permission: string): void => setPermissions((prev) => toggleValue(prev, permission).sort());
  const submit = (): void => {
    const payload = {
      name,
      stable_key: stableKey,
      description,
      scope,
      risk,
      permissions,
      expected_latest_version: base?.version,
    };
    if (mode === 'create') {
      create.mutate(payload, {
        onSuccess: (created) => {
          onCreated(created);
          onToast({ tone: 'success', message: `Created RAM Role ${created.name}.` });
          onClose();
        },
        onError: (error) => onToast(accessToastFromError(error, 'Create RAM Role failed')),
      });
      return;
    }
    if (!role) return;
    update.mutate({ id: role.id, payload }, {
      onSuccess: (saved) => {
        onToast({ tone: 'success', message: `Saved RAM Role ${saved.name}.` });
        onClose();
      },
      onError: (error) => onToast(accessToastFromError(error, 'Save RAM Role failed')),
    });
  };
  return (
    <div className="fixed inset-0 z-50" data-testid="access-role-drawer-backdrop">
      <aside
        ref={containerRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="fixed inset-y-0 left-14 flex h-full w-full max-w-[16rem] flex-col border-r border-border-base bg-bg-elevated text-text-primary shadow-2"
        data-testid="access-role-drawer"
      >
        <div className="flex items-start justify-between gap-3 border-b border-border-base px-5 py-4">
          <div>
            <h2 className="text-lg font-semibold">{title}</h2>
            <p className="mt-1 text-xs text-text-muted">
              Define a reusable role template with scope support and Resource / Scope / Action permissions.
            </p>
          </div>
          <button type="button" aria-label="Close" title="Close" className="rounded p-1.5 text-text-secondary hover:bg-bg-subtle" onClick={onClose}>
            <IconClose />
          </button>
        </div>
        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-5">
          <div className="space-y-3">
            <RoleTextField label="Name" value={name} onChange={setName} testId="access-role-name" />
            <RoleTextField label="Stable key" value={stableKey} onChange={setStableKey} testId="access-role-stable-key" />
            <label className="mt-3 block">
              <span className="text-xs font-semibold text-text-muted">Scope support</span>
              <select className="mt-1 w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm text-text-primary" value={scope} data-testid="access-role-scope" onChange={(event) => setScope(event.target.value)}>
                <option value="org">Organization</option><option value="project">Project</option><option value="team">Team</option>
              </select>
            </label>
            <label className="mt-3 block">
              <span className="text-xs font-semibold uppercase text-text-muted">Risk</span>
              <select className="mt-1 w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm text-text-primary" value={risk} data-testid="access-role-risk" onChange={(event) => setRisk(event.target.value as AccessRisk)}>
                <option value="low">low</option>
                <option value="medium">medium</option>
                <option value="high">high</option>
              </select>
            </label>
          </div>
          <label className="block">
            <span className="text-xs font-semibold uppercase text-text-muted">Description</span>
            <textarea
              className="mt-1 min-h-20 w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm text-text-primary"
              value={description}
              data-testid="access-role-description"
              onChange={(event) => setDescription(event.target.value)}
            />
          </label>
          <div className="sr-only"><PermissionSummary role={{
            id: role?.id ?? 'draft',
            stable_key: stableKey,
            name,
            version: base?.version ?? 1,
            kind: role?.kind ?? 'custom',
            description,
            permissions,
            risk,
            scope,
          }} catalog={catalog} /></div>
          <PermissionChecklist catalog={catalog} selected={permissions} onToggle={togglePermission} />
          {hasHighRisk && (
            <div className="flex items-start gap-2 rounded border border-warning/30 bg-warning/10 p-3 text-xs text-text-secondary">
              <button
                type="button"
                role="switch"
                aria-checked={ack}
                aria-labelledby="access-role-risk-ack-label"
                className={`mt-0.5 h-5 w-9 shrink-0 rounded-full p-0.5 transition-colors ${ack ? 'bg-warning' : 'bg-border-strong'}`}
                data-testid="access-role-risk-ack"
                onClick={() => setAck((value) => !value)}
              >
                <span className={`block h-4 w-4 rounded-full bg-white transition-transform ${ack ? 'translate-x-4' : ''}`} />
              </button>
              <span id="access-role-risk-ack-label">Approve high-risk RAM Role permissions before saving.</span>
            </div>
          )}
          {(create.isError || update.isError) && <p className="text-sm text-danger" role="alert">{((create.error ?? update.error) as Error).message}</p>}
        </div>
        <div className="flex items-center justify-end gap-2 border-t border-border-base px-5 py-4">
          <button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm font-semibold" onClick={onClose}>Cancel</button>
          <button
            type="button"
            className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-semibold text-btn-primary-fg disabled:opacity-50"
            disabled={!canSubmit || create.isPending || update.isPending}
            data-testid="access-role-create-submit"
            onClick={submit}
          >
            {mode === 'create' ? 'Create' : 'Save changes'}
          </button>
        </div>
      </aside>
    </div>
  );
}

function RoleTextField({ label, value, onChange, testId }: { label: string; value: string; onChange: (value: string) => void; testId: string }): React.ReactElement {
  return (
    <label className="mt-3 block">
      <span className="text-xs font-semibold uppercase text-text-muted">{label}</span>
      <input
        className="mt-1 w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm text-text-primary"
        value={value}
        data-testid={testId}
        onChange={(e) => onChange(e.target.value)}
      />
    </label>
  );
}

function PermissionChecklist({ catalog, selected, onToggle }: { catalog: AccessPermissionDefinition[]; selected: string[]; onToggle: (permission: string) => void }): React.ReactElement {
  return (
    <div className="mt-3" data-testid="access-role-permissions">
      <div className="mb-1 flex items-center justify-between"><span className="text-xs font-semibold text-text-muted">Permission templates</span><AccessMetaPill>{selected.length} selected</AccessMetaPill></div>
      <div className="max-h-40 space-y-1 overflow-y-auto rounded border border-border-base p-2">
      {catalog.map((permission) => {
        const semantic = semanticPermissionFromDefinition(permission);
        return (
          <button
            key={permission.key}
            type="button"
            aria-pressed={selected.includes(permission.key)}
            className={[
              'flex w-full items-center justify-between gap-2 rounded px-2 py-1.5 text-left text-xs',
              selected.includes(permission.key) ? 'bg-status-emerald-bg text-status-emerald-fg' : 'text-text-secondary hover:bg-bg-subtle',
            ].join(' ')}
            onClick={() => onToggle(permission.key)}
          >
            <span className="min-w-0">
              <span className="block truncate font-semibold text-text-primary">{semantic.label}</span>
              <span className="block truncate font-mono text-[0.6875rem] text-text-muted">{permission.key}</span>
            </span>
            <AccessRiskBadge risk={permission.risk} />
          </button>
        );
      })}
      </div>
    </div>
  );
}

type MappingEntry = {
  team: TeamView;
  role: string;
  query: { data?: TeamRAMRoleMapping; isLoading: boolean; isError: boolean; error: unknown };
};

type MemberEntry = {
  team: TeamView;
  query: { data?: import('@/api/teams').MemberView[]; isLoading: boolean; isError: boolean };
};

function useRAMRoleReferences(mappingEntries: MappingEntry[]): Map<string, Array<{ team: TeamView; role: string }>> {
  return useMemo(() => {
    const result = new Map<string, Array<{ team: TeamView; role: string }>>();
    for (const entry of mappingEntries) {
      for (const roleID of entry.query.data?.ram_role_ids ?? []) {
        const current = result.get(roleID) ?? [];
        current.push({ team: entry.team, role: entry.role });
        result.set(roleID, current);
      }
    }
    return result;
  }, [mappingEntries]);
}

function SubjectDecisionView({
  decisions,
  grants,
  subjects,
  subjectByRef,
  permissionByKey,
  memberEntries,
  mappingEntries,
  onSelectSubject,
}: {
  decisions: AccessDecision[];
  grants: AccessGrant[];
  subjects: AccessSubject[];
  subjectByRef: Map<string, AccessSubject>;
  permissionByKey: Map<string, AccessPermissionDefinition>;
  memberEntries: MemberEntry[];
  mappingEntries: MappingEntry[];
  onSelectSubject: (subjectRef: string) => void;
}): React.ReactElement {
  const pageSize = 5;
  const [selectedSubjectRef, setSelectedSubjectRef] = useState('');
  const [page, setPage] = useState(0);
  const groups = useMemo(() => {
    const bySubject = new Map<string, AccessDecision[]>();
    for (const decision of decisions) {
      const rows = bySubject.get(decision.subject_ref) ?? [];
      rows.push(decision);
      bySubject.set(decision.subject_ref, rows);
    }
    return [...bySubject.entries()].sort(([aRef, aRows], [bRef, bRows]) => {
      const aSubject = subjectByRef.get(aRef);
      const bSubject = subjectByRef.get(bRef);
      const aDenied = aRows.filter((row) => row.status === 'denied').length;
      const bDenied = bRows.filter((row) => row.status === 'denied').length;
      if (aDenied !== bDenied) return bDenied - aDenied;
      return (aSubject?.name ?? aRef).localeCompare(bSubject?.name ?? bRef);
    });
  }, [decisions, grants, subjects, subjectByRef]);
  const selectedRef = selectedSubjectRef && groups.some(([ref]) => ref === selectedSubjectRef)
    ? selectedSubjectRef
    : groups[0]?.[0] ?? '';
  const selectedRows = groups.find(([ref]) => ref === selectedRef)?.[1] ?? [];
  const selectedSubject = subjectByRef.get(selectedRef) ?? subjects.find((subject) => subject.ref === selectedRef);
  const selectedGrants = grants.filter((grant) => grant.subject_ref === selectedRef);
  const subjectTeams = useMemo(() => subjectTeamBindings(selectedRef, memberEntries, mappingEntries), [selectedRef, memberEntries, mappingEntries]);
  const pageCount = Math.max(1, Math.ceil(groups.length / pageSize));
  const safePage = Math.min(page, pageCount - 1);
  const pageStart = safePage * pageSize;
  const visibleGroups = groups.slice(pageStart, pageStart + pageSize);
  const stats = decisionStats(selectedRows, selectedGrants);
  useEffect(() => {
    setPage(0);
  }, [groups.length]);
  useEffect(() => {
    if (selectedRef) onSelectSubject(selectedRef);
  }, [selectedRef, onSelectSubject]);
  if (groups.length === 0) {
    return <EmptyState title="No matching access decisions" body="Change the filters to widen the API query." testId="access-empty" />;
  }
  return (
    <section className="rounded border border-border-base bg-bg-elevated" data-testid="access-subject-view">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border-base px-4 py-3">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">Subject access workbench</h2>
          <p className="mt-1 text-xs text-text-muted">{groups.length} subjects from the filtered authorization read model.</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <AccessMetaPill>{stats.allowed} effective</AccessMetaPill>
          <AccessMetaPill>{stats.denied} explicit deny</AccessMetaPill>
          <AccessMetaPill>{stats.direct} direct</AccessMetaPill>
        </div>
      </div>
      <div className="grid min-h-[32rem] lg:grid-cols-[20rem_minmax(0,1fr)]">
        <div className="border-b border-border-base lg:border-b-0 lg:border-r">
          <div className="divide-y divide-border-base" data-testid="access-subject-list">
            {visibleGroups.map(([subjectRef, rows]) => {
              const subject = subjectByRef.get(subjectRef);
              const rowStats = decisionStats(rows, grants.filter((grant) => grant.subject_ref === subjectRef));
              const active = subjectRef === selectedRef;
              return (
                <button
                  key={subjectRef}
                  type="button"
                  className={[
                    'block w-full px-4 py-3 text-left hover:bg-bg-subtle',
                    active ? 'bg-brand/10 ring-1 ring-inset ring-brand/30' : '',
                  ].join(' ')}
                  onClick={() => {
                    setSelectedSubjectRef(subjectRef);
                    onSelectSubject(subjectRef);
                  }}
                  data-testid={`access-subject-row-${subjectRef}`}
                >
                  <span className="block truncate text-sm font-semibold text-text-primary">{subject?.name ?? subjectRef}</span>
                  <span className="mt-0.5 block truncate font-mono text-xs text-text-muted">{subjectRef}</span>
                  <span className="mt-2 flex flex-wrap gap-1">
                    <AccessMetaPill>{subject?.kind ?? 'subject'}</AccessMetaPill>
                    {subject?.status && <AccessMetaPill>{subject.status}</AccessMetaPill>}
                    <AccessMetaPill>{rowStats.allowed} allow</AccessMetaPill>
                    {rowStats.denied > 0 && <AccessMetaPill>{rowStats.denied} deny</AccessMetaPill>}
                  </span>
                  {rows.find((row) => !row.allowed)?.reason && (
                    <span className="mt-2 block text-xs text-text-secondary">{rows.find((row) => !row.allowed)?.reason}</span>
                  )}
                </button>
              );
            })}
          </div>
          <div className="flex items-center justify-between gap-2 border-t border-border-base px-4 py-3 text-xs text-text-muted" data-testid="access-subject-pagination">
            <button type="button" className="rounded border border-border-base px-2 py-1 disabled:opacity-50" disabled={pageStart === 0} onClick={() => setPage((value) => Math.max(0, value - 1))}>Previous</button>
            <span>Page {safePage + 1} of {pageCount}</span>
            <button type="button" className="rounded border border-border-base px-2 py-1 disabled:opacity-50" disabled={pageStart + pageSize >= groups.length} onClick={() => setPage((value) => Math.min(pageCount - 1, value + 1))}>Next</button>
          </div>
        </div>

        <div className="min-w-0" data-testid="access-subject-detail">
          <div className="flex flex-wrap items-start justify-between gap-2 border-b border-border-base px-4 py-3">
            <div>
              <h3 className="text-base font-semibold text-text-primary">{selectedSubject?.name ?? selectedRef}</h3>
              <p className="font-mono text-xs text-text-muted">{selectedRef}</p>
              {selectedSubject?.email && <p className="mt-1 text-xs text-text-secondary">{selectedSubject.email}</p>}
              {selectedSubject?.team_names && selectedSubject.team_names.length > 0 && (
                <p className="mt-1 text-xs text-text-secondary">{selectedSubject.team_names.join(', ')}</p>
              )}
            </div>
            <div className="flex flex-wrap gap-2">
              {selectedSubject?.role && <AccessMetaPill>{selectedSubject.role}</AccessMetaPill>}
              {selectedSubject?.status && <AccessMetaPill>{selectedSubject.status}</AccessMetaPill>}
              {selectedSubject?.kind && <AccessMetaPill>{selectedSubject.kind}</AccessMetaPill>}
            </div>
          </div>
          <div className="grid gap-3 border-b border-border-base p-4 sm:grid-cols-5" data-testid="access-subject-metrics">
            <SummaryTile label="Effective" value={stats.allowed} tone="success" />
            <SummaryTile label="Explicit deny" value={stats.denied} tone="danger" />
            <SummaryTile label="Direct union" value={stats.direct} tone="muted" />
            <SummaryTile label="High risk" value={stats.highRisk} tone="warning" />
            <SummaryTile label="Expiring" value={stats.expiring} tone="warning" />
          </div>
          <div className="grid gap-4 p-4 xl:grid-cols-2">
            <section className="rounded border border-border-base bg-bg-base p-3" data-testid="access-subject-why">
              <h4 className="text-xs font-semibold uppercase text-text-muted">Why access</h4>
              <div className="mt-2 space-y-2">
                {selectedRows.filter((row) => row.allowed).slice(0, 6).map((row) => (
                  <div key={`${row.permission}:${row.evidence_ref}`} className="rounded border border-border-base p-2 text-xs">
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-semibold text-text-primary">{semanticPermissionForDecision(row.permission, row.template, row.resource, permissionByKey).label}</span>
                      <AccessStatusBadge status="allowed" />
                    </div>
                    <dl className="mt-2 grid grid-cols-[4.5rem_1fr] gap-x-2 gap-y-1 text-text-secondary">
                      <dt className="font-semibold text-text-muted">Result</dt><dd>Allowed · {row.reason}</dd>
                      <dt className="font-semibold text-text-muted">Resource</dt><dd>{accessResourceLabel(row.resource)} <span className="font-mono">({row.resource.kind}:{row.resource.id})</span></dd>
                      <dt className="font-semibold text-text-muted">Key</dt><dd className="font-mono">{row.permission}</dd>
                      <dt className="font-semibold text-text-muted">Source chain</dt><dd className="font-mono">{sourceChain(row)} → final allow</dd>
                    </dl>
                  </div>
                ))}
                {selectedRows.every((row) => !row.allowed) && <p className="text-sm text-text-muted">No allowed decisions in the current filter.</p>}
              </div>
            </section>
            <section className="rounded border border-border-base bg-bg-base p-3" data-testid="access-explicit-deny">
              <h4 className="text-xs font-semibold uppercase text-text-muted">Explicit deny and N/A</h4>
              <div className="mt-2 space-y-2">
                {selectedRows.filter((row) => !row.allowed).map((row) => (
                  <div key={`${row.permission}:${row.status}:${row.evidence_ref}`} className="text-xs">
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-semibold text-text-primary">{semanticPermissionForDecision(row.permission, row.template, row.resource, permissionByKey).label}</span>
                      <AccessStatusBadge status={row.status ?? 'denied'} />
                    </div>
                    <div className="mt-1 text-text-secondary">{row.reason}</div>
                    <div className="font-mono text-text-muted">{row.permission} · {row.evidence_ref}</div>
                  </div>
                ))}
                {selectedRows.every((row) => row.allowed) && <p className="text-sm text-text-muted">No denied, unauthorized, or not-applicable decisions.</p>}
              </div>
            </section>
            <section className="rounded border border-border-base bg-bg-base p-3" data-testid="access-direct-binding-union-inline">
              <h4 className="text-xs font-semibold uppercase text-text-muted">Direct union</h4>
              <div className="mt-2 space-y-2">
                {selectedGrants.filter((grant) => grant.source === 'custom_role').map((grant) => (
                  <div key={grant.id} className="text-xs">
                    <div className="font-mono font-semibold text-text-primary">{grant.id}</div>
                    <div className="text-text-secondary">{semanticPermissionForDecision(grant.permission, grant.template, grant.resource, permissionByKey).label} on {accessResourceLabel(grant.resource)}</div>
                    <div className="text-text-muted">{grant.status} · expires {displayAccessDate(grant.expires_at)}</div>
                  </div>
                ))}
                {selectedGrants.filter((grant) => grant.source === 'custom_role').length === 0 && <p className="text-sm text-text-muted">No direct bindings in this subject slice.</p>}
              </div>
            </section>
            <section className="rounded border border-border-base bg-bg-base p-3" data-testid="access-subject-activity">
              <h4 className="text-xs font-semibold uppercase text-text-muted">Summary and activity</h4>
              <div className="mt-2 space-y-2 text-xs text-text-secondary">
                <p>{stats.allowed} effective permissions across {new Set(selectedRows.map((row) => accessResourceKey(row.resource))).size} resources.</p>
                <p>{selectedGrants.length} active or expiring grants are present for this subject.</p>
                <p>{subjectTeams.length} team-role bindings contribute to the current union.</p>
              </div>
            </section>
          </div>
          <details className="border-t border-border-base" data-testid={`access-subject-effective-${selectedRef}`} open>
            <summary className="cursor-pointer px-4 py-3 text-xs font-semibold text-text-secondary">
              {stats.allowed} effective permissions · full decision table
            </summary>
            <div className="grid gap-2 border-t border-border-base px-4 py-3 text-xs text-text-secondary md:grid-cols-2">
              {subjectTeams.map((binding) => (
                <p key={`${binding.team.id}:${binding.role}:detail`} className="rounded border border-border-base p-2">
                  <span className="font-mono text-text-primary">membership:{binding.team.name}</span> -&gt; Team Role <strong>{binding.role}</strong> -&gt; RAM Role {binding.ramRoleIDs.join(', ') || 'none'}
                </p>
              ))}
              {selectedRows.filter((row) => row.source === 'custom_role').map((row) => (
                <p key={`${row.permission}:${row.evidence_ref}:detail`} className="rounded border border-border-base p-2">
                  <span className="font-mono text-text-primary">direct binding</span> -&gt; grant {row.grant_id || roleIDFromEvidence(row.evidence_ref) || 'unknown'} -&gt; {semanticPermissionForDecision(row.permission, row.template, row.resource, permissionByKey).label} on {accessResourceLabel(row.resource)}
                </p>
              ))}
              {selectedRows.some((row) => !['team_member', 'team_role_ram', 'custom_role'].includes(row.source)) && (
                <p className="rounded border border-border-base p-2">
                  Other bindings: {[...new Set(selectedRows.filter((row) => !['team_member', 'team_role_ram', 'custom_role'].includes(row.source)).map((row) => row.source))].join(', ')}
                </p>
              )}
            </div>
            <DecisionTable decisions={selectedRows} subjectByRef={subjectByRef} permissionByKey={permissionByKey} compact />
          </details>
        </div>
      </div>
    </section>
  );
}

function decisionStats(rows: AccessDecision[], grants: AccessGrant[]): { allowed: number; denied: number; direct: number; highRisk: number; expiring: number } {
  return {
    allowed: rows.filter((row) => row.allowed).length,
    denied: rows.filter((row) => row.status === 'denied').length,
    direct: grants.filter((grant) => grant.source === 'custom_role').length,
    highRisk: rows.filter((row) => row.risk === 'high').length,
    expiring: grants.filter((grant) => grant.status === 'expires_soon').length,
  };
}

function sourceChain(row: AccessDecision): string {
  if (row.source === 'team_role_ram') return `membership → Team Role → RAM Role ${row.role_id || roleIDFromEvidence(row.evidence_ref) || 'unknown'}`;
  if (row.source === 'custom_role') return `direct binding → grant ${row.grant_id || roleIDFromEvidence(row.evidence_ref) || 'unknown'}`;
  return `${row.source} → ${row.evidence_ref}`;
}

function subjectTeamBindings(subjectRef: string, memberEntries: MemberEntry[], mappingEntries: MappingEntry[]): Array<{ team: TeamView; role: string; ramRoleIDs: string[] }> {
  return memberEntries.flatMap(({ team, query }) => (query.data ?? [])
    .filter((member) => member.member_ref === subjectRef)
    .flatMap((member) => (member.roles ?? [member.role]).map((role) => {
      const mapping = mappingEntries.find((entry) => entry.team.id === team.id && entry.role === role)?.query.data;
      return { team, role, ramRoleIDs: mapping?.ram_role_ids ?? [] };
    })));
}

function DecisionTable({
  decisions,
  subjectByRef,
  permissionByKey,
  compact = false,
}: {
  decisions: AccessDecision[];
  subjectByRef: Map<string, AccessSubject>;
  permissionByKey: Map<string, AccessPermissionDefinition>;
  compact?: boolean;
}): React.ReactElement {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[54rem] text-left text-sm" data-testid="access-decision-table">
        <thead className="border-b border-border-base text-[0.6875rem] uppercase text-text-muted">
          <tr>
            <th className="px-4 py-2 font-semibold">Subject</th>
            <th className="px-4 py-2 font-semibold">Permission</th>
            <th className="px-4 py-2 font-semibold">Resource</th>
            <th className="px-4 py-2 font-semibold">Status</th>
            <th className="px-4 py-2 font-semibold">Risk</th>
            <th className="px-4 py-2 font-semibold">Source</th>
            {!compact && <th className="px-4 py-2 font-semibold">Evidence</th>}
          </tr>
        </thead>
        <tbody>
          {decisions.map((decision) => {
            const subject = subjectByRef.get(decision.subject_ref);
            const permission = permissionByKey.get(decision.permission);
            const semantic = semanticPermissionForDecision(decision.permission, decision.template, decision.resource, permissionByKey);
            const rowKey = `${decision.subject_ref}-${decision.permission}-${accessResourceKey(decision.resource)}-${decision.source}-${decision.evidence_ref}-${decision.status}`;
            return (
              <tr key={rowKey} className="border-b border-border-base last:border-0">
                <td className="px-4 py-3">
                  <div className="font-medium text-text-primary">{subject?.name ?? decision.subject_ref}</div>
                  <div className="font-mono text-xs text-text-muted">{decision.subject_ref}</div>
                </td>
                <td className="px-4 py-3">
                  <div className="text-xs font-semibold text-text-primary">{semantic.label}</div>
                  <div className="font-mono text-[0.6875rem] text-text-muted">{decision.permission}</div>
                  {permission?.label && <div className="text-xs text-text-muted">{permission.label}</div>}
                </td>
                <td className="px-4 py-3">
                  <div className="font-medium text-text-primary">{accessResourceLabel(decision.resource)}</div>
                  <div className="text-xs text-text-muted">{decision.resource.kind}</div>
                </td>
                <td className="px-4 py-3">
                  <AccessStatusBadge status={decision.status ?? (decision.allowed ? 'allowed' : 'denied')} />
                  <div className="mt-1 max-w-xs text-xs text-text-muted">{decision.reason}</div>
                </td>
                <td className="px-4 py-3"><AccessRiskBadge risk={decision.risk ?? permission?.risk ?? 'low'} /></td>
                <td className="px-4 py-3 font-mono text-xs text-text-secondary">{decision.source}</td>
                {!compact && <td className="px-4 py-3 font-mono text-xs text-text-muted">{decision.evidence_ref}</td>}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function SubjectAccessSidebar({
  decisions,
  grants,
  subjects,
  selectedSubjectRef,
  permissionByKey,
  memberEntries,
  mappingEntries,
}: {
  decisions: AccessDecision[];
  grants: AccessGrant[];
  subjects: AccessSubject[];
  selectedSubjectRef: string;
  permissionByKey: Map<string, AccessPermissionDefinition>;
  memberEntries: MemberEntry[];
  mappingEntries: MappingEntry[];
}): React.ReactElement {
  const subjectRef = selectedSubjectRef || decisions[0]?.subject_ref || subjects[0]?.ref || '';
  const subject = subjects.find((entry) => entry.ref === subjectRef);
  const rows = decisions.filter((decision) => decision.subject_ref === subjectRef);
  const audit = usePermissionAudit(subjectRef, Boolean(subjectRef));
  const effectiveRows = rows.filter((row) => row.allowed);
  const direct = grants.filter((grant) => grant.subject_ref === subjectRef && grant.source === 'custom_role');
  const teamBindings = subjectTeamBindings(subjectRef, memberEntries, mappingEntries);
  const deniedRows = rows.filter((row) => row.status === 'denied');
  const notApplicableRows = rows.filter((row) => row.status === 'not_applicable');
  return (
    <section className="rounded border border-border-base bg-bg-elevated" data-testid="access-subject-sidebar">
      <div className="border-b border-border-base px-4 py-3">
        <h2 className="text-sm font-semibold text-text-primary">Trace + audit</h2>
        <p className="mt-1 font-mono text-xs text-text-muted">{subject?.name ?? subjectRef}</p>
      </div>
      <div className="space-y-4 p-4">
        <div data-testid="access-permission-trace">
          <h3 className="text-xs font-semibold uppercase text-text-muted">Decision trace</h3>
          {rows.length === 0 && teamBindings.length === 0 && direct.length === 0 ? (
            <p className="mt-2 text-sm text-text-muted">No decisions in the current filter.</p>
          ) : (
            <div className="mt-2 space-y-2">
              {teamBindings.map((binding) => (
                <div key={`${binding.team.id}:${binding.role}`} className="rounded border border-border-base bg-bg-base p-2 text-xs">
                  <div className="font-semibold text-text-primary">Membership → Team Role → RAM Role</div>
                  <p className="mt-1 text-text-secondary">{binding.team.name} → {binding.role} → {binding.ramRoleIDs.join(', ') || 'none'}</p>
                </div>
              ))}
              {direct.map((grant) => (
                <div key={`${grant.id}:trace`} className="rounded border border-border-base bg-bg-base p-2 text-xs">
                  <div className="font-semibold text-text-primary">Direct union</div>
                  <p className="mt-1 text-text-secondary">direct binding → grant {grant.id} → {semanticPermissionForDecision(grant.permission, grant.template, grant.resource, permissionByKey).label}</p>
                </div>
              ))}
              {deniedRows.map((row) => (
                <div key={`${row.permission}:${row.evidence_ref}:deny`} className="rounded border border-danger/40 bg-danger/5 p-2 text-xs">
                  <div className="font-semibold text-danger">Explicit deny</div>
                  <p className="mt-1 text-text-secondary">{semanticPermissionForDecision(row.permission, row.template, row.resource, permissionByKey).label} → {row.evidence_ref}</p>
                </div>
              ))}
              {effectiveRows.slice(0, 8).map((row) => (
                <div key={`${row.permission}:${row.source}:${row.evidence_ref}`} className="rounded border border-border-base bg-bg-base p-2 text-xs">
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-semibold text-text-primary">{semanticPermissionForDecision(row.permission, row.template, row.resource, permissionByKey).label}</span>
                    <AccessRiskBadge risk={row.risk ?? permissionByKey.get(row.permission)?.risk ?? 'low'} />
                  </div>
                  <p className="mt-1 text-text-secondary">
                    Final → allowed · {sourceChain(row)}
                  </p>
                  <p className="mt-1 font-mono text-[0.6875rem] text-text-muted">{row.permission} · {row.evidence_ref}</p>
                </div>
              ))}
              {deniedRows.map((row) => (
                <div key={`${row.permission}:${row.evidence_ref}:final`} className="rounded border border-danger/40 bg-danger/5 p-2 text-xs">
                  <div className="font-semibold text-danger">{semanticPermissionForDecision(row.permission, row.template, row.resource, permissionByKey).label}</div>
                  <p className="mt-1 text-text-secondary">Final → denied · explicit deny takes precedence over inherited and direct allows.</p>
                </div>
              ))}
              {notApplicableRows.map((row) => (
                <div key={`${row.permission}:${row.evidence_ref}:na`} className="rounded border border-warning/40 bg-warning/5 p-2 text-xs">
                  <div className="font-semibold text-warning">{semanticPermissionForDecision(row.permission, row.template, row.resource, permissionByKey).label}</div>
                  <p className="mt-1 text-text-secondary">Final → N/A · {row.reason}</p>
                </div>
              ))}
            </div>
          )}
        </div>
        <div data-testid="access-direct-binding-union">
          <h3 className="text-xs font-semibold uppercase text-text-muted">Direct binding union</h3>
          {direct.length === 0 ? (
            <p className="mt-2 text-sm text-text-muted">No direct bindings in the current filter.</p>
          ) : (
            <div className="mt-2 space-y-2">
              {direct.map((grant) => (
                <div key={grant.id} className="rounded border border-border-base bg-bg-base p-2 text-xs">
                  <div className="font-semibold text-text-primary">{semanticPermissionForDecision(grant.permission, grant.template, grant.resource, permissionByKey).label}</div>
                  <div className="mt-1 text-text-secondary">{accessResourceLabel(grant.resource)} · {displayAccessDate(grant.expires_at)}</div>
                  <div className="mt-1 font-mono text-text-muted">{grant.permission} · {grant.id}</div>
                </div>
              ))}
            </div>
          )}
        </div>
        <AuditHistory events={audit.data?.events ?? []} loading={audit.isLoading} error={audit.error as Error | null} />
      </div>
    </section>
  );
}

function AuditHistory({ events, loading, error }: { events: PermissionAuditEvent[]; loading: boolean; error: Error | null }): React.ReactElement {
  return (
    <div data-testid="access-audit-history">
      <h3 className="text-xs font-semibold uppercase text-text-muted">Audit history</h3>
      {loading && <p className="mt-2 text-sm text-text-muted">Loading audit…</p>}
      {error && <p className="mt-2 text-sm text-danger" role="alert">{error.message}</p>}
      {!loading && !error && events.length === 0 && <p className="mt-2 text-sm text-text-muted">No audit events.</p>}
      <div className="mt-2 space-y-2">
        {events.slice(0, 6).map((event) => (
          <div key={event.id} className="rounded border border-border-base bg-bg-base p-2 text-xs">
            <div className="font-mono font-semibold text-text-primary">{event.event_type}</div>
            <div className="mt-1 text-text-secondary">{event.actor_ref} · {displayAccessDate(event.created_at)}</div>
            <div className="mt-1 font-mono text-text-muted">{event.assignment_id || event.role_id || event.request_id || event.id}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

function GrantRevoke({ grants, subjectRef, permissionByKey, canManageAccess, onToast }: { grants: AccessGrant[]; subjectRef: string; permissionByKey: Map<string, AccessPermissionDefinition>; canManageAccess: boolean; onToast: (toast: AccessToast) => void }): React.ReactElement {
  const revoke = useAccessBulkRevoke();
  const previewRevoke = useAccessRevokePreview();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [reason, setReason] = useState('access cleanup');
  const [preview, setPreview] = useState<((AccessBatchPreview & { preview_id: string; token: string }) & { grant_ids: string[]; reason: string; message: string; idempotency_key: string }) | null>(null);
  const directGrants = grants.filter((grant) => grant.subject_ref === subjectRef && grant.source === 'custom_role');
  const toggle = (id: string): void => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };
  const selectedIds = [...selected];
  return (
    <section className="rounded border border-border-base bg-bg-elevated" data-testid="access-grants">
      <div className="flex items-center justify-between gap-2 border-b border-border-base px-4 py-3">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">Direct bindings</h2>
          <p className="mt-1 font-mono text-[0.6875rem] text-text-muted">{subjectRef || 'Select a subject'}</p>
        </div>
        <button
          type="button"
          disabled={!canManageAccess || selectedIds.length === 0 || previewRevoke.isPending || !reason.trim()}
          onClick={() => {
            const grant_ids = selectedIds;
            const message = reason.trim();
            previewRevoke.mutate({ grant_ids, reason: message, message }, {
              onSuccess: (data) => setPreview({
                ...data,
                grant_ids,
                reason: message,
                message,
                idempotency_key: `access-revoke-${data.preview_id}`,
              }),
              onError: (error) => onToast(accessToastFromError(error, 'Revoke preview failed')),
            });
          }}
          className="inline-flex items-center gap-1 rounded border border-danger/40 px-2.5 py-1.5 text-xs font-semibold text-danger hover:bg-danger/10 disabled:opacity-50"
          data-testid="access-revoke-preview"
        >
          <IconTrash className="h-3.5 w-3.5" />
          Preview revoke
        </button>
      </div>
      <div className="space-y-3 p-4">
        <label className="block">
          <span className="text-xs font-semibold uppercase text-text-muted">Reason</span>
          <input
            className="mt-1 w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm text-text-primary"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </label>
        <div className="overflow-x-auto rounded border border-border-base">
          <table className="w-full min-w-[22rem] text-left text-sm">
            <thead className="border-b border-border-base text-[0.6875rem] uppercase text-text-muted">
              <tr>
                <th className="w-9 px-2 py-2 font-semibold">Select</th>
                <th className="px-2 py-2 font-semibold">Grant</th>
              </tr>
            </thead>
            <tbody>
              {directGrants.map((grant) => (
                <tr key={grant.id} className="border-b border-border-base last:border-0">
                  <td className="px-2 py-3 align-top">
                    <input
                      type="checkbox"
                      checked={selected.has(grant.id)}
                      disabled={!canManageAccess}
                      onChange={() => toggle(grant.id)}
                      aria-label={`Select ${semanticPermissionForDecision(grant.permission, grant.template, grant.resource, permissionByKey).label} for revoke`}
                      data-testid="access-grant-select"
                    />
                  </td>
                  <td className="min-w-0 px-2 py-3">
                    <span className="block text-xs font-semibold text-text-primary">{semanticPermissionForDecision(grant.permission, grant.template, grant.resource, permissionByKey).label}</span>
                    <span className="block truncate text-xs text-text-secondary">
                      {grant.subject_name}
                      {' -> '}
                      {accessResourceLabel(grant.resource)}
                    </span>
                    <span className="mt-1 block truncate font-mono text-[0.6875rem] text-text-muted">{grant.permission}</span>
                    <span className="mt-1 flex flex-wrap gap-1">
                      <AccessRiskBadge risk={grant.risk} />
                      <AccessMetaPill>{grant.source}</AccessMetaPill>
                      <AccessMetaPill>{grant.status}</AccessMetaPill>
                    </span>
                    <span className="mt-1 block text-xs text-text-muted">
                      <IconCalendar className="mr-1 inline h-3.5 w-3.5" />
                      {displayAccessDate(grant.expires_at)}
                    </span>
                  </td>
                </tr>
              ))}
              {directGrants.length === 0 && (
                <tr><td colSpan={2} className="px-3 py-6 text-center text-sm text-text-muted">No direct bindings for this subject. Inherited access is read-only here and must be changed at its source.</td></tr>
              )}
            </tbody>
          </table>
        </div>
        {preview && (
          <div className="rounded border border-warning/40 bg-warning/5 p-2" data-testid="access-revoke-preview-panel">
            <ResultPanel result={preview} title="Revoke preview" />
            <button
              type="button"
              className="mt-2 rounded bg-danger px-2.5 py-1.5 text-xs font-semibold text-white disabled:opacity-50"
              data-testid="access-revoke-confirm"
              disabled={!canManageAccess || revoke.isPending}
              onClick={() => revoke.mutate({
                grant_ids: preview.grant_ids,
                reason: preview.reason,
                message: preview.message,
                preview_id: preview.preview_id,
                token: preview.token,
                idempotency_key: preview.idempotency_key,
              }, {
                onSuccess: (data) => onToast({ tone: data.summary.partial_failure ? 'warning' : 'success', message: data.summary.partial_failure ? 'Revoke completed with partial failure' : 'Revoke completed' }),
                onError: (error) => onToast(accessToastFromError(error, 'Revoke failed')),
              })}
            >
              Confirm revoke
            </button>
          </div>
        )}
        {revoke.data && <ResultPanel result={revoke.data} title="Revoke result" />}
        {previewRevoke.isError && <p className="text-xs text-danger" role="alert">{(previewRevoke.error as Error).message}</p>}
        {revoke.isError && <p className="text-xs text-danger" role="alert">{(revoke.error as Error).message}</p>}
      </div>
    </section>
  );
}

function BatchGrantDrawer({
  subjects,
  roles,
  permissions,
  resources,
  decisions,
  teams,
  canManageAccess,
  contextSubjectRef,
  onToast,
  onClose,
  mode = 'dialog',
}: {
  subjects: AccessSubject[];
  roles: AccessRole[];
  permissions: AccessPermissionDefinition[];
  resources: AccessResourceScope[];
  decisions: AccessDecision[];
  teams: TeamView[];
  canManageAccess: boolean;
  contextSubjectRef: string;
  onToast: (toast: AccessToast) => void;
  onClose: () => void;
  mode?: 'dialog' | 'page';
}): React.ReactElement {
  const containerRef = useModalA11y({ open: mode === 'dialog', onClose });
  const previewMutation = useAccessBatchPreview();
  const applyMutation = useAccessBatchApply();
  const [step, setStep] = useState(0);
  const [request, setRequest] = useState<AccessBatchRequest>(() => emptyBatchRequest(contextSubjectRef));
  const [preview, setPreview] = useState<AccessBatchPreview | null>(null);
  const [result, setResult] = useState<AccessBatchResult | null>(null);
  const [highRiskAck, setHighRiskAck] = useState(false);
  const [subjectQuery, setSubjectQuery] = useState('');
  const [subjectProjectID, setSubjectProjectID] = useState('all');
  const [subjectTeamName, setSubjectTeamName] = useState('all');
  const [permissionQuery, setPermissionQuery] = useState('');
  const [selectedPickerIDs, setSelectedPickerIDs] = useState<string[]>([]);
  const [pickerResources, setPickerResources] = useState<Record<string, string>>({});
  const [customPickerResources, setCustomPickerResources] = useState<Record<string, AccessResourceScope>>({});
  const [collapsedPickerGroups, setCollapsedPickerGroups] = useState<Record<string, boolean>>({});
  const [scopePickerRowID, setScopePickerRowID] = useState<string | null>(null);
  const [grantEntries, setGrantEntries] = useState<GrantEntry[]>([]);
  const [selectedGrantIDs, setSelectedGrantIDs] = useState<string[]>([]);
  const [grantNotice, setGrantNotice] = useState<{ tone: 'success' | 'warning'; message: string } | null>(null);
  const title = 'Batch authorization';
  const assignableRoles = useMemo(() => assignableRAMRoles(roles), [roles]);
  const directTemplates = useMemo(() => buildDirectGrantTemplates(permissions), [permissions]);
  const grantableResourceKinds = useMemo<Set<string>>(() => new Set(resources.map((resource) => resource.kind)), [resources]);
  const canGrantToKinds = (kinds: string[]): boolean => {
    if (kinds.includes('mixed')) return resources.length > 0;
    return kinds.some((kind) => grantableResourceKinds.has(kind));
  };
  const pickerRows = useMemo<PermissionPickerRow[]>(() => [
    ...assignableRoles.flatMap((role) => {
      const compatibleKinds = [...new Set([ramRoleScope(role), 'team'])];
      if (!canGrantToKinds(compatibleKinds)) return [];
      return [{
      id: `role:${role.id}`,
      kind: 'role' as const,
      label: role.name,
      resource: 'Role template',
      permission: role.name,
      scope: accessResourceKindLabel(ramRoleScope(role)),
      action: 'Assign',
      detail: roleTemplateDetail(role),
      risk: role.high_risk ? 'high' as AccessRisk : accessRoleRiskForUI(role, permissions),
      role,
      compatibleKinds,
      }];
    }),
    ...directTemplates.flatMap((template) => {
      const compatibleKinds = templateCompatibleKinds(template);
      if (!canGrantToKinds(compatibleKinds)) return [];
      return [{
      id: `permission:${template.id}`,
      kind: 'permission' as const,
      label: directTemplateLabel(template),
      resource: template.resource,
      permission: template.permissionKey,
      scope: template.scope,
      action: template.action,
      detail: template.description,
      risk: template.risk,
      template,
      compatibleKinds,
      }];
    }),
  ], [assignableRoles, directTemplates, grantableResourceKinds, permissions, resources.length]);
  const filteredPickerRows = useMemo(() => {
    const q = permissionQuery.trim().toLowerCase();
    if (!q) return pickerRows;
    return pickerRows.filter((row) => [row.label, row.resource, row.permission, row.scope, row.action, row.detail]
      .join(' ')
      .toLowerCase()
      .includes(q));
  }, [permissionQuery, pickerRows]);
  const pickerGroups = useMemo(() => {
    const byResource = new Map<string, PermissionPickerRow[]>();
    for (const row of filteredPickerRows) {
      const group = byResource.get(row.resource) ?? [];
      group.push(row);
      byResource.set(row.resource, group);
    }
    return [...byResource.entries()]
      .sort((a, b) => comparePickerResource(a[0], b[0]))
      .map(([resource, rows]) => ({
        resource,
        rows: rows.sort((a, b) => `${a.scope}:${a.action}:${a.label}`.localeCompare(`${b.scope}:${b.action}:${b.label}`)),
      }));
  }, [filteredPickerRows]);
  const resourceByKey = useMemo(
    () => new Map([...resources, ...Object.values(customPickerResources)].map((resource) => [accessResourceKey(resource), resource])),
    [customPickerResources, resources],
  );
  const projectSubjectRefs = useMemo(() => {
    const byProject = new Map<string, Set<string>>();
    for (const decision of decisions) {
      const id = projectIDForAccessResource(decision.resource);
      if (!id) continue;
      if (!byProject.has(id)) byProject.set(id, new Set());
      if (decision.status === 'allowed') byProject.get(id)?.add(decision.subject_ref);
    }
    return byProject;
  }, [decisions]);
  const subjectProjectOptions = useMemo(() => {
    const byID = new Map<string, string>();
    for (const resource of resources) {
      const id = projectIDForAccessResource(resource);
      if (id) byID.set(id, resource.label || id);
    }
    return [{ value: 'all', label: 'All projects' }, ...[...byID.entries()].sort((a, b) => a[1].localeCompare(b[1])).map(([value, label]) => ({ value, label }))];
  }, [resources]);
  const subjectTeamOptions = useMemo(() => {
    const names = new Set<string>();
    for (const subject of subjects) {
      for (const name of subject.team_names ?? []) names.add(name);
    }
    for (const tm of teams) names.add(tm.name);
    return [{ value: 'all', label: 'All teams' }, ...[...names].sort().map((name) => ({ value: name, label: name }))];
  }, [subjects, teams]);
  const visibleSubjects = useMemo(() => {
    const q = subjectQuery.trim().toLowerCase();
    return subjects.filter((subject) => {
      if (q) {
        const haystack = [subject.name, subject.ref, subject.email ?? '', subject.role ?? '', ...(subject.team_names ?? [])].join(' ').toLowerCase();
        if (!haystack.includes(q)) return false;
      }
      if (subjectTeamName !== 'all' && !(subject.team_names ?? []).includes(subjectTeamName)) return false;
      if (subjectProjectID !== 'all') {
        const refs = projectSubjectRefs.get(subjectProjectID);
        const isTeamRole = subject.kind === 'team_role' && subject.team_names?.some((name) => subjectTeamName === 'all' || name === subjectTeamName);
        if (!refs?.has(subject.ref) && !isTeamRole) return false;
      }
      return true;
    });
  }, [projectSubjectRefs, subjectProjectID, subjectQuery, subjectTeamName, subjects]);
  const roleIDs = [...new Set(grantEntries.flatMap((entry) => entry.roleId ? [entry.roleId] : []))];
  const permissionKeys = [...new Set(grantEntries.flatMap((entry) => entry.permissionKey ? [entry.permissionKey] : []))];
  const entryResources = uniqueAccessResources(grantEntries.map((entry) => entry.resource));
  const entries: AccessBatchGrantEntry[] = grantEntries.map((entry) => ({
    role_id: entry.roleId,
    permission_key: entry.permissionKey,
    resource: entry.resource,
  }));
  const effectiveRequest: AccessBatchRequest = {
    ...request,
    role_ids: roleIDs,
    permission_keys: permissionKeys,
    resources: entryResources,
    entries,
  };
  const previewMissing = previewDisabledReason(canManageAccess, request, grantEntries.length);
  const canPreview =
    canManageAccess &&
    request.subject_refs.length > 0 &&
    grantEntries.length > 0 &&
    request.reason.trim().length > 0;
  const canConfirm = canManageAccess && !!preview && (preview.summary.high_risk === 0 || highRiskAck);

  const toggleSubject = (ref: string): void => {
    setRequest((prev) => ({ ...prev, subject_refs: toggleValue(prev.subject_refs, ref) }));
  };
  const toggleAllVisibleSubjects = (): void => {
    const visibleRefs = visibleSubjects.map((subject) => subject.ref);
    const allSelected = visibleRefs.length > 0 && visibleRefs.every((ref) => request.subject_refs.includes(ref));
    setRequest((prev) => ({
      ...prev,
      subject_refs: allSelected
        ? prev.subject_refs.filter((ref) => !visibleRefs.includes(ref))
        : [...new Set([...prev.subject_refs, ...visibleRefs])],
    }));
  };
  const compatibleResources = (kinds: string[]): AccessResourceScope[] => resources.filter((resource) => kinds.includes('mixed') || kinds.includes(resource.kind));
  const scopePickerRow = scopePickerRowID ? pickerRows.find((row) => row.id === scopePickerRowID) : undefined;
  const scopePickerResources = scopePickerRow ? compatibleResources(scopePickerRow.compatibleKinds) : [];
  const visibleSelectedPickerRows = filteredPickerRows.filter((row) => selectedPickerIDs.includes(row.id));
  const filteredPickerIDs = filteredPickerRows.map((row) => row.id);
  const allFilteredPickerRowsSelected = filteredPickerRows.length > 0 && filteredPickerRows.every((row) => selectedPickerIDs.includes(row.id));
  const toggleAllFilteredPickerRows = (): void => {
    setSelectedPickerIDs((prev) => allFilteredPickerRowsSelected
      ? prev.filter((id) => !filteredPickerIDs.includes(id))
      : [...new Set([...prev, ...filteredPickerIDs])]);
  };
  const addPickerRows = (rows = visibleSelectedPickerRows): void => {
    const next: GrantEntry[] = [];
    let skipped = 0;
    for (const row of rows) {
      const resource = resourceByKey.get(pickerResources[row.id]) ?? compatibleResources(row.compatibleKinds)[0];
      if (!resource) {
        skipped += 1;
        continue;
      }
      if (row.kind === 'role' && !row.role) {
        skipped += 1;
        continue;
      }
      if (row.kind === 'permission' && !row.template) {
        skipped += 1;
        continue;
      }
      next.push({
        id: `grant:${row.id}:${accessResourceKey(resource)}:${Date.now()}:${next.length}`,
        kind: row.kind,
        roleId: row.kind === 'role' ? row.role?.id : undefined,
        roleName: row.kind === 'role' ? row.role?.name : undefined,
        permissionKey: row.kind === 'permission' && row.template ? permissionKeyForTemplateResource(row.template, resource) : undefined,
        template: row.kind === 'permission' ? row.template : undefined,
        resource,
        risk: row.risk,
      });
    }
    if (next.length > 0) {
      const addedPickerIDs = new Set(rows.map((row) => row.id));
      setGrantEntries((prev) => [...prev, ...next]);
      setSelectedPickerIDs((prev) => prev.filter((id) => !addedPickerIDs.has(id)));
      setGrantNotice({ tone: skipped > 0 ? 'warning' : 'success', message: skipped > 0 ? `Added ${next.length}; ${skipped} had no compatible scope.` : `Added ${next.length} grant ${next.length === 1 ? 'entry' : 'entries'}.` });
      return;
    }
    setGrantNotice({ tone: 'warning', message: rows.length === 0 ? 'Select a visible permission before adding.' : 'No grant entry was added. Choose a compatible scope first.' });
  };
  const removeSelectedGrants = (): void => {
    setGrantEntries((prev) => prev.filter((entry) => !selectedGrantIDs.includes(entry.id)));
    setSelectedGrantIDs([]);
    setGrantNotice({ tone: 'success', message: `Deleted ${selectedGrantIDs.length} grant ${selectedGrantIDs.length === 1 ? 'entry' : 'entries'}.` });
  };
  const runPreview = (): void => {
    previewMutation.mutate(effectiveRequest, {
      onSuccess: (data) => {
        setPreview(data);
        setResult(null);
        setHighRiskAck(false);
        setStep(1);
      },
    });
  };
  const runApply = (): void => {
    applyMutation.mutate(
      { ...effectiveRequest, preview_request_id: preview?.request_id },
      {
        onSuccess: (data) => {
          setResult(data);
          setStep(3);
          onToast({
            tone: data.summary.partial_failure ? 'warning' : 'success',
            message: data.summary.partial_failure ? 'Batch grant completed with partial failure' : 'Batch grant applied',
          });
        },
        onError: (error) => onToast(accessToastFromError(error, 'Batch grant failed')),
      },
    );
  };

  return (
    <div className={mode === 'dialog' ? 'fixed inset-0 z-50 bg-black/30' : 'flex min-h-0 min-w-0 flex-1 overflow-hidden'} data-testid="access-batch-drawer-backdrop">
      <div
        ref={containerRef}
        role={mode === 'dialog' ? 'dialog' : undefined}
        aria-modal={mode === 'dialog' ? 'true' : undefined}
        aria-label={title}
        className={mode === 'dialog'
          ? 'fixed inset-x-4 top-6 mx-auto flex max-h-[calc(100vh-3rem)] max-w-6xl flex-col rounded border border-border-base bg-bg-elevated text-text-primary shadow-2xl'
          : 'flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded border border-border-base bg-bg-elevated text-text-primary'}
        data-testid="access-batch-drawer"
      >
        <div className="flex items-start justify-between gap-3 border-b border-border-base px-5 py-4">
          <div>
            <h2 className="text-lg font-semibold">{title}</h2>
            <div className="mt-2 flex flex-wrap gap-1">
              {['Scope', 'Preview', 'Confirm', 'Result'].map((label, idx) => (
                <span
                  key={label}
                  className={[
                    'rounded px-2 py-1 text-[0.6875rem] font-semibold',
                    step === idx ? 'bg-brand text-white' : step > idx ? 'bg-status-emerald-bg text-status-emerald-fg' : 'bg-bg-subtle text-text-secondary',
                  ].join(' ')}
                >
                  {idx + 1}. {label}
                </span>
              ))}
            </div>
          </div>
          <button
            type="button"
            aria-label={mode === 'dialog' ? 'Close' : 'Back to Subject access'}
            title={mode === 'dialog' ? 'Close' : 'Back to Subject access'}
            className="rounded p-1.5 text-text-secondary hover:bg-bg-subtle"
            onClick={onClose}
          >
            <IconClose />
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-5">
          {step === 0 && (
            <div className="space-y-4">
              <section className="rounded border border-border-base bg-bg-base" data-testid="access-grant-subjects">
                <div className="flex flex-wrap items-end gap-2 border-b border-border-base px-3 py-3">
                  <label className="min-w-0 flex-[1_1_14rem]">
                    <span className="text-xs font-semibold uppercase text-text-muted">Keyword</span>
                    <input className="mt-1 w-full rounded border border-border-base bg-bg-elevated px-2 py-1.5 text-sm" value={subjectQuery} onChange={(e) => setSubjectQuery(e.target.value)} data-testid="access-grant-subject-keyword" />
                  </label>
                  <Select label="Project" value={subjectProjectID} onChange={setSubjectProjectID} options={subjectProjectOptions} />
                  <Select label="Team" value={subjectTeamName} onChange={setSubjectTeamName} options={subjectTeamOptions} />
                </div>
                <div className="max-h-52 overflow-auto">
                  <table className="w-full min-w-[44rem] text-left text-sm" data-testid="access-grant-subject-table">
                    <thead className="border-b border-border-base text-[0.6875rem] uppercase text-text-muted">
                      <tr>
                        <th className="w-10 px-3 py-2"><input type="checkbox" checked={visibleSubjects.length > 0 && visibleSubjects.every((subject) => request.subject_refs.includes(subject.ref))} onChange={toggleAllVisibleSubjects} aria-label="Select all visible subjects" /></th>
                        <th className="px-3 py-2 font-semibold">Subject</th>
                        <th className="px-3 py-2 font-semibold">Type</th>
                        <th className="px-3 py-2 font-semibold">Team</th>
                        <th className="px-3 py-2 font-semibold">Status</th>
                      </tr>
                    </thead>
                    <tbody>
                      {visibleSubjects.map((subject) => (
                        <tr key={subject.ref} className="border-b border-border-base last:border-0">
                          <td className="px-3 py-2"><input type="checkbox" checked={request.subject_refs.includes(subject.ref)} onChange={() => toggleSubject(subject.ref)} data-testid="access-grant-subject-select" /></td>
                          <td className="px-3 py-2"><div className="font-medium">{subject.name}</div><div className="font-mono text-xs text-text-muted">{subject.ref}</div></td>
                          <td className="px-3 py-2">{subject.kind === 'team_role' ? 'team role' : subject.kind}</td>
                          <td className="px-3 py-2 text-xs text-text-secondary">{(subject.team_names ?? []).join(', ') || '—'}</td>
                          <td className="px-3 py-2">{subject.status ?? 'unknown'}</td>
                        </tr>
                      ))}
                      {visibleSubjects.length === 0 && <tr><td colSpan={5} className="px-3 py-6 text-center text-sm text-text-muted">No subjects match the current filters.</td></tr>}
                    </tbody>
                  </table>
                </div>
              </section>
              <div className="grid min-w-0 gap-4 2xl:grid-cols-[minmax(0,1.15fr)_minmax(22rem,0.85fr)]">
                <section className="min-w-0 rounded border border-border-base bg-bg-base" data-testid="access-permission-picker">
                  <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border-base px-3 py-2">
                    <h3 className="text-xs font-semibold uppercase text-text-muted">Permission picker</h3>
                    <div className="flex min-w-0 flex-[1_1_22rem] items-center justify-end gap-2">
                      <label className="min-w-0 flex-1">
                        <span className="sr-only">Filter permissions</span>
                        <input
                          className="w-full rounded border border-border-base bg-bg-elevated px-2 py-1.5 text-sm"
                          value={permissionQuery}
                          onChange={(e) => setPermissionQuery(e.target.value)}
                          placeholder="Filter permissions"
                          data-testid="access-picker-keyword"
                        />
                      </label>
                      <button type="button" className="shrink-0 rounded border border-border-base px-2 py-1 text-xs font-semibold disabled:opacity-50" disabled={visibleSelectedPickerRows.length === 0} onClick={() => addPickerRows()} data-testid="access-add-selected-grants">Add selected{visibleSelectedPickerRows.length > 0 ? ` (${visibleSelectedPickerRows.length})` : ''}</button>
                    </div>
                  </div>
                  <div className="max-h-80 min-w-0 overflow-auto">
                    <table className="w-full min-w-[36rem] text-left text-sm">
                      <thead className="border-b border-border-base text-[0.6875rem] uppercase text-text-muted">
                        <tr>
                          <th className="w-10 px-3 py-2"><input type="checkbox" checked={allFilteredPickerRowsSelected} onChange={toggleAllFilteredPickerRows} aria-label="Select all visible permissions" /></th>
                          <th className="px-3 py-2 font-semibold">Resource</th>
                          <th className="px-3 py-2 font-semibold">Permission</th>
                          <th className="px-3 py-2 font-semibold">Scope</th>
                          <th className="px-3 py-2 font-semibold">Risk</th>
                          <th className="px-3 py-2 font-semibold">Action</th>
                        </tr>
                      </thead>
                      <tbody>
                        {pickerGroups.map((group) => {
                          const groupToken = accessTestIDToken(group.resource);
                          const collapsed = collapsedPickerGroups[group.resource] ?? false;
                          return (
                            <Fragment key={group.resource}>
                              <tr className="border-b border-border-base bg-bg-subtle/70" data-testid={`access-picker-group-${groupToken}`}>
                                <td colSpan={6} className="px-3 py-2">
                                  <button
                                    type="button"
                                    className="flex w-full items-center justify-between gap-3 text-left"
                                    onClick={() => setCollapsedPickerGroups((prev) => ({ ...prev, [group.resource]: !collapsed }))}
                                    aria-expanded={!collapsed}
                                    data-testid={`access-picker-group-toggle-${groupToken}`}
                                  >
                                    <span className="text-xs font-semibold uppercase text-text-secondary">{group.resource}</span>
                                    <span className="text-xs text-text-muted">{collapsed ? 'Show' : 'Hide'} {group.rows.length}</span>
                                  </button>
                                </td>
                              </tr>
                              {!collapsed && group.rows.map((row) => {
                                const rowResources = compatibleResources(row.compatibleKinds);
                                const selectedResourceKey = pickerResources[row.id] || (rowResources[0] ? accessResourceKey(rowResources[0]) : '');
                                return (
                                  <tr key={row.id} className="border-b border-border-base last:border-0" data-testid={`access-picker-row-${accessTestIDToken(row.id)}`}>
                                    <td className="px-3 py-2"><input type="checkbox" checked={selectedPickerIDs.includes(row.id)} onChange={() => setSelectedPickerIDs((prev) => toggleValue(prev, row.id))} data-testid={`access-picker-select-${accessTestIDToken(row.id)}`} /></td>
                                    <td className="px-3 py-2">{row.resource}</td>
                                    <td className="px-3 py-2"><div className="font-medium">{row.label}</div><div className="text-xs text-text-muted">{row.detail}</div></td>
                                    <td className="px-3 py-2">
                                      <select className="sr-only" aria-hidden="true" tabIndex={-1} value={selectedResourceKey} onChange={(e) => setPickerResources((prev) => ({ ...prev, [row.id]: e.target.value }))} data-testid={`access-picker-resource-${accessTestIDToken(row.id)}`}>
                                        {rowResources.length === 0 && <option value="">No compatible scope targets</option>}
                                        {rowResources.map((resource) => <option key={accessResourceKey(resource)} value={accessResourceKey(resource)}>{accessResourceLabel(resource)}</option>)}
                                      </select>
                                      <button
                                        type="button"
                                        className="flex w-full min-w-36 flex-col rounded border border-border-base bg-bg-elevated px-2 py-1 text-left text-xs hover:border-accent disabled:opacity-50"
                                        disabled={rowResources.length === 0}
                                        onClick={() => setScopePickerRowID(row.id)}
                                        data-testid={`access-picker-scope-${accessTestIDToken(row.id)}`}
                                      >
                                        <span className="font-semibold text-text-primary">{resourceByKey.get(selectedResourceKey) ? accessResourceLabel(resourceByKey.get(selectedResourceKey)!) : 'Choose scope'}</span>
                                        <span className="text-text-muted">{row.scope}</span>
                                      </button>
                                    </td>
                                    <td className="px-3 py-2"><AccessRiskBadge risk={row.risk} /></td>
                                    <td className="px-3 py-2"><button type="button" className="rounded border border-border-base px-2 py-1 text-xs font-semibold" disabled={rowResources.length === 0} onClick={() => addPickerRows([row])} data-testid={`access-picker-add-${accessTestIDToken(row.id)}`}>Add</button></td>
                                  </tr>
                                );
                              })}
                            </Fragment>
                          );
                        })}
                        {filteredPickerRows.length === 0 && <tr><td colSpan={6} className="px-3 py-6 text-center text-sm text-text-muted">No permissions match the current filter.</td></tr>}
                      </tbody>
                    </table>
                  </div>
                </section>
                <section className="min-w-0 rounded border border-border-base bg-bg-base" data-testid="access-grant-list">
                  <div className="flex items-center justify-between border-b border-border-base px-3 py-2">
                    <h3 className="text-xs font-semibold uppercase text-text-muted">Grant list</h3>
                    <button type="button" className="rounded border border-danger/30 px-2 py-1 text-xs font-semibold text-danger disabled:opacity-50" disabled={selectedGrantIDs.length === 0} onClick={removeSelectedGrants} data-testid="access-remove-selected-grants">Delete selected</button>
                  </div>
                  {grantNotice && (
                    <p
                      className={[
                        'border-b px-3 py-2 text-xs',
                        grantNotice.tone === 'success' ? 'border-status-emerald-border bg-status-emerald-bg text-status-emerald-fg' : 'border-warning/30 bg-warning/10 text-warning',
                      ].join(' ')}
                      role="status"
                      data-testid="access-grant-notice"
                    >
                      {grantNotice.message}
                    </p>
                  )}
                  <div className="max-h-80 min-w-0 overflow-auto">
                    <table className="w-full min-w-[32rem] text-left text-sm">
                      <thead className="border-b border-border-base text-[0.6875rem] uppercase text-text-muted">
                        <tr>
                          <th className="w-10 px-3 py-2"><input type="checkbox" checked={grantEntries.length > 0 && grantEntries.every((entry) => selectedGrantIDs.includes(entry.id))} onChange={() => setSelectedGrantIDs(selectedGrantIDs.length === grantEntries.length ? [] : grantEntries.map((entry) => entry.id))} aria-label="Select all grant entries" /></th>
                          <th className="px-3 py-2 font-semibold">Resource</th>
                          <th className="px-3 py-2 font-semibold">Permission</th>
                          <th className="px-3 py-2 font-semibold">Scope</th>
                          <th className="px-3 py-2 font-semibold">Risk</th>
                        </tr>
                      </thead>
                      <tbody>
                        {grantEntries.map((entry) => (
                          <tr key={entry.id} className="border-b border-border-base last:border-0" data-testid={`access-grant-entry-${accessTestIDToken(entry.id)}`}>
                            <td className="px-3 py-2"><input type="checkbox" checked={selectedGrantIDs.includes(entry.id)} onChange={() => setSelectedGrantIDs((prev) => toggleValue(prev, entry.id))} data-testid="access-grant-entry-select" /></td>
                            <td className="px-3 py-2">{entry.kind === 'role' ? 'Role template' : entry.template?.resource}</td>
                            <td className="px-3 py-2"><div className="font-medium">{entry.kind === 'role' ? entry.roleName : directTemplateLabelForResource(entry.template!, entry.resource)}</div><div className="font-mono text-xs text-text-muted">{entry.roleId ?? entry.permissionKey}</div></td>
                            <td className="px-3 py-2">{accessResourceLabel(entry.resource)}</td>
                            <td className="px-3 py-2"><AccessRiskBadge risk={entry.risk} /></td>
                          </tr>
                        ))}
                        {grantEntries.length === 0 && <tr><td colSpan={5} className="px-3 py-6 text-center text-sm text-text-muted">Add permissions or role templates to build the batch grant.</td></tr>}
                      </tbody>
                    </table>
                  </div>
                </section>
              </div>
              <div className="grid gap-3 md:grid-cols-2">
                <label className="block">
                  <span className="text-xs font-semibold uppercase text-text-muted">Expires at</span>
                  <input
                    type="datetime-local"
                    className="mt-1 w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm text-text-primary"
                    value={request.expires_at?.slice(0, 16) ?? ''}
                    disabled={!canManageAccess}
                    onChange={(e) => setRequest((prev) => ({ ...prev, expires_at: e.target.value ? new Date(e.target.value).toISOString() : '' }))}
                    data-testid="access-batch-expires"
                  />
                </label>
                <label className="block">
                  <span className="text-xs font-semibold uppercase text-text-muted">Reason</span>
                  <input
                    className="mt-1 w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm text-text-primary"
                    value={request.reason}
                    disabled={!canManageAccess}
                    onChange={(e) => setRequest((prev) => ({ ...prev, reason: e.target.value }))}
                    data-testid="access-batch-reason"
                  />
                </label>
              </div>
              {previewMutation.isError && <p className="text-sm text-danger" role="alert">{(previewMutation.error as Error).message}</p>}
              {!canPreview && (
                <p className="rounded border border-border-base bg-bg-subtle px-3 py-2 text-xs text-text-secondary" data-testid="access-preview-disabled-reason">
                  {previewMissing}
                </p>
              )}
            </div>
          )}

          {step === 1 && preview && (
            <div className="space-y-4">
              <PreviewSummary preview={preview} />
              {preview.summary.high_risk > 0 && (
                <label className="flex gap-2 rounded border border-status-rose-border bg-status-rose-bg px-3 py-2 text-sm text-status-rose-fg">
                  <input type="checkbox" checked={highRiskAck} onChange={(e) => setHighRiskAck(e.target.checked)} data-testid="access-high-risk-ack" />
                  <span>Acknowledge high-risk grants</span>
                </label>
              )}
              <BatchItemsTable items={preview.items} />
            </div>
          )}

          {step === 2 && preview && (
            <div className="space-y-4">
              <PreviewSummary preview={preview} />
              <div className="rounded border border-border-base bg-bg-base p-4">
                <h3 className="text-sm font-semibold">Ready to apply</h3>
                <p className="mt-1 text-sm text-text-secondary">
                  {preview.summary.grantable} grantable items, {preview.summary.unauthorized} no-access items, {preview.summary.not_applicable} not-applicable items.
                </p>
              </div>
              {applyMutation.isError && <p className="text-sm text-danger" role="alert">{(applyMutation.error as Error).message}</p>}
            </div>
          )}

          {step === 3 && result && <ResultPanel result={result} title="Authorization result" />}
        </div>
        <div className="flex justify-between gap-2 border-t border-border-base px-5 py-4">
          <button
            type="button"
            className="rounded px-3 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle"
            onClick={step === 0 ? onClose : () => setStep(Math.max(0, step - 1))}
            disabled={previewMutation.isPending || applyMutation.isPending}
          >
            {step === 0 ? 'Cancel' : 'Back'}
          </button>
          <div className="flex gap-2">
            {step === 0 && (
              <button
                type="button"
                className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-medium text-btn-primary-fg hover:opacity-90 disabled:opacity-50"
                disabled={!canPreview || previewMutation.isPending}
                title={!canPreview ? previewMissing : undefined}
                onClick={runPreview}
                data-testid="access-run-preview"
              >
                Preview
              </button>
            )}
            {step === 1 && (
              <button
                type="button"
                className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-medium text-btn-primary-fg hover:opacity-90 disabled:opacity-50"
                disabled={!canConfirm}
                onClick={() => setStep(2)}
                data-testid="access-preview-continue"
              >
                Continue
              </button>
            )}
            {step === 2 && (
              <button
                type="button"
                className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-medium text-btn-primary-fg hover:opacity-90 disabled:opacity-50"
                disabled={!canManageAccess || applyMutation.isPending}
                onClick={runApply}
                data-testid="access-apply-batch"
              >
                Apply
              </button>
            )}
            {step === 3 && (
              <button
                type="button"
                className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-medium text-btn-primary-fg hover:opacity-90"
                onClick={onClose}
              >
                Done
              </button>
            )}
          </div>
        </div>
      </div>
      {scopePickerRow && (
        <ScopePickerModal
          row={scopePickerRow}
          resources={scopePickerResources}
          selectedKey={pickerResources[scopePickerRow.id] || (scopePickerResources[0] ? accessResourceKey(scopePickerResources[0]) : '')}
          onSelect={(resource) => {
            const key = accessResourceKey(resource);
            setCustomPickerResources((prev) => resourceByKey.has(key) ? prev : { ...prev, [key]: resource });
            setPickerResources((prev) => ({ ...prev, [scopePickerRow.id]: accessResourceKey(resource) }));
            setScopePickerRowID(null);
          }}
          onClose={() => setScopePickerRowID(null)}
        />
      )}
    </div>
  );
}

function ScopePickerModal({
  row,
  resources,
  selectedKey,
  onSelect,
  onClose,
}: {
  row: PermissionPickerRow;
  resources: AccessResourceScope[];
  selectedKey: string;
  onSelect: (resource: AccessResourceScope) => void;
  onClose: () => void;
}): React.ReactElement {
  const containerRef = useModalA11y({ open: true, onClose });
  const concreteKinds = useMemo(
    () => row.compatibleKinds.filter((kind): kind is AccessResourceKind => ['issue', 'plan', 'task'].includes(kind)),
    [row.compatibleKinds],
  );
  const projectResources = useMemo(() => resources.filter((resource) => resource.kind === 'project'), [resources]);
  const [specificKind, setSpecificKind] = useState<AccessResourceKind>(concreteKinds[0] ?? 'issue');
  const [specificProjectKey, setSpecificProjectKey] = useState(projectResources[0] ? accessResourceKey(projectResources[0]) : '');
  const [specificID, setSpecificID] = useState('');
  useEffect(() => {
    if (concreteKinds.length > 0 && !concreteKinds.includes(specificKind)) setSpecificKind(concreteKinds[0]);
  }, [concreteKinds, specificKind]);
  useEffect(() => {
    if (!specificProjectKey && projectResources[0]) setSpecificProjectKey(accessResourceKey(projectResources[0]));
  }, [projectResources, specificProjectKey]);
  const groups = useMemo(() => {
    const byKind = new Map<string, AccessResourceScope[]>();
    for (const resource of resources) {
      const group = byKind.get(resource.kind) ?? [];
      group.push(resource);
      byKind.set(resource.kind, group);
    }
    return [...byKind.entries()]
      .sort((a, b) => compareAccessResourceKind(a[0], b[0]))
      .map(([kind, entries]) => ({
        kind,
        entries: entries.sort((a, b) => accessResourceLabel(a).localeCompare(accessResourceLabel(b))),
      }));
  }, [resources]);
  const selectedProject = projectResources.find((resource) => accessResourceKey(resource) === specificProjectKey) ?? projectResources[0];
  const canAddSpecific = concreteKinds.length > 0 && specificID.trim().length > 0 && !!selectedProject;
  const addSpecificScope = (): void => {
    if (!canAddSpecific || !selectedProject) return;
    const id = specificID.trim();
    onSelect({
      kind: specificKind,
      id,
      org_id: selectedProject.org_id,
      project_id: selectedProject.id,
      label: `${accessResourceKindLabel(specificKind)} ${id}`,
    });
  };

  return (
    <div className="fixed inset-0 z-[60] flex items-start justify-center bg-black/30 px-4 py-12" data-testid="access-scope-picker-backdrop">
      <div
        ref={containerRef}
        role="dialog"
        aria-modal="true"
        aria-label="Choose scope"
        className="flex max-h-[calc(100vh-6rem)] w-full max-w-2xl flex-col rounded border border-border-base bg-bg-elevated text-text-primary shadow-2xl"
        data-testid="access-scope-picker"
      >
        <div className="flex items-start justify-between gap-3 border-b border-border-base px-4 py-3">
          <div>
            <h3 className="text-sm font-semibold">Choose scope</h3>
            <p className="mt-1 text-xs text-text-muted">{row.label} · {row.scope}</p>
          </div>
          <button type="button" aria-label="Close scope picker" title="Close" className="rounded p-1.5 text-text-secondary hover:bg-bg-subtle" onClick={onClose}>
            <IconClose />
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-4">
          {concreteKinds.length > 0 && (
            <section className="mb-3 rounded border border-border-base bg-bg-base p-3" data-testid="access-scope-picker-specific">
              <div className="text-xs font-semibold uppercase text-text-secondary">Specific {accessResourceKindLabel(concreteKinds[0]).toLowerCase()}</div>
              <div className="mt-2 grid gap-2 md:grid-cols-[9rem_minmax(0,1fr)_minmax(0,1fr)_auto]">
                <label>
                  <span className="text-[0.6875rem] font-semibold uppercase text-text-muted">Type</span>
                  <select
                    className="mt-1 w-full rounded border border-border-base bg-bg-elevated px-2 py-1.5 text-sm"
                    value={specificKind}
                    onChange={(event) => setSpecificKind(event.target.value as AccessResourceKind)}
                    data-testid="access-scope-picker-specific-kind"
                  >
                    {concreteKinds.map((kind) => <option key={kind} value={kind}>{accessResourceKindLabel(kind)}</option>)}
                  </select>
                </label>
                <label>
                  <span className="text-[0.6875rem] font-semibold uppercase text-text-muted">Project</span>
                  <select
                    className="mt-1 w-full rounded border border-border-base bg-bg-elevated px-2 py-1.5 text-sm"
                    value={specificProjectKey}
                    onChange={(event) => setSpecificProjectKey(event.target.value)}
                    data-testid="access-scope-picker-specific-project"
                  >
                    {projectResources.map((resource) => <option key={accessResourceKey(resource)} value={accessResourceKey(resource)}>{accessResourceLabel(resource)}</option>)}
                  </select>
                </label>
                <label>
                  <span className="text-[0.6875rem] font-semibold uppercase text-text-muted">{accessResourceKindLabel(specificKind)} ID</span>
                  <input
                    className="mt-1 w-full rounded border border-border-base bg-bg-elevated px-2 py-1.5 text-sm"
                    value={specificID}
                    onChange={(event) => setSpecificID(event.target.value)}
                    placeholder={`${specificKind}-id`}
                    data-testid="access-scope-picker-specific-id"
                  />
                </label>
                <button
                  type="button"
                  className="self-end rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-medium text-btn-primary-fg disabled:opacity-50"
                  disabled={!canAddSpecific}
                  onClick={addSpecificScope}
                  data-testid="access-scope-picker-specific-add"
                >
                  Use
                </button>
              </div>
            </section>
          )}
          {groups.length === 0 && <p className="rounded border border-border-base bg-bg-subtle px-3 py-6 text-center text-sm text-text-muted">No compatible scopes.</p>}
          <div className="space-y-3">
            {groups.map((group) => (
              <section key={group.kind} className="rounded border border-border-base" data-testid={`access-scope-picker-group-${accessTestIDToken(group.kind)}`}>
                <div className="border-b border-border-base bg-bg-subtle px-3 py-2 text-xs font-semibold uppercase text-text-secondary">
                  {accessResourceKindLabel(group.kind)}
                </div>
                <div className="divide-y divide-border-base">
                  {group.entries.map((resource) => {
                    const key = accessResourceKey(resource);
                    return (
                      <button
                        key={key}
                        type="button"
                        className={[
                          'flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm hover:bg-bg-subtle',
                          key === selectedKey ? 'bg-brand/10 ring-1 ring-inset ring-brand/30' : '',
                        ].join(' ')}
                        onClick={() => onSelect(resource)}
                        data-testid={`access-scope-picker-option-${accessTestIDToken(key)}`}
                      >
                        <span>
                          <span className="block font-medium text-text-primary">{accessResourceLabel(resource)}</span>
                          <span className="block text-xs text-text-muted">{accessResourceMetaLabel(resource)}</span>
                        </span>
                        {key === selectedKey && <span className="text-xs font-semibold text-brand">Selected</span>}
                      </button>
                    );
                  })}
                </div>
              </section>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function toggleValue(values: string[], value: string): string[] {
  return values.includes(value) ? values.filter((v) => v !== value) : [...values, value];
}

function previewDisabledReason(canManageAccess: boolean, request: AccessBatchRequest, grantEntryCount = (request.entries?.length ?? 0)): string {
  if (!canManageAccess) return 'Preview unavailable: current subject cannot manage access.';
  if (request.subject_refs.length === 0) return 'Select at least one subject.';
  if (grantEntryCount === 0 && (request.role_ids?.length ?? 0) === 0 && request.permission_keys.length === 0) return 'Add at least one grant entry.';
  if (grantEntryCount === 0 && request.resources.length === 0) return 'Select at least one scope parameter.';
  if (request.reason.trim().length === 0) return 'Enter a reason before previewing.';
  return 'Ready to preview.';
}

function roleIDFromEvidence(evidenceRef: string): string {
  const parts = evidenceRef.split('/');
  return parts.length >= 3 ? parts[2] : '';
}

function PreviewSummary({ preview }: { preview: AccessBatchPreview }): React.ReactElement {
  return (
    <div className="space-y-2" data-testid="access-preview-summary">
      <div className="grid gap-2 md:grid-cols-5">
        <SummaryTile label="Total" value={preview.summary.total} tone="muted" />
        <SummaryTile label="Grantable" value={preview.summary.grantable} tone="success" />
        <SummaryTile label="High risk" value={preview.summary.high_risk} tone="danger" />
        <SummaryTile label="No access" value={preview.summary.unauthorized} tone="warning" />
        <SummaryTile label="N/A" value={preview.summary.not_applicable} tone="muted" />
      </div>
      <div className="rounded border border-border-base bg-bg-base px-3 py-2 text-sm text-text-secondary">
        <span className="font-semibold text-text-primary">Expires</span>
        <span className="ml-2">{displayAccessDate(preview.expires_at)}</span>
      </div>
    </div>
  );
}

function BatchItemsTable({ items }: { items: AccessBatchItem[] }): React.ReactElement {
  return (
    <div className="overflow-x-auto rounded border border-border-base">
      <table className="w-full min-w-[42rem] text-left text-sm" data-testid="access-batch-items">
        <thead className="border-b border-border-base text-[0.6875rem] uppercase text-text-muted">
          <tr>
            <th className="px-3 py-2 font-semibold">Subject</th>
            <th className="px-3 py-2 font-semibold">Permission</th>
            <th className="px-3 py-2 font-semibold">Resource</th>
            <th className="px-3 py-2 font-semibold">Status</th>
            <th className="px-3 py-2 font-semibold">Code</th>
            <th className="px-3 py-2 font-semibold">Reason</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.id} className="border-b border-border-base last:border-0">
              <td className="px-3 py-2">{item.subject_name}</td>
              <td className="px-3 py-2">
                {item.role_id ? (
                  <span>
                    <span className="block text-xs font-semibold text-text-primary">{item.role_name || item.role_id}</span>
                    <span className="block font-mono text-xs text-text-muted">{item.role_id}</span>
                  </span>
                ) : (
                  <span>
                    <span className="block text-xs font-semibold text-text-primary">{semanticPermission(item.permission, item.template, item.resource.kind, undefined).label}</span>
                    <span className="block font-mono text-[0.6875rem] text-text-muted">{item.permission}</span>
                  </span>
                )}
              </td>
              <td className="px-3 py-2">{accessResourceLabel(item.resource)}</td>
              <td className="px-3 py-2"><AccessStatusBadge status={item.status} /></td>
              <td className="px-3 py-2 font-mono text-xs" data-testid="access-batch-item-code">{item.code || (item.status === 'denied' ? '403 denied' : '—')}</td>
              <td className="px-3 py-2 text-xs text-text-secondary">{item.reason}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ResultPanel({ result, title }: { result: AccessBatchResult | AccessBatchPreview; title: string }): React.ReactElement {
  const failed = 'failed' in result.summary ? result.summary.failed : result.summary.total - result.summary.grantable;
  const succeeded = 'succeeded' in result.summary ? result.summary.succeeded : result.summary.grantable;
  const partialFailure = 'partial_failure' in result.summary ? result.summary.partial_failure : failed > 0;
  return (
    <div className="space-y-3" data-testid="access-result">
      <div
        className={[
          'rounded border px-3 py-2 text-sm',
          partialFailure
            ? 'border-status-amber-border bg-status-amber-bg text-status-amber-fg'
            : 'border-status-emerald-border bg-status-emerald-bg text-status-emerald-fg',
        ].join(' ')}
        role="status"
      >
        <p className="font-semibold">{partialFailure ? 'Partial failure' : title}</p>
        <p className="mt-1">
          {succeeded} succeeded, {failed} failed, {result.summary.unauthorized} no access, {result.summary.not_applicable} not applicable.
        </p>
      </div>
      <BatchItemsTable items={result.items} />
    </div>
  );
}

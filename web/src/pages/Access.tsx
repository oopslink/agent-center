import type React from 'react';
import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ApiError } from '@/api/client';
import {
  type AccessBatchItem,
  type AccessBatchPreview,
  type AccessBatchRequest,
  type AccessBatchResult,
  type AccessDecision,
  type AccessGrant,
  type RAMRole,
  type AccessPermissionDefinition,
  type AccessResourceKind,
  type AccessResourceScope,
  type AccessRisk,
  type AccessRole,
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
  useRAMRoleRevoke,
  useRAMRoleUpdate,
  useRAMRoles,
  useAccessRevokePreview,
  useAccessRoleUpdate,
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
import { EntityMultiSelect } from '@/components/EntityMultiSelect';
import { EmptyState } from '@/components/EmptyState';
import { ConfirmModal } from '@/components/ConfirmModal';
import { Skeleton } from '@/components/Skeleton';
import { useModalA11y } from '@/components/useModalA11y';
import { useAppStore } from '@/store/app';
import { useOptionalOrgContext } from '@/OrgContext';
import {
  useAllTeamMembers,
  useAllTeamRoleRAMMappings,
  usePreviewTeamRoleRAMMapping,
  useReplaceTeamRoleRAMMapping,
  useTeams,
  type TeamRAMRoleMapping,
  type TeamView,
} from '@/api/teams';
import {
  AccessMetaPill,
  AccessRiskBadge,
  AccessStatusBadge,
  accessResourceKey,
  accessResourceLabel,
  accessRiskLabel,
  displayAccessDate,
} from '@/components/access/kit';

type AccessView = 'roles' | 'subjects';
type AccessToast = { tone: 'success' | 'danger' | 'warning'; message: string } | null;

const STATUS_OPTIONS: Array<AccessStatus | 'all'> = ['all', 'allowed', 'denied', 'unauthorized', 'not_applicable'];
const RISK_OPTIONS: Array<AccessRisk | 'all'> = ['all', 'high', 'medium', 'low'];
const SUBJECT_OPTIONS: Array<AccessSubjectKind | 'all'> = ['all', 'human', 'agent', 'worker', 'system'];
const RESOURCE_OPTIONS: Array<AccessResourceKind | 'all'> = [
  'all',
  'org',
  'project',
  'team',
  'task',
  'issue',
  'plan',
  'conversation',
  'file',
  'agent',
  'worker',
  'admin_token',
];

function emptyBatchRequest(resources: AccessResourceScope[]): AccessBatchRequest {
  return {
    subject_refs: [],
    permission_keys: [],
    resources: resources.slice(0, 1),
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

export default function Access(): React.ReactElement {
  const org = useOptionalOrgContext();
  const subjectRef = useAppStore((s) => s.currentUserId);
  const orgResource = useMemo<ResourceScope>(() => ({ kind: 'org', id: org?.orgId ?? 'org-test' }), [org?.orgId]);
  const currentPermissions = useCurrentSubjectEffectivePermissions(orgResource);
  const canManageAccess = hasEffectivePermission(currentPermissions.data, 'org.member.role.manage');
  const explainAccess = usePermissionExplain(
    subjectRef,
    'org.member.role.manage',
    orgResource,
    currentPermissions.isSuccess && !canManageAccess,
  );
  const [searchParams, setSearchParams] = useSearchParams();
  const view = parseAccessView(searchParams.get('view'));
  const [query, setQuery] = useState('');
  const [subjectKind, setSubjectKind] = useState<AccessSubjectKind | 'all'>('all');
  const [resourceKind, setResourceKind] = useState<AccessResourceKind | 'all'>('all');
  const [risk, setRisk] = useState<AccessRisk | 'all'>('all');
  const [status, setStatus] = useState<AccessStatus | 'all'>('all');
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerMode, setDrawerMode] = useState<'direct' | 'batch'>('direct');
  const [selectedSubjectRef, setSelectedSubjectRef] = useState('');
  const [toast, setToast] = useState<AccessToast>(null);
  const setView = (next: AccessView): void => {
    setSearchParams((prev) => {
      const params = new URLSearchParams(prev);
      params.set('view', next === 'subjects' ? 'subject-access' : 'roles-and-mappings');
      return params;
    }, { replace: true });
  };

  const overview = useAccessOverview({
    q: query,
    subject_kind: subjectKind,
    resource_kind: resourceKind,
    risk,
    status,
  }, currentPermissions.isSuccess && canManageAccess);
  const data = overview.data;
  const ramRoles = useRAMRoles();
  const teams = useTeams();
  const mappingEntries = useAllTeamRoleRAMMappings(teams.data ?? []);
  const memberEntries = useAllTeamMembers(teams.data ?? []);
  const resources = useMemo(
    () => uniqueResources(data?.decisions ?? [], data?.grants ?? []),
    [data?.decisions, data?.grants],
  );

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

  return (
    <section className="space-y-4" data-testid="page-Access">
      {toast && <AccessToastNotice toast={toast} onDismiss={() => setToast(null)} />}
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">Access</h1>
        </div>
        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-medium text-btn-primary-fg hover:opacity-90"
            onClick={() => { setDrawerMode('direct'); setDrawerOpen(true); }}
            disabled={!canManageAccess}
            title={!canManageAccess ? 'Requires org.member.role.manage' : undefined}
            data-testid="access-open-direct-binding"
          >
            Add direct binding
          </button>
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm font-medium text-text-primary hover:bg-bg-subtle disabled:opacity-50"
            onClick={() => { setDrawerMode('batch'); setDrawerOpen(true); }}
            disabled={!canManageAccess}
            title={!canManageAccess ? 'Requires org.member.role.manage' : undefined}
            data-testid="access-open-batch"
          >
            Batch grant
          </button>
        </div>
      </header>

      {currentPermissions.isLoading && <Skeleton height="10rem" />}
      {currentPermissions.isError && (
        <AccessForbidden
          reason={(currentPermissions.error as Error).message}
          status={currentPermissions.error instanceof ApiError ? currentPermissions.error.status : 403}
        />
      )}
      {currentPermissions.isSuccess && !canManageAccess && (
        <AccessForbidden
          reason={explainAccess.data?.decision.reason ?? (explainAccess.error as Error | undefined)?.message ?? 'Current subject lacks org.member.role.manage'}
          status={403}
        />
      )}

      {currentPermissions.isSuccess && canManageAccess && (
      <>
      <div className="grid gap-3 md:grid-cols-5">
        <SummaryTile label="Allowed" value={data?.summary.allowed ?? 0} tone="success" />
        <SummaryTile label="High risk" value={data?.summary.high_risk ?? 0} tone="danger" />
        <SummaryTile label="Expiring" value={data?.summary.expiring ?? 0} tone="warning" />
        <SummaryTile label="No access" value={data?.summary.denied ?? 0} tone="warning" />
        <SummaryTile label="Not applicable" value={data?.summary.not_applicable ?? 0} tone="muted" />
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <div className="inline-flex rounded border border-border-base bg-bg-elevated p-0.5" role="tablist" aria-label="Access view">
          <button
            type="button"
            role="tab"
            aria-selected={view === 'roles'}
            className={segmentedClass(view === 'roles')}
            onClick={() => setView('roles')}
            data-testid="access-view-roles"
          >
            Roles & mappings
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={view === 'subjects'}
            className={segmentedClass(view === 'subjects')}
            onClick={() => setView('subjects')}
            data-testid="access-view-subjects"
          >
            Subject access
          </button>
        </div>
        <label className="relative min-w-[14rem] flex-1 md:max-w-xs">
          <span className="sr-only">Search access</span>
          <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-muted">
            <IconSearch />
          </span>
          <input
            className="w-full rounded border border-border-base bg-bg-elevated py-2 pl-9 pr-3 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
            placeholder="Search subject, permission, reason"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            data-testid="access-search"
          />
        </label>
        <Select label="Resource" value={resourceKind} onChange={(v) => setResourceKind(v as AccessResourceKind | 'all')} options={RESOURCE_OPTIONS} />
        <Select label="Subject" value={subjectKind} onChange={(v) => setSubjectKind(v as AccessSubjectKind | 'all')} options={SUBJECT_OPTIONS} />
        <Select label="Risk" value={risk} onChange={(v) => setRisk(v as AccessRisk | 'all')} options={RISK_OPTIONS} />
        <Select label="Status" value={status} onChange={(v) => setStatus(v as AccessStatus | 'all')} options={STATUS_OPTIONS} />
      </div>

      {overview.isLoading && <Skeleton height="18rem" />}
      {overview.isError && (
        <p className="rounded border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
          {(overview.error as Error).message}
        </p>
      )}

      {!overview.isLoading && !overview.isError && data && (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]">
          <div className="space-y-4">
            {view === 'roles' ? (
              <>
                <RAMRolesView
                  catalog={data.catalog}
                  mappingEntries={mappingEntries}
                  canManageAccess={canManageAccess}
                />
                <TeamRoleMappingsView
                  roles={ramRoles.data?.roles ?? []}
                  teams={teams.data ?? []}
                  mappingEntries={mappingEntries}
                  canManageAccess={canManageAccess}
                />
              </>
            ) : (
              <SubjectDecisionView
                decisions={data.decisions}
                subjectByRef={subjectByRef}
                permissionByKey={permissionByKey}
                memberEntries={memberEntries}
                mappingEntries={mappingEntries}
                onSelectSubject={setSelectedSubjectRef}
              />
            )}
            <PermissionCatalog catalog={data.catalog} />
          </div>
          <aside className="space-y-4">
            {view === 'subjects' && (
              <SubjectAccessSidebar
                decisions={data.decisions}
                grants={data.grants}
                subjects={data.subjects}
                selectedSubjectRef={selectedSubjectRef}
                permissionByKey={permissionByKey}
              />
            )}
            {view === 'subjects' && <RoleManagement roles={data.roles} catalog={data.catalog} canManageAccess={canManageAccess} />}
            <GrantRevoke grants={data.grants} canManageAccess={canManageAccess} onToast={setToast} />
          </aside>
        </div>
      )}

      {drawerOpen && data && (
        <BatchGrantDrawer
          subjects={data.subjects}
          permissions={data.catalog}
          resources={resources}
          canManageAccess={canManageAccess}
          mode={drawerMode}
          onToast={setToast}
          onClose={() => setDrawerOpen(false)}
        />
      )}
      </>
      )}
    </section>
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

function parseAccessView(value: string | null): AccessView {
  if (value === 'subject-access') return 'subjects';
  return 'roles';
}

function AccessForbidden({ reason, status }: { reason: string; status: number }): React.ReactElement {
  return (
    <section className="rounded border border-danger/30 bg-danger/10 p-4" data-testid="access-forbidden" role="alert">
      <h2 className="text-sm font-semibold text-danger">Access unavailable ({status})</h2>
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

function segmentedClass(active: boolean): string {
  return [
    'rounded px-3 py-1.5 text-sm font-medium',
    active ? 'bg-brand text-white' : 'text-text-secondary hover:bg-bg-subtle',
  ].join(' ');
}

function Select({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: readonly string[];
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
        {options.map((option) => (
          <option key={option} value={option}>
            {option === 'all' ? `All ${label.toLowerCase()}` : option}
          </option>
        ))}
      </select>
    </label>
  );
}

function RAMRolesView({
  catalog,
  mappingEntries,
  canManageAccess,
}: {
  catalog: AccessPermissionDefinition[];
  mappingEntries: MappingEntry[];
  canManageAccess: boolean;
}): React.ReactElement {
  const roles = useRAMRoles();
  const create = useRAMRoleCreate();
  const update = useRAMRoleUpdate();
  const deleteRole = useRAMRoleDelete();
  const revokeRole = useRAMRoleRevoke();
  const replaceMapping = useReplaceTeamRoleRAMMapping();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [roleSearch, setRoleSearch] = useState('');
  const [draftName, setDraftName] = useState('');
  const [draftStableKey, setDraftStableKey] = useState('');
  const [draftDescription, setDraftDescription] = useState('');
  const [draftScope, setDraftScope] = useState('team');
  const [editName, setEditName] = useState('');
  const [editDescription, setEditDescription] = useState('');
  const [editScope, setEditScope] = useState('team');
  const [createPermissions, setCreatePermissions] = useState<string[]>([]);
  const [draftPermissions, setDraftPermissions] = useState<string[]>([]);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [createdRoleId, setCreatedRoleId] = useState<string | null>(null);
  const [deleteNameConfirm, setDeleteNameConfirm] = useState('');
  const [showReferences, setShowReferences] = useState(false);
  const [migrationTarget, setMigrationTarget] = useState('');
  const [status, setStatus] = useState<string | null>(null);
  const selected = selectedId ?? roles.data?.roles[0]?.id ?? null;
  const detail = useRAMRole(selected);
  const latest = detail.data?.latest;
  const versionPermissions = draftPermissions.length > 0 ? draftPermissions : latest?.permissions ?? [];
  const mappedByRAMRole = useRAMRoleReferences(mappingEntries);
  const mappedReferences = selected ? mappedByRAMRole.get(selected) ?? [] : [];
  const filteredRoles = useMemo(() => {
    const q = roleSearch.trim().toLowerCase();
    const all = roles.data?.roles ?? [];
    if (!q) return all;
    return all.filter((role) => [role.name, role.stable_key, role.id, role.description, role.scope].some((v) => (v ?? '').toLowerCase().includes(q)));
  }, [roleSearch, roles.data?.roles]);

  const toggleDraftPermission = (permission: string): void => {
    setDraftPermissions((prev) => toggleValue(prev, permission).sort());
  };
  const toggleCreatePermission = (permission: string): void => {
    setCreatePermissions((prev) => toggleValue(prev, permission).sort());
  };
  const resetDraft = (role?: RAMRole): void => {
    setDraftName('');
    setDraftStableKey('');
    setDraftDescription('');
    setDraftScope(role?.scope ?? 'team');
    setDraftPermissions(role?.permissions ?? []);
    setDeleteNameConfirm('');
    setShowReferences(false);
    setMigrationTarget('');
  };
  useEffect(() => {
    if (!detail.data) return;
    setEditName(detail.data.name);
    setEditDescription(detail.data.description);
    setEditScope(detail.data.scope || 'team');
    setDraftPermissions(detail.data.latest.permissions ?? []);
  }, [detail.data?.id, detail.data?.latest.version]);
  const selectedReferences = detail.data?.references ?? [];
  const selectedIsReferenced = selectedReferences.length > 0 || mappedReferences.length > 0;
  const selectedIsCustom = detail.data?.kind === 'custom';
  const selectedWasCreatedHere = selected === createdRoleId;
  const runReferenceMigration = async (): Promise<void> => {
    if (!selected || !migrationTarget || migrationTarget === selected) return;
    const pending = mappedReferences
      .map((ref) => {
        const mapping = mappingEntries.find((entry) => entry.team.id === ref.team.id && entry.role === ref.role)?.query.data;
        if (!mapping) return null;
        const next = Array.from(new Set(mapping.ram_role_ids.map((id) => (id === selected ? migrationTarget : id))));
        return { mapping, next };
      })
      .filter((item): item is { mapping: TeamRAMRoleMapping; next: string[] } => item !== null);
    for (const item of pending) {
      await replaceMapping.mutateAsync({
        team_id: item.mapping.team_id,
        role: item.mapping.team_role,
        ram_role_ids: item.next,
        expected_version: item.mapping.version,
      });
    }
    setStatus(`Migrated ${pending.length} Team Role references.`);
  };

  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]" data-testid="access-roles-view">
      <section className="rounded border border-border-base bg-bg-elevated">
        <div className="flex items-center justify-between gap-2 border-b border-border-base px-4 py-3">
          <h2 className="text-sm font-semibold text-text-primary">RAM Roles</h2>
          <div className="flex items-center gap-2">
            <AccessMetaPill>{roles.data?.roles.length ?? 0} current versions</AccessMetaPill>
            <button type="button" data-testid="access-role-new" className="rounded bg-btn-primary-bg px-3 py-1.5 text-xs font-semibold text-btn-primary-fg" onClick={() => document.querySelector<HTMLInputElement>('[data-testid="access-role-name"]')?.focus()}>New RAM Role</button>
          </div>
        </div>
        {status && <div className="border-b border-success/30 bg-success/10 px-4 py-2 text-sm text-success" role="status" data-testid="access-role-success">{status}</div>}
        <div className="border-b border-border-base px-4 py-3">
          <label className="relative block">
            <span className="sr-only">Search RAM Roles</span>
            <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-muted">
              <IconSearch />
            </span>
            <input
              className="w-full rounded border border-border-base bg-bg-base py-2 pl-9 pr-3 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent focus-visible:outline-none"
              placeholder="Search name, stable key, scope"
              value={roleSearch}
              onChange={(e) => setRoleSearch(e.target.value)}
              data-testid="access-role-search"
            />
          </label>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[42rem] text-left text-sm">
            <thead className="border-b border-border-base text-[0.6875rem] uppercase text-text-muted">
              <tr>
                <th className="px-4 py-2 font-semibold">Role</th>
                <th className="px-4 py-2 font-semibold">Scope</th>
                <th className="px-4 py-2 font-semibold">Version</th>
                <th className="px-4 py-2 font-semibold">Risk</th>
                <th className="px-4 py-2 font-semibold">Permissions</th>
              </tr>
            </thead>
            <tbody>
              {filteredRoles.map((role) => (
                <tr
                  key={role.id}
                  className={['cursor-pointer border-b border-border-base last:border-0', selected === role.id ? 'bg-brand/5' : 'hover:bg-bg-subtle'].join(' ')}
                  onClick={() => {
                    setSelectedId(role.id);
                    resetDraft(role);
                  }}
                  data-testid={`access-role-row-${role.id}`}
                >
                  <td className="px-4 py-3">
                    <div className="font-semibold text-text-primary">{role.name}</div>
                    <div className="text-xs text-text-muted">{role.description}</div>
                    <div className="font-mono text-[0.6875rem] text-text-muted">{role.stable_key || role.id}</div>
                    <div className="mt-1 text-[0.6875rem] text-text-muted">
                      Referenced by {role.references ?? (mappedByRAMRole.get(role.id) ?? []).length} Team Roles
                    </div>
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-text-secondary">{role.scope}</td>
                  <td className="px-4 py-3 font-mono text-xs text-text-secondary">v{role.version}</td>
                  <td className="px-4 py-3"><AccessRiskBadge risk={role.risk} /></td>
                  <td className="px-4 py-3 font-mono text-xs text-text-secondary">{role.permissions.join(', ')}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <aside className="space-y-4">
        <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="access-role-create">
          <h2 className="text-sm font-semibold text-text-primary">Create role</h2>
          <RoleTextField label="Name" value={draftName} onChange={setDraftName} testId="access-role-name" />
          <RoleTextField label="Stable key" value={draftStableKey} onChange={setDraftStableKey} testId="access-role-stable-key" />
          <RoleTextField label="Description" value={draftDescription} onChange={setDraftDescription} testId="access-role-description" />
          <RoleTextField label="Scope" value={draftScope} onChange={setDraftScope} testId="access-role-scope" />
          <PermissionChecklist catalog={catalog} selected={createPermissions} onToggle={toggleCreatePermission} />
          <button
            type="button"
            className="mt-3 rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-semibold text-btn-primary-fg disabled:opacity-50"
            disabled={!canManageAccess || !draftName.trim() || createPermissions.length === 0 || create.isPending}
            data-testid="access-role-create-submit"
            onClick={() => create.mutate({
              name: draftName,
              stable_key: draftStableKey,
              description: draftDescription,
              scope: draftScope,
              permissions: createPermissions,
            }, {
              onSuccess: (created) => {
                setSelectedId(created.id);
                setCreatedRoleId(created.id);
                setCreatePermissions([]);
                resetDraft(created.latest);
              },
            })}
          >
            Create
          </button>
          {create.isError && <p className="mt-2 text-xs text-danger" role="alert">{(create.error as Error).message}</p>}
        </section>

        <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="access-role-detail">
          <h2 className="text-sm font-semibold text-text-primary">Version history</h2>
          {detail.isLoading && <Skeleton height="8rem" />}
          {detail.data && (
            <>
              <div className="mt-2 rounded border border-border-base bg-bg-subtle p-2">
                <div className="font-semibold text-text-primary">{detail.data.name}</div>
                <div className="text-xs text-text-muted">Latest v{detail.data.latest.version} · {detail.data.scope} · {detail.data.stable_key}</div>
                <div className="mt-1 text-xs text-text-secondary" data-testid="access-role-used-by">
                  Referenced by:{' '}
                  {mappedReferences.length > 0
                    ? mappedReferences.map((ref) => `${ref.team.name} / ${ref.role}`).join(', ')
                    : selectedReferences.length > 0
                      ? selectedReferences.map((ref) => `${ref.team_name} / ${ref.team_role}`).join(', ')
                      : 'None'}
                </div>
                {selectedIsReferenced && (
                  <div className="mt-2 space-y-2" data-testid="access-role-delete-blocked">
                    <p className="text-xs text-danger">This RAM Role cannot be deleted until references are migrated.</p>
                    <button type="button" data-testid="access-role-view-references" className="rounded border border-border-base px-2 py-1 text-xs" onClick={() => setShowReferences((value) => !value)}>
                      View references
                    </button>
                    {showReferences && <div className="space-y-2" data-testid="access-role-references">
                      {mappedReferences.map((ref) => <p key={`${ref.team.id}:${ref.role}`} className="text-xs">{ref.team.name} / {ref.role}</p>)}
                      <select data-testid="access-role-migrate-target" className="w-full rounded border border-border-base bg-bg-elevated px-2 py-1.5 text-sm" value={migrationTarget} onChange={(event) => setMigrationTarget(event.target.value)}>
                        <option value="">Select RAM Role</option>
                        {(roles.data?.roles ?? []).filter((role) => role.id !== selected).map((role) => <option key={role.id} value={role.id}>{role.name}</option>)}
                      </select>
                      <button type="button" data-testid="access-role-migrate-references" className="rounded bg-btn-primary-bg px-2 py-1 text-xs font-semibold text-btn-primary-fg" disabled={!migrationTarget || replaceMapping.isPending} onClick={() => void runReferenceMigration()}>Migrate references</button>
                    </div>}
                  </div>
                )}
              </div>
              <div className="mt-3 space-y-2" data-testid="access-role-versions">
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
              <div className="mt-3 border-t border-border-base pt-3">
                <h3 className="text-xs font-semibold uppercase text-text-muted">Edit RAM Role</h3>
                <RoleTextField label="Name" value={editName} onChange={setEditName} testId="access-role-edit-name" />
                <RoleTextField label="Description" value={editDescription} onChange={setEditDescription} testId="access-role-edit-description" />
                <RoleTextField label="Scope" value={editScope} onChange={setEditScope} testId="access-role-edit-scope" />
                <PermissionChecklist catalog={catalog} selected={versionPermissions} onToggle={toggleDraftPermission} />
                <button
                  type="button"
                  className="mt-3 rounded border border-border-base px-3 py-1.5 text-sm font-semibold text-text-primary hover:bg-bg-subtle disabled:opacity-50"
                  disabled={!canManageAccess || !latest || !selected || !selectedIsCustom || versionPermissions.length === 0 || update.isPending}
                  data-testid="access-role-new-version-submit"
                  onClick={() => {
                    if (!latest || !selected) return;
                    update.mutate({
                      id: selected,
                      payload: {
                        name: editName,
                        description: editDescription,
                        scope: editScope,
                        permissions: versionPermissions,
                        expected_latest_version: latest.version,
                      },
                    }, { onSuccess: (updated) => resetDraft(updated.latest) });
                  }}
                >
                  Save changes
                </button>
                <button
                  type="button"
                  className="ml-2 rounded border border-danger/40 px-3 py-1.5 text-sm font-semibold text-danger hover:bg-danger/10 disabled:opacity-50"
                  disabled={!canManageAccess || !selected || !latest || !selectedIsCustom || selectedIsReferenced || (!selectedWasCreatedHere && deleteNameConfirm !== detail.data.name) || deleteRole.isPending || revokeRole.isPending}
                  data-testid="access-role-disable-submit"
                  onClick={() => {
                    if (!selected || !latest) return;
                    if (selectedWasCreatedHere) {
                      setDeleteConfirmOpen(true);
                      return;
                    }
                    revokeRole.mutate({ id: selected, expected_latest_version: latest.version, reason: 'RAM role deleted after reference-impact review' }, { onSuccess: () => { setStatus(`Deleted RAM Role ${detail.data.name}.`); setSelectedId(null); } });
                  }}
                >
                  Delete
                </button>
                {selectedIsReferenced && (
                  <p className="mt-2 text-xs text-danger">Delete is blocked while Team Role references exist. Use View references or Migrate references first.</p>
                )}
                {!selectedIsReferenced && !selectedWasCreatedHere && (
                  <RoleTextField label={`Type ${detail.data.name} to delete`} value={deleteNameConfirm} onChange={setDeleteNameConfirm} testId="access-role-delete-name" />
                )}
              </div>
            </>
          )}
          {update.isError && <p className="mt-2 text-xs text-danger" role="alert">{(update.error as Error).message}</p>}
          {deleteRole.isError && <p className="mt-2 text-xs text-danger" role="alert">{(deleteRole.error as Error).message}</p>}
          {revokeRole.isError && <p className="mt-2 text-xs text-danger" role="alert">{(revokeRole.error as Error).message}</p>}
          {replaceMapping.isError && <p className="mt-2 text-xs text-danger" role="alert">{(replaceMapping.error as Error).message}</p>}
        </section>
      </aside>
      <ConfirmModal
        open={deleteConfirmOpen}
        title="Delete RAM Role"
        message={
          <div className="space-y-2">
            {selectedIsReferenced ? (
              <p>This RAM Role is referenced by Team Roles. Remove or migrate references before deleting.</p>
            ) : (
              <>
                <p>This unreferenced RAM Role will be revoked after this second confirmation.</p>
                <p className="font-mono text-xs">{detail.data?.stable_key}</p>
              </>
            )}
          </div>
        }
        confirmLabel={selectedIsReferenced ? 'Blocked' : 'Delete'}
        danger
        busy={deleteRole.isPending}
        onCancel={() => setDeleteConfirmOpen(false)}
        onConfirm={() => {
          if (!selected || !latest || selectedIsReferenced) return;
          setDeleteConfirmOpen(false);
          deleteRole.mutate({ id: selected, expected_latest_version: latest.version, confirm_unreferenced: true, reason: 'RAM role deleted after unreferenced confirmation' });
        }}
      />
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
    <div className="mt-3 max-h-56 space-y-1 overflow-y-auto rounded border border-border-base p-2" data-testid="access-role-permissions">
      {catalog.map((permission) => (
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
          <span className="font-mono">{permission.key}</span>
          <AccessRiskBadge risk={permission.risk} />
        </button>
      ))}
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

function TeamRoleMappingsView({
  roles,
  teams,
  mappingEntries,
  canManageAccess,
}: {
  roles: RAMRole[];
  teams: TeamView[];
  mappingEntries: MappingEntry[];
  canManageAccess: boolean;
}): React.ReactElement {
  return (
    <div className="space-y-4" data-testid="access-team-role-mappings-view">
      <section className="rounded border border-border-base bg-bg-elevated" data-testid="access-team-role-mappings">
        <div className="border-b border-border-base px-4 py-3">
          <h2 className="text-sm font-semibold text-text-primary">Team Role mappings</h2>
          <p className="mt-1 text-xs text-text-muted">Preview impact, then replace with optimistic concurrency control.</p>
        </div>
        {teams.length === 0 ? (
          <p className="p-4 text-sm text-text-muted">No teams.</p>
        ) : (
          <div className="divide-y divide-border-base">
            {mappingEntries.map((entry) => (
              <TeamRoleMappingRow key={`${entry.team.id}:${entry.role}`} entry={entry} roles={roles} canManageAccess={canManageAccess} />
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

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

function TeamRoleMappingRow({ entry, roles, canManageAccess }: { entry: MappingEntry; roles: RAMRole[]; canManageAccess: boolean }): React.ReactElement {
  const preview = usePreviewTeamRoleRAMMapping();
  const replace = useReplaceTeamRoleRAMMapping();
  const current = entry.query.data;
  const [draft, setDraft] = useState<string[] | null>(null);
  const [confirming, setConfirming] = useState(false);
  const selected = draft ?? current?.ram_role_ids ?? [];
  const changed = Boolean(current && [...selected].sort().join('|') !== [...current.ram_role_ids].sort().join('|'));
  const updateSelection = (roleIDs: string[]): void => {
    setDraft(roleIDs);
    preview.reset();
  };
  const runPreview = (): void => preview.mutate({ team_id: entry.team.id, role: entry.role, ram_role_ids: selected });
  const save = (): void => {
    if (!current || !preview.data) return;
    replace.mutate(
      { team_id: entry.team.id, role: entry.role, ram_role_ids: selected, expected_version: current.version },
      { onSuccess: () => { setDraft(null); preview.reset(); } },
    );
  };
  const previewAddedRoleCount = preview.data?.added_ram_role_ids?.length ?? 0;
  const previewRemovedRoleCount = preview.data?.removed_ram_role_ids?.length ?? 0;
  const previewProjectCount = preview.data?.affected_project_ids?.length ?? 0;
  return (
    <div className="p-4" data-testid={`access-mapping-${entry.team.id}-${entry.role}`}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div><span className="font-semibold text-text-primary">{entry.team.name}</span><span className="mx-2 text-text-muted">/</span><span className="font-mono text-sm">{entry.role}</span></div>
        {current && <AccessMetaPill>v{current.version}</AccessMetaPill>}
      </div>
      {entry.query.isLoading ? <p className="mt-2 text-xs text-text-muted">Loading mapping…</p> : entry.query.isError ? (
        <p className="mt-2 text-xs text-danger" role="alert">{(entry.query.error as Error)?.message ?? 'Mapping unavailable'}</p>
      ) : (
        <>
          <div className="mt-2 max-w-xl">
            <EntityMultiSelect
              testId={`access-mapping-roles-${entry.team.id}-${entry.role}`}
              options={roles.map((role) => ({ value: role.id, label: role.name, hint: role.description }))}
              values={selected}
              onChange={updateSelection}
              placeholder="Select RAM Roles"
              searchPlaceholder="Search RAM Roles"
              emptyLabel="No matching RAM Roles"
              ariaLabel={`RAM Roles for ${entry.team.name} / ${entry.role}`}
              disabled={!canManageAccess || replace.isPending}
            />
          </div>
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <button type="button" className="rounded border border-border-base px-2.5 py-1 text-xs" disabled={!changed || preview.isPending} onClick={runPreview}>Preview impact</button>
            <button type="button" className="rounded bg-btn-primary-bg px-2.5 py-1 text-xs font-medium text-btn-primary-fg" disabled={!preview.data || replace.isPending} onClick={() => setConfirming(true)}>Save mapping</button>
            {preview.data && <span className="text-xs text-text-muted" data-testid="access-mapping-preview">{preview.data.affected_members} members · +{previewAddedRoleCount} / −{previewRemovedRoleCount} roles · {previewProjectCount} projects</span>}
          </div>
          {(preview.isError || replace.isError) && <p className="mt-2 text-xs text-danger" role="alert">{((preview.error ?? replace.error) as Error).message}</p>}
          <ConfirmModal
            open={confirming}
            title="Apply Team Role mapping"
            message={
              <div className="space-y-2" data-testid="access-mapping-confirm-diff">
                <p>This immediately changes effective permissions for {preview.data?.affected_members ?? 0} members.</p>
                <p className="font-mono text-xs">
                  {(preview.data?.current_ram_role_ids ?? []).join(', ') || 'none'} → {(preview.data?.next_ram_role_ids ?? []).join(', ') || 'none'}
                </p>
                <p className="text-xs">{previewProjectCount} linked projects use this Team Role scope.</p>
              </div>
            }
            confirmLabel="Apply now"
            danger
            busy={replace.isPending}
            onCancel={() => setConfirming(false)}
            onConfirm={() => { setConfirming(false); save(); }}
          />
        </>
      )}
    </div>
  );
}

function SubjectDecisionView({
  decisions,
  subjectByRef,
  permissionByKey,
  memberEntries,
  mappingEntries,
  onSelectSubject,
}: {
  decisions: AccessDecision[];
  subjectByRef: Map<string, AccessSubject>;
  permissionByKey: Map<string, AccessPermissionDefinition>;
  memberEntries: MemberEntry[];
  mappingEntries: MappingEntry[];
  onSelectSubject: (subjectRef: string) => void;
}): React.ReactElement {
  const groups = useMemo(() => {
    const bySubject = new Map<string, AccessDecision[]>();
    for (const decision of decisions) {
      const rows = bySubject.get(decision.subject_ref) ?? [];
      rows.push(decision);
      bySubject.set(decision.subject_ref, rows);
    }
    return [...bySubject.entries()];
  }, [decisions]);
  if (groups.length === 0) {
    return <EmptyState title="No matching access decisions" body="Change the filters to widen the API query." testId="access-empty" />;
  }
  return (
    <div className="space-y-3" data-testid="access-subject-view">
      {groups.map(([subjectRef, rows]) => {
        const subject = subjectByRef.get(subjectRef);
        return (
          <section key={subjectRef} className="rounded border border-border-base bg-bg-elevated" onClick={() => onSelectSubject(subjectRef)}>
            <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border-base px-4 py-3">
              <div>
                <h2 className="text-sm font-semibold text-text-primary">{subject?.name ?? subjectRef}</h2>
                <p className="text-xs text-text-muted">{subjectRef}</p>
              </div>
              <div className="flex flex-wrap gap-2">
                {subject?.role && <AccessMetaPill>{subject.role}</AccessMetaPill>}
                {subject?.status && <AccessMetaPill>{subject.status}</AccessMetaPill>}
              </div>
            </div>
            <details className="group" data-testid={`access-subject-effective-${subjectRef}`}>
              <summary className="cursor-pointer px-4 py-3 text-xs font-semibold text-text-secondary">
                {rows.filter((row) => row.allowed).length} effective permissions · show source chain
              </summary>
              <div className="border-t border-border-base px-4 py-3 text-xs text-text-secondary">
                {memberEntries.flatMap(({ team, query }) => (query.data ?? [])
                  .filter((member) => member.member_ref === subjectRef)
                  .map((member) => {
                    const teamRoles = member.roles ?? [member.role];
                    return teamRoles.map((teamRole) => {
                      const mapping = mappingEntries.find((entry) => entry.team.id === team.id && entry.role === teamRole)?.query.data;
                      const sourced = rows.filter((row) => row.source === 'team_role_ram' && row.evidence_ref.includes(`${team.id}/${teamRole}/`));
                      return (
                        <p key={`${team.id}:${teamRole}`} className="mb-1">
                          <span className="font-mono">membership:{team.name}</span> -&gt; Team Role <strong>{teamRole}</strong> -&gt; RAM Role {(mapping?.ram_role_ids ?? []).join(', ') || 'none'} -&gt; {sourced.length || rows.filter((row) => row.source === 'team_member').length} scoped effective permissions
                        </p>
                      );
                    });
                  }))}
                {rows.filter((row) => row.source === 'custom_role').map((row) => (
                  <p key={`${row.evidence_ref}:${row.permission}`} className="mb-1">
                    <span className="font-mono">direct binding</span> -&gt; RAM Role {row.role_id || roleIDFromEvidence(row.evidence_ref) || row.source} -&gt; {row.permission} on {accessResourceLabel(row.resource)}
                    {row.expires_at ? ` - expires ${displayAccessDate(row.expires_at)}` : ''}
                  </p>
                ))}
                {rows.some((row) => !['team_member', 'team_role_ram', 'custom_role'].includes(row.source)) && <p>Other bindings: {[...new Set(rows.filter((row) => !['team_member', 'team_role_ram', 'custom_role'].includes(row.source)).map((row) => row.source))].join(', ')}</p>}
              </div>
              <DecisionTable decisions={rows} subjectByRef={subjectByRef} permissionByKey={permissionByKey} compact />
            </details>
          </section>
        );
      })}
    </div>
  );
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
            const rowKey = `${decision.subject_ref}-${decision.permission}-${accessResourceKey(decision.resource)}-${decision.source}-${decision.evidence_ref}-${decision.status}`;
            return (
              <tr key={rowKey} className="border-b border-border-base last:border-0">
                <td className="px-4 py-3">
                  <div className="font-medium text-text-primary">{subject?.name ?? decision.subject_ref}</div>
                  <div className="font-mono text-xs text-text-muted">{decision.subject_ref}</div>
                </td>
                <td className="px-4 py-3">
                  <div className="font-mono text-xs font-semibold text-text-primary">{decision.permission}</div>
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

function PermissionCatalog({ catalog }: { catalog: AccessPermissionDefinition[] }): React.ReactElement {
  return (
    <section className="rounded border border-border-base bg-bg-elevated" data-testid="access-catalog">
      <div className="border-b border-border-base px-4 py-3">
        <h2 className="text-sm font-semibold text-text-primary">Permission catalog</h2>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[44rem] text-left text-sm">
          <thead className="border-b border-border-base text-[0.6875rem] uppercase text-text-muted">
            <tr>
              <th className="px-4 py-2 font-semibold">Key</th>
              <th className="px-4 py-2 font-semibold">Scope</th>
              <th className="px-4 py-2 font-semibold">Risk</th>
              <th className="px-4 py-2 font-semibold">Sources</th>
            </tr>
          </thead>
          <tbody>
            {catalog.map((permission) => (
              <tr key={permission.key} className="border-b border-border-base last:border-0">
                <td className="px-4 py-3">
                  <div className="font-mono text-xs font-semibold text-text-primary">{permission.key}</div>
                  <div className="mt-1 text-xs text-text-muted">{permission.description}</div>
                </td>
                <td className="px-4 py-3 text-xs text-text-secondary">{permission.resource_kinds.join(', ')}</td>
                <td className="px-4 py-3"><AccessRiskBadge risk={permission.risk} /></td>
                <td className="px-4 py-3 font-mono text-xs text-text-secondary">{permission.legacy_sources.join(', ')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function SubjectAccessSidebar({
  decisions,
  grants,
  subjects,
  selectedSubjectRef,
  permissionByKey,
}: {
  decisions: AccessDecision[];
  grants: AccessGrant[];
  subjects: AccessSubject[];
  selectedSubjectRef: string;
  permissionByKey: Map<string, AccessPermissionDefinition>;
}): React.ReactElement {
  const subjectRef = selectedSubjectRef || decisions[0]?.subject_ref || subjects[0]?.ref || '';
  const subject = subjects.find((entry) => entry.ref === subjectRef);
  const rows = decisions.filter((decision) => decision.subject_ref === subjectRef);
  const audit = usePermissionAudit(subjectRef, Boolean(subjectRef));
  const effectiveRows = rows.filter((row) => row.allowed);
  const direct = grants.filter((grant) => grant.subject_ref === subjectRef && grant.source === 'custom_role');
  return (
    <section className="rounded border border-border-base bg-bg-elevated" data-testid="access-subject-sidebar">
      <div className="border-b border-border-base px-4 py-3">
        <h2 className="text-sm font-semibold text-text-primary">Permission trace</h2>
        <p className="mt-1 font-mono text-xs text-text-muted">{subject?.name ?? subjectRef}</p>
      </div>
      <div className="space-y-4 p-4">
        <div data-testid="access-permission-trace">
          <h3 className="text-xs font-semibold uppercase text-text-muted">Effective source trace</h3>
          {effectiveRows.length === 0 ? (
            <p className="mt-2 text-sm text-text-muted">No allowed permissions in the current filter.</p>
          ) : (
            <div className="mt-2 space-y-2">
              {effectiveRows.slice(0, 8).map((row) => (
                <div key={`${row.permission}:${row.source}:${row.evidence_ref}`} className="rounded border border-border-base bg-bg-base p-2 text-xs">
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-mono font-semibold text-text-primary">{row.permission}</span>
                    <AccessRiskBadge risk={row.risk ?? permissionByKey.get(row.permission)?.risk ?? 'low'} />
                  </div>
                  <p className="mt-1 text-text-secondary">
                    {row.source === 'team_role_ram'
                      ? `Team membership -> Team Role -> RAM Role ${row.role_id || roleIDFromEvidence(row.evidence_ref) || 'unknown'}`
                      : row.source === 'custom_role'
                        ? `direct binding -> RAM Role ${row.role_id || roleIDFromEvidence(row.evidence_ref) || 'unknown'}`
                        : `${row.source} -> effective permission`}
                  </p>
                  <p className="mt-1 font-mono text-[0.6875rem] text-text-muted">{row.evidence_ref}</p>
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
                  <div className="font-mono font-semibold text-text-primary">{grant.permission}</div>
                  <div className="mt-1 text-text-secondary">{accessResourceLabel(grant.resource)} · {displayAccessDate(grant.expires_at)}</div>
                  <div className="mt-1 font-mono text-text-muted">{grant.role_id || grant.id}</div>
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

function RoleManagement({
  roles,
  catalog,
  canManageAccess,
}: {
  roles: AccessRole[];
  catalog: AccessPermissionDefinition[];
  canManageAccess: boolean;
}): React.ReactElement {
  const updateRole = useAccessRoleUpdate();
  const [drafts, setDrafts] = useState<Record<string, string[]>>({});
  const [reason, setReason] = useState('access role review');
  const permissionsFor = (role: AccessRole): string[] => drafts[role.id] ?? role.permissions;
  const togglePermission = (role: AccessRole, permission: string): void => {
    setDrafts((prev) => {
      const current = new Set(prev[role.id] ?? role.permissions);
      if (current.has(permission)) current.delete(permission);
      else current.add(permission);
      return { ...prev, [role.id]: [...current].sort() };
    });
  };
  const save = (role: AccessRole): void => {
    updateRole.mutate({ role_id: role.id, permissions: permissionsFor(role), reason });
  };
  return (
    <section className="rounded border border-border-base bg-bg-elevated" data-testid="access-role-management">
      <div className="border-b border-border-base px-4 py-3">
        <h2 className="text-sm font-semibold text-text-primary">Role management</h2>
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
        {roles.map((role) => (
          <div key={role.id} className="rounded border border-border-base p-3">
            <div className="flex items-start justify-between gap-2">
              <div>
                <h3 className="text-sm font-semibold text-text-primary">{role.name}</h3>
                <p className="text-xs text-text-muted">{role.description}</p>
              </div>
              {role.high_risk && <AccessRiskBadge risk="high" />}
            </div>
            <div className="mt-3 space-y-1">
              {catalog.map((permission) => {
                const checked = permissionsFor(role).includes(permission.key);
                return (
                  <button
                    key={`${role.id}-${permission.key}`}
                    type="button"
                    aria-pressed={checked}
                    disabled={!canManageAccess || !role.editable || updateRole.isPending}
                    onClick={() => togglePermission(role, permission.key)}
                    className={[
                      'flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs text-text-secondary',
                      checked ? 'bg-status-emerald-bg text-status-emerald-fg' : 'hover:bg-bg-subtle',
                      !role.editable ? 'opacity-60' : '',
                    ].join(' ')}
                  >
                    <span className={checked ? 'h-2 w-2 rounded-full bg-success' : 'h-2 w-2 rounded-full border border-border-strong'} aria-hidden="true" />
                    <span className="font-mono">{permission.key}</span>
                  </button>
                );
              })}
            </div>
            <button
              type="button"
              disabled={!canManageAccess || !role.editable || updateRole.isPending || !reason.trim()}
              onClick={() => save(role)}
              className="mt-3 rounded border border-border-base px-2.5 py-1.5 text-xs font-semibold text-text-primary hover:bg-bg-subtle disabled:opacity-50"
              data-testid={`access-save-role-${role.id}`}
            >
              Save role
            </button>
          </div>
        ))}
        {updateRole.isError && <p className="text-xs text-danger" role="alert">{(updateRole.error as Error).message}</p>}
      </div>
    </section>
  );
}

function GrantRevoke({ grants, canManageAccess, onToast }: { grants: AccessGrant[]; canManageAccess: boolean; onToast: (toast: AccessToast) => void }): React.ReactElement {
  const revoke = useAccessBulkRevoke();
  const previewRevoke = useAccessRevokePreview();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [reason, setReason] = useState('access cleanup');
  const [preview, setPreview] = useState<((AccessBatchPreview & { preview_id: string; token: string }) & { grant_ids: string[]; reason: string; message: string; idempotency_key: string }) | null>(null);
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
        <h2 className="text-sm font-semibold text-text-primary">Active grants</h2>
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
              {grants.map((grant) => (
                <tr key={grant.id} className="border-b border-border-base last:border-0">
                  <td className="px-2 py-3 align-top">
                    <input
                      type="checkbox"
                      checked={selected.has(grant.id)}
                      disabled={!canManageAccess}
                      onChange={() => toggle(grant.id)}
                      aria-label={`Select ${grant.permission} for revoke`}
                      data-testid="access-grant-select"
                    />
                  </td>
                  <td className="min-w-0 px-2 py-3">
                    <span className="block font-mono text-xs font-semibold text-text-primary">{grant.permission}</span>
                    <span className="block truncate text-xs text-text-secondary">
                      {grant.subject_name}
                      {' -> '}
                      {accessResourceLabel(grant.resource)}
                    </span>
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
  permissions,
  resources,
  canManageAccess,
  mode,
  onToast,
  onClose,
}: {
  subjects: AccessSubject[];
  permissions: AccessPermissionDefinition[];
  resources: AccessResourceScope[];
  canManageAccess: boolean;
  mode: 'direct' | 'batch';
  onToast: (toast: AccessToast) => void;
  onClose: () => void;
}): React.ReactElement {
  const containerRef = useModalA11y({ open: true, onClose });
  const previewMutation = useAccessBatchPreview();
  const applyMutation = useAccessBatchApply();
  const [step, setStep] = useState(0);
  const [request, setRequest] = useState<AccessBatchRequest>(() => emptyBatchRequest(resources));
  const [preview, setPreview] = useState<AccessBatchPreview | null>(null);
  const [result, setResult] = useState<AccessBatchResult | null>(null);
  const [highRiskAck, setHighRiskAck] = useState(false);
  const title = mode === 'direct' ? 'Add direct binding' : 'Batch authorization';

  const canPreview =
    canManageAccess &&
    request.subject_refs.length > 0 &&
    request.permission_keys.length > 0 &&
    request.resources.length > 0 &&
    request.reason.trim().length > 0;
  const canConfirm = canManageAccess && !!preview && (preview.summary.high_risk === 0 || highRiskAck);

  const toggleSubject = (ref: string): void => {
    setRequest((prev) => ({ ...prev, subject_refs: toggleValue(prev.subject_refs, ref) }));
  };
  const togglePermission = (key: string): void => {
    setRequest((prev) => ({ ...prev, permission_keys: toggleValue(prev.permission_keys, key) }));
  };
  const toggleResource = (resource: AccessResourceScope): void => {
    setRequest((prev) => {
      const keys = new Set(prev.resources.map(accessResourceKey));
      const next = keys.has(accessResourceKey(resource))
        ? prev.resources.filter((r) => accessResourceKey(r) !== accessResourceKey(resource))
        : [...prev.resources, resource];
      return { ...prev, resources: next };
    });
  };
  const runPreview = (): void => {
    previewMutation.mutate(request, {
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
      { ...request, preview_request_id: preview?.request_id },
      {
        onSuccess: (data) => {
          setResult(data);
          setStep(3);
          onToast({
            tone: data.summary.partial_failure ? 'warning' : 'success',
            message: mode === 'direct'
              ? (data.summary.partial_failure ? 'Direct binding completed with partial failure' : 'Direct binding granted')
              : (data.summary.partial_failure ? 'Batch grant completed with partial failure' : 'Batch grant applied'),
          });
        },
        onError: (error) => onToast(accessToastFromError(error, mode === 'direct' ? 'Direct binding failed' : 'Batch grant failed')),
      },
    );
  };

  return (
    <div className="fixed inset-0 z-50 bg-black/30" data-testid="access-batch-drawer-backdrop">
      <aside
        ref={containerRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="fixed inset-y-0 right-0 flex h-full w-full max-w-3xl flex-col border-l border-border-base bg-bg-elevated text-text-primary shadow-2 md:w-[46rem]"
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
            aria-label="Close"
            title="Close"
            className="rounded p-1.5 text-text-secondary hover:bg-bg-subtle"
            onClick={onClose}
          >
            <IconClose />
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-5">
          {step === 0 && (
            <div className="space-y-4">
              <Picker title="Subjects">
                {subjects.map((subject) => (
                  <ChoiceRow
                    key={subject.ref}
                    checked={request.subject_refs.includes(subject.ref)}
                    disabled={!canManageAccess}
                    onChange={() => toggleSubject(subject.ref)}
                    label={subject.name}
                    detail={`${subject.ref} · ${subject.role ?? subject.kind} · ${subject.status ?? 'unknown'}`}
                  />
                ))}
              </Picker>
              <Picker title="Permissions">
                {permissions.map((permission) => (
                  <ChoiceRow
                    key={permission.key}
                    checked={request.permission_keys.includes(permission.key)}
                    disabled={!canManageAccess}
                    onChange={() => togglePermission(permission.key)}
                    label={permission.key}
                    detail={`${permission.label} · ${accessRiskLabel(permission.risk)}`}
                    badge={<AccessRiskBadge risk={permission.risk} />}
                  />
                ))}
              </Picker>
              <Picker title="Resources">
                {resources.map((resource) => (
                  <ChoiceRow
                    key={accessResourceKey(resource)}
                    checked={request.resources.some((r) => accessResourceKey(r) === accessResourceKey(resource))}
                    disabled={!canManageAccess}
                    onChange={() => toggleResource(resource)}
                    label={accessResourceLabel(resource)}
                    detail={resource.kind}
                  />
                ))}
              </Picker>
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
      </aside>
    </div>
  );
}

function toggleValue(values: string[], value: string): string[] {
  return values.includes(value) ? values.filter((v) => v !== value) : [...values, value];
}

function roleIDFromEvidence(evidenceRef: string): string {
  const parts = evidenceRef.split('/');
  return parts.length >= 3 ? parts[2] : '';
}

function Picker({ title, children }: { title: string; children: React.ReactNode }): React.ReactElement {
  return (
    <section className="rounded border border-border-base bg-bg-base">
      <h3 className="border-b border-border-base px-3 py-2 text-xs font-semibold uppercase text-text-muted">{title}</h3>
      <div className="grid gap-1 p-2 md:grid-cols-2">{children}</div>
    </section>
  );
}

function ChoiceRow({
  checked,
  disabled,
  onChange,
  label,
  detail,
  badge,
}: {
  checked: boolean;
  disabled?: boolean;
  onChange: () => void;
  label: string;
  detail: string;
  badge?: React.ReactNode;
}): React.ReactElement {
  return (
    <button
      type="button"
      aria-pressed={checked}
      disabled={disabled}
      onClick={onChange}
      className={[
        'flex min-w-0 items-start gap-2 rounded px-2 py-2 text-left text-sm',
        checked ? 'bg-status-emerald-bg text-status-emerald-fg' : 'hover:bg-bg-subtle',
        disabled ? 'opacity-50' : '',
      ].join(' ')}
    >
      <span className={checked ? 'mt-1 h-2 w-2 rounded-full bg-success' : 'mt-1 h-2 w-2 rounded-full border border-border-strong'} aria-hidden="true" />
      <span className="min-w-0 flex-1">
        <span className="block truncate font-medium text-text-primary">{label}</span>
        <span className="block truncate text-xs text-text-muted">{detail}</span>
      </span>
      {badge}
    </button>
  );
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
            <th className="px-3 py-2 font-semibold">Reason</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.id} className="border-b border-border-base last:border-0">
              <td className="px-3 py-2">{item.subject_name}</td>
              <td className="px-3 py-2 font-mono text-xs">{item.permission}</td>
              <td className="px-3 py-2">{accessResourceLabel(item.resource)}</td>
              <td className="px-3 py-2"><AccessStatusBadge status={item.status} /></td>
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

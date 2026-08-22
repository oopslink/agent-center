import type React from 'react';
import { useEffect, useMemo, useState } from 'react';
import { ApiError } from '@/api/client';
import {
  type AccessPermissionDefinition,
  type AccessRisk,
  type RAMRole,
  type RAMRoleDetail,
  useAccessOverview,
  useRAMRole,
  useRAMRoleCreate,
  useRAMRoleDelete,
  useRAMRoleNewVersion,
  useRAMRoleRevoke,
  useRAMRoleUpdate,
  useRAMRoles,
} from '@/api/access';
import {
  type TeamRAMRoleMapping,
  type TeamView,
  useAllTeamRoleRAMMappings,
  useReplaceTeamRoleRAMMapping,
  useTeams,
} from '@/api/teams';
import { AccessMetaPill, AccessRiskBadge } from '@/components/access/kit';
import { ConfirmModal } from '@/components/ConfirmModal';
import { EmptyState } from '@/components/EmptyState';
import { IconClose, IconSearch, IconTrash } from '@/components/icons';
import { Skeleton } from '@/components/Skeleton';

type Notice = { tone: 'success' | 'warning' | 'danger'; message: string } | null;
type RoleFilter = 'all' | 'system' | 'custom';
type RiskFilter = 'all' | AccessRisk;
type DrawerState = { mode: 'create' | 'edit' | 'duplicate'; roleId?: string } | null;

type MappingEntry = {
  team: TeamView;
  role: string;
  query: { data?: TeamRAMRoleMapping; isLoading: boolean; isError: boolean; error: unknown; refetch?: () => unknown };
};

const PAGE_SIZES = [4, 8, 16];

export default function RAMRoles(): React.ReactElement {
  const overview = useAccessOverview();
  const ramRoles = useRAMRoles();
  const teams = useTeams();
  const mappingEntries = useAllTeamRoleRAMMappings(teams.data ?? []) as MappingEntry[];
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  const [kind, setKind] = useState<RoleFilter>('all');
  const [risk, setRisk] = useState<RiskFilter>('all');
  const [scope, setScope] = useState('all');
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(8);
  const [drawer, setDrawer] = useState<DrawerState>(null);
  const [notice, setNotice] = useState<Notice>(null);

  const roles = ramRoles.data?.roles ?? [];
  const refsByRole = useMemo(() => ramRoleReferences(mappingEntries), [mappingEntries]);
  const scopes = useMemo(() => ['all', ...Array.from(new Set(roles.map((role) => role.scope || 'mixed'))).sort()], [roles]);
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return roles.filter((role) => {
      const text = [role.name, role.stable_key, role.id, role.description, role.scope, role.permissions.join(' ')].join(' ').toLowerCase();
      return (!q || text.includes(q))
        && (kind === 'all' || role.kind === kind)
        && (risk === 'all' || role.risk === risk)
        && (scope === 'all' || role.scope === scope);
    });
  }, [kind, query, risk, roles, scope]);
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
  const currentPage = Math.min(page, totalPages);
  const pageRows = filtered.slice((currentPage - 1) * pageSize, currentPage * pageSize);
  const selected = selectedId ?? pageRows[0]?.id ?? filtered[0]?.id ?? roles[0]?.id ?? null;
  const selectedRefs = selected ? refsByRole.get(selected) ?? [] : [];
  const detail = useRAMRole(selected);
  const customCount = roles.filter((role) => role.kind === 'custom').length;
  const highRiskCount = roles.filter((role) => role.risk === 'high').length;
  const referencedCount = roles.filter((role) => (refsByRole.get(role.id) ?? []).length > 0 || (role.references ?? 0) > 0).length;
  const permissionCount = new Set(roles.flatMap((role) => role.permissions)).size;

  useEffect(() => setPage(1), [kind, query, risk, scope, pageSize]);

  return (
    <section className="space-y-4" data-testid="page-RAMRoles">
      {notice && <NoticeBanner notice={notice} onClose={() => setNotice(null)} />}
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs font-semibold uppercase text-text-muted">Access / RAM Roles</p>
          <h1 className="mt-1 font-heading text-2xl font-semibold text-text-primary">RAM Roles</h1>
          <p className="mt-1 max-w-3xl text-sm text-text-secondary">
            Manage versioned authorization roles, permission scope, Team Role references, delete safeguards, and reference migrations.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm font-semibold text-text-primary hover:bg-bg-subtle" data-testid="ram-roles-refresh" onClick={() => {
            void ramRoles.refetch();
            void overview.refetch();
            void teams.refetch();
            for (const entry of mappingEntries) void entry.query.refetch?.();
            setNotice({ tone: 'success', message: 'RAM Roles refreshed from server state.' });
          }}>
            Refresh
          </button>
          <button type="button" className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-semibold text-btn-primary-fg" data-testid="ram-role-create-open" onClick={() => setDrawer({ mode: 'create' })}>
            New RAM Role
          </button>
        </div>
      </header>

      <div className="grid gap-3 md:grid-cols-4">
        <Summary label="Current versions" value={roles.length} />
        <Summary label="Custom roles" value={customCount} />
        <Summary label="Team references" value={referencedCount} />
        <Summary label="Permission keys" value={permissionCount} tone={highRiskCount > 0 ? 'warning' : 'default'} />
      </div>

      <section className="rounded border border-border-base bg-bg-elevated" data-testid="ram-role-controls">
        <div className="grid gap-3 border-b border-border-base px-4 py-3 lg:grid-cols-[minmax(16rem,1fr)_repeat(4,max-content)]">
          <label className="relative block">
            <span className="sr-only">Search RAM Roles</span>
            <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-muted"><IconSearch /></span>
            <input className="w-full rounded border border-border-base bg-bg-base py-2 pl-9 pr-3 text-sm text-text-primary" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search name, stable key, permission" data-testid="ram-role-search" />
          </label>
          <Select label="Kind" value={kind} options={['all', 'system', 'custom']} onChange={(value) => setKind(value as RoleFilter)} />
          <Select label="Risk" value={risk} options={['all', 'high', 'medium', 'low']} onChange={(value) => setRisk(value as RiskFilter)} />
          <Select label="Scope" value={scope} options={scopes} onChange={setScope} />
          <Select label="Rows" value={String(pageSize)} options={PAGE_SIZES.map(String)} onChange={(value) => setPageSize(Number(value))} />
        </div>
        {ramRoles.isLoading && <div className="p-4"><Skeleton height="18rem" /></div>}
        {ramRoles.isError && <p className="p-4 text-sm text-danger" role="alert">{(ramRoles.error as Error).message}</p>}
        {ramRoles.isSuccess && filtered.length === 0 && <EmptyState title="No RAM Roles match" body="Change search or filters to widen the catalog." testId="ram-roles-empty" />}
        {ramRoles.isSuccess && filtered.length > 0 && (
          <>
            <div className="overflow-x-auto">
              <table className="w-full min-w-[64rem] text-left text-sm" data-testid="ram-role-table">
                <thead className="border-b border-border-base text-[0.6875rem] uppercase text-text-muted">
                  <tr>
                    <th className="px-4 py-2 font-semibold">RAM Role</th>
                    <th className="px-4 py-2 font-semibold">Scope</th>
                    <th className="px-4 py-2 font-semibold">Risk</th>
                    <th className="px-4 py-2 font-semibold">Version</th>
                    <th className="px-4 py-2 font-semibold">Permissions</th>
                    <th className="px-4 py-2 font-semibold">Team Role references</th>
                  </tr>
                </thead>
                <tbody>
                  {pageRows.map((role) => {
                    const refs = refsByRole.get(role.id) ?? [];
                    return (
                      <tr key={role.id} className={`cursor-pointer border-b border-border-base last:border-0 ${selected === role.id ? 'bg-brand/5' : 'hover:bg-bg-subtle'}`} data-testid={`ram-role-row-${role.id}`} onClick={() => setSelectedId(role.id)}>
                        <td className="px-4 py-3">
                          <div className="font-semibold text-text-primary">{role.name}</div>
                          <div className="font-mono text-xs text-text-muted">{role.stable_key || role.id}</div>
                          <div className="mt-1 text-xs text-text-secondary">{role.description}</div>
                        </td>
                        <td className="px-4 py-3 font-mono text-xs text-text-secondary">{role.scope}</td>
                        <td className="px-4 py-3"><AccessRiskBadge risk={role.risk} /></td>
                        <td className="px-4 py-3 font-mono text-xs text-text-secondary">v{role.version}</td>
                        <td className="px-4 py-3"><PermissionSummary permissions={role.permissions} /></td>
                        <td className="px-4 py-3 text-xs text-text-secondary">{refs.length || role.references || 0} references</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            <Pagination page={currentPage} totalPages={totalPages} total={filtered.length} pageSize={pageSize} onPage={setPage} />
          </>
        )}
      </section>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
        <RAMRoleDetailPanel
          detail={detail.data}
          loading={detail.isLoading}
          error={detail.error}
          refs={selectedRefs}
          roles={roles}
          mappingEntries={mappingEntries}
          onEdit={() => selected && setDrawer({ mode: 'edit', roleId: selected })}
          onDuplicate={() => selected && setDrawer({ mode: 'duplicate', roleId: selected })}
          onNotice={setNotice}
          onSelect={setSelectedId}
        />
        <PermissionCatalogSummary catalog={overview.data?.catalog ?? []} roles={roles} />
      </div>

      {drawer && (
        <RAMRoleDrawer
          state={drawer}
          catalog={overview.data?.catalog ?? []}
          sourceRole={roles.find((role) => role.id === drawer.roleId)}
          onClose={() => setDrawer(null)}
          onNotice={setNotice}
          onSelect={setSelectedId}
        />
      )}
    </section>
  );
}

function RAMRoleDetailPanel({
  detail,
  loading,
  error,
  refs,
  roles,
  mappingEntries,
  onEdit,
  onDuplicate,
  onNotice,
  onSelect,
}: {
  detail?: RAMRoleDetail;
  loading: boolean;
  error: unknown;
  refs: Array<{ team: TeamView; role: string }>;
  roles: RAMRole[];
  mappingEntries: MappingEntry[];
  onEdit: () => void;
  onDuplicate: () => void;
  onNotice: (notice: Notice) => void;
  onSelect: (roleId: string | null) => void;
}): React.ReactElement {
  const deleteRole = useRAMRoleDelete();
  const revokeRole = useRAMRoleRevoke();
  const replaceMapping = useReplaceTeamRoleRAMMapping();
  const [confirming, setConfirming] = useState(false);
  const [migrationTarget, setMigrationTarget] = useState('');
  const [deleteName, setDeleteName] = useState('');

  useEffect(() => {
    setConfirming(false);
    setMigrationTarget('');
    setDeleteName('');
  }, [detail?.id]);

  if (loading) return <section className="rounded border border-border-base bg-bg-elevated p-4"><Skeleton height="16rem" /></section>;
  if (error) return <section className="rounded border border-danger/40 bg-danger/10 p-4 text-sm text-danger" role="alert">{(error as Error).message}</section>;
  if (!detail) return <EmptyState title="Select a RAM Role" body="Choose a role from the list to inspect versions, permissions, and references." testId="ram-role-detail-empty" />;

  const references = refs.length > 0 ? refs : detail.references.map((ref) => ({ team: { id: ref.team_id, name: ref.team_name, roles: [] } as TeamView, role: ref.team_role }));
  const blocked = references.length > 0;
  const latest = detail.latest;
  const canDelete = detail.kind === 'custom' && !blocked && deleteName === detail.name;
  const migrate = async (): Promise<void> => {
    if (!migrationTarget || migrationTarget === detail.id) return;
    const pending = refs.flatMap((ref) => {
      const mapping = mappingEntries.find((entry) => entry.team.id === ref.team.id && entry.role === ref.role)?.query.data;
      if (!mapping) return [];
      const next = Array.from(new Set(mapping.ram_role_ids.map((id) => (id === detail.id ? migrationTarget : id))));
      return [{ mapping, next }];
    });
    for (const item of pending) {
      await replaceMapping.mutateAsync({
        team_id: item.mapping.team_id,
        role: item.mapping.team_role,
        ram_role_ids: item.next,
        expected_version: item.mapping.version,
      });
    }
    onNotice({ tone: 'success', message: `Migrated ${pending.length} Team Role references from ${detail.name}.` });
  };
  const remove = (): void => {
    const payload = { id: detail.id, expected_latest_version: latest.version, reason: 'RAM Roles standalone delete safeguard' };
    if (detail.kind === 'custom') {
      deleteRole.mutate({ ...payload, confirm_unreferenced: true }, {
        onSuccess: () => {
          onNotice({ tone: 'success', message: `Deleted RAM Role ${detail.name}.` });
          onSelect(null);
        },
      });
    } else {
      revokeRole.mutate(payload, {
        onSuccess: () => {
          onNotice({ tone: 'success', message: `Revoked RAM Role ${detail.name}.` });
          onSelect(null);
        },
      });
    }
  };

  return (
    <section className="rounded border border-border-base bg-bg-elevated" data-testid="ram-role-detail">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border-base px-4 py-3">
        <div>
          <h2 className="text-lg font-semibold text-text-primary">{detail.name}</h2>
          <p className="mt-1 font-mono text-xs text-text-muted">{detail.stable_key || detail.id} · latest v{latest.version}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button type="button" className="rounded border border-border-base px-2.5 py-1 text-xs font-semibold hover:bg-bg-subtle" onClick={onEdit} data-testid="ram-role-edit-open">Edit</button>
          <button type="button" className="rounded border border-border-base px-2.5 py-1 text-xs font-semibold hover:bg-bg-subtle" onClick={onDuplicate} data-testid="ram-role-duplicate-open">Duplicate</button>
          <button type="button" className="rounded border border-danger/40 px-2.5 py-1 text-xs font-semibold text-danger hover:bg-danger/10" onClick={() => setConfirming(true)} data-testid="ram-role-delete-open"><IconTrash className="inline h-3.5 w-3.5" /> Delete</button>
        </div>
      </div>
      <div className="grid gap-4 p-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <div className="space-y-4">
          <section className="rounded border border-border-base bg-bg-subtle p-3">
            <div className="flex flex-wrap gap-2">
              <AccessMetaPill>{detail.kind}</AccessMetaPill>
              <AccessMetaPill>{detail.scope}</AccessMetaPill>
              <AccessRiskBadge risk={latest.risk} />
            </div>
            <p className="mt-2 text-sm text-text-secondary">{detail.description}</p>
            <div className="mt-3" data-testid="ram-role-permission-summary">
              <PermissionSummary permissions={latest.permissions} expanded />
            </div>
          </section>
          <section>
            <h3 className="text-sm font-semibold text-text-primary">Version history</h3>
            <div className="mt-2 grid gap-2">
              {detail.versions.map((version) => (
                <div key={version.version} className="rounded border border-border-base p-3" data-testid={`ram-role-version-${version.version}`}>
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="font-mono text-xs font-semibold">v{version.version}</span>
                    <AccessRiskBadge risk={version.risk} />
                  </div>
                  <p className="mt-1 text-sm text-text-secondary">{version.description}</p>
                  <PermissionSummary permissions={version.permissions} />
                </div>
              ))}
            </div>
          </section>
        </div>
        <aside className="space-y-4">
          <section className="rounded border border-border-base p-3" data-testid="ram-role-team-references">
            <h3 className="text-sm font-semibold text-text-primary">Team Role references</h3>
            {references.length === 0 ? <p className="mt-2 text-sm text-text-muted">No Team Role references.</p> : (
              <div className="mt-2 space-y-2">
                {references.map((ref) => <div key={`${ref.team.id}:${ref.role}`} className="rounded border border-border-base bg-bg-subtle px-2 py-1.5 text-sm">{ref.team.name} / <span className="font-mono">{ref.role}</span></div>)}
              </div>
            )}
            {blocked && (
              <div className="mt-3 space-y-2" data-testid="ram-role-reference-block">
                <p className="text-xs text-danger">Delete is blocked until references are removed or migrated.</p>
                <select className="w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm" value={migrationTarget} onChange={(event) => setMigrationTarget(event.target.value)} data-testid="ram-role-migrate-target">
                  <option value="">Migration target</option>
                  {roles.filter((role) => role.id !== detail.id).map((role) => <option key={role.id} value={role.id}>{role.name}</option>)}
                </select>
                <button type="button" className="rounded bg-btn-primary-bg px-2.5 py-1 text-xs font-semibold text-btn-primary-fg disabled:opacity-50" disabled={!migrationTarget || replaceMapping.isPending || refs.length === 0} onClick={() => void migrate()} data-testid="ram-role-migrate-submit">Migrate references</button>
              </div>
            )}
          </section>
          <section className="rounded border border-border-base p-3" data-testid="ram-role-delete-safeguard">
            <h3 className="text-sm font-semibold text-text-primary">Delete safeguard</h3>
            <p className="mt-1 text-xs text-text-muted">Custom roles require latest version, no references, and typed confirmation. System roles are revoked through the same confirmation path.</p>
            {detail.kind === 'custom' && !blocked && (
              <label className="mt-3 block">
                <span className="text-xs font-semibold uppercase text-text-muted">Type {detail.name}</span>
                <input className="mt-1 w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm" value={deleteName} onChange={(event) => setDeleteName(event.target.value)} data-testid="ram-role-delete-name" />
              </label>
            )}
          </section>
        </aside>
      </div>
      {(deleteRole.isError || revokeRole.isError || replaceMapping.isError) && <p className="px-4 pb-4 text-sm text-danger" role="alert">{((deleteRole.error ?? revokeRole.error ?? replaceMapping.error) as Error).message}</p>}
      <ConfirmModal
        open={confirming}
        title="Delete RAM Role"
        message={blocked ? 'This RAM Role is referenced by Team Roles. Migrate or remove references first.' : `Delete ${detail.name}? This uses latest v${latest.version} and writes an audit event.`}
        confirmLabel={blocked ? 'Blocked' : detail.kind === 'custom' ? 'Delete' : 'Revoke'}
        danger
        busy={deleteRole.isPending || revokeRole.isPending}
        confirmDisabled={blocked || (detail.kind === 'custom' && !canDelete)}
        onCancel={() => setConfirming(false)}
        onConfirm={() => { setConfirming(false); remove(); }}
      />
    </section>
  );
}

function RAMRoleDrawer({
  state,
  catalog,
  sourceRole,
  onClose,
  onNotice,
  onSelect,
}: {
  state: { mode: 'create' | 'edit' | 'duplicate'; roleId?: string };
  catalog: AccessPermissionDefinition[];
  sourceRole?: RAMRole;
  onClose: () => void;
  onNotice: (notice: Notice) => void;
  onSelect: (roleId: string) => void;
}): React.ReactElement {
  const detail = useRAMRole(state.roleId ?? null);
  const create = useRAMRoleCreate();
  const update = useRAMRoleUpdate();
  const newVersion = useRAMRoleNewVersion();
  const base = detail.data?.latest ?? sourceRole;
  const [name, setName] = useState('');
  const [stableKey, setStableKey] = useState('');
  const [description, setDescription] = useState('');
  const [scope, setScope] = useState('team');
  const [permissions, setPermissions] = useState<string[]>([]);
  const conflict = (update.error instanceof ApiError && update.error.status === 409) || (newVersion.error instanceof ApiError && newVersion.error.status === 409);

  useEffect(() => {
    if (state.mode === 'create') {
      setName('');
      setStableKey('');
      setDescription('');
      setScope('team');
      setPermissions([]);
      return;
    }
    if (!base) return;
    setName(state.mode === 'duplicate' ? `${base.name} copy` : base.name);
    setStableKey(state.mode === 'duplicate' ? `${base.stable_key || base.id}.copy` : base.stable_key);
    setDescription(base.description);
    setScope(base.scope || 'team');
    setPermissions(base.permissions ?? []);
  }, [base?.id, base?.version, state.mode]);

  const title = state.mode === 'create' ? 'Create RAM Role' : state.mode === 'duplicate' ? 'Duplicate RAM Role' : 'Edit RAM Role';
  const payload = { name, stable_key: stableKey, description, scope, permissions, expected_latest_version: base?.version };
  const saveMetadata = (): void => {
    if (state.mode === 'create' || state.mode === 'duplicate') {
      create.mutate(payload, {
        onSuccess: (created) => {
          onNotice({ tone: 'success', message: `Created RAM Role ${created.name}.` });
          onSelect(created.id);
          onClose();
        },
      });
      return;
    }
    if (!state.roleId) return;
    update.mutate({ id: state.roleId, payload }, {
      onSuccess: (saved) => {
        onNotice({ tone: 'success', message: `Saved RAM Role ${saved.name}.` });
        onSelect(saved.id);
        onClose();
      },
    });
  };
  const saveVersion = (): void => {
    if (!state.roleId || state.mode !== 'edit') return;
    newVersion.mutate({ id: state.roleId, payload }, {
      onSuccess: (saved) => {
        onNotice({ tone: 'success', message: `Created v${saved.latest.version} for ${saved.name}.` });
        onSelect(saved.id);
        onClose();
      },
    });
  };

  return (
    <div className="fixed inset-0 z-50 bg-black/30" data-testid="ram-role-drawer-backdrop">
      <aside className="ml-auto flex h-full w-full max-w-2xl flex-col bg-bg-elevated shadow-2" data-testid="ram-role-drawer">
        <header className="flex items-start justify-between gap-3 border-b border-border-base px-5 py-4">
          <div>
            <h2 className="text-lg font-semibold text-text-primary">{title}</h2>
            <p className="mt-1 text-sm text-text-muted">{base ? `${base.stable_key || base.id} · latest v${base.version}` : 'New versioned authorization role'}</p>
          </div>
          <button type="button" aria-label="Close drawer" className="rounded border border-border-base p-2" onClick={onClose}><IconClose className="h-4 w-4" /></button>
        </header>
        <div className="flex-1 space-y-4 overflow-y-auto p-5">
          {state.mode !== 'create' && detail.isLoading && <Skeleton height="12rem" />}
          <TextField label="Name" value={name} onChange={setName} testId="ram-role-form-name" />
          <TextField label="Stable key" value={stableKey} onChange={setStableKey} testId="ram-role-form-stable-key" />
          <TextField label="Description" value={description} onChange={setDescription} testId="ram-role-form-description" />
          <TextField label="Scope" value={scope} onChange={setScope} testId="ram-role-form-scope" />
          <PermissionPicker catalog={catalog} selected={permissions} onChange={setPermissions} />
          <section className="rounded border border-border-base bg-bg-subtle p-3" data-testid="ram-role-form-permission-summary">
            <h3 className="text-sm font-semibold text-text-primary">Permission summary</h3>
            <PermissionSummary permissions={permissions} expanded />
          </section>
          {(create.isError || update.isError || newVersion.isError) && (
            <div className="rounded border border-danger/40 bg-danger/10 p-3 text-sm text-danger" role="alert" data-testid="ram-role-form-error">
              {((create.error ?? update.error ?? newVersion.error) as Error).message}
              {conflict && <button type="button" className="ml-2 rounded border border-danger/40 px-2 py-1 text-xs font-semibold" onClick={() => { void detail.refetch(); update.reset(); newVersion.reset(); onNotice({ tone: 'warning', message: 'RAM Role CAS conflict detected. Refreshed latest version.' }); }}>Refresh latest</button>}
            </div>
          )}
        </div>
        <footer className="flex justify-end gap-2 border-t border-border-base px-5 py-4">
          <button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm" onClick={onClose}>Cancel</button>
          {state.mode === 'edit' && <button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm font-semibold disabled:opacity-50" disabled={permissions.length === 0 || newVersion.isPending} onClick={saveVersion} data-testid="ram-role-form-create-version">Create version</button>}
          <button type="button" className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-semibold text-btn-primary-fg disabled:opacity-50" disabled={!name.trim() || permissions.length === 0 || create.isPending || update.isPending} onClick={saveMetadata} data-testid="ram-role-form-save">{state.mode === 'edit' ? 'Save metadata' : 'Create'}</button>
        </footer>
      </aside>
    </div>
  );
}

function PermissionCatalogSummary({ catalog, roles }: { catalog: AccessPermissionDefinition[]; roles: RAMRole[] }): React.ReactElement {
  const used = new Set(roles.flatMap((role) => role.permissions));
  const highRisk = catalog.filter((permission) => used.has(permission.key) && permission.risk === 'high').length;
  const mediumRisk = catalog.filter((permission) => used.has(permission.key) && permission.risk === 'medium').length;
  return (
    <aside className="rounded border border-border-base bg-bg-elevated p-4" data-testid="ram-role-permission-catalog-summary">
      <h2 className="text-sm font-semibold text-text-primary">Permission summary</h2>
      <div className="mt-3 grid grid-cols-3 gap-2">
        <Spec label="Used" value={used.size} />
        <Spec label="High" value={highRisk} />
        <Spec label="Medium" value={mediumRisk} />
      </div>
      <div className="mt-3 max-h-64 space-y-1 overflow-y-auto">
        {catalog.filter((permission) => used.has(permission.key)).map((permission) => (
          <div key={permission.key} className="flex items-center justify-between gap-2 rounded border border-border-base px-2 py-1.5 text-xs">
            <span className="font-mono text-text-secondary">{permission.key}</span>
            <AccessRiskBadge risk={permission.risk} />
          </div>
        ))}
      </div>
    </aside>
  );
}

function PermissionPicker({ catalog, selected, onChange }: { catalog: AccessPermissionDefinition[]; selected: string[]; onChange: (values: string[]) => void }): React.ReactElement {
  return (
    <section className="rounded border border-border-base p-3" data-testid="ram-role-form-permissions">
      <h3 className="text-xs font-semibold uppercase text-text-muted">Permissions</h3>
      <div className="mt-2 max-h-72 space-y-1 overflow-y-auto">
        {catalog.map((permission) => {
          const checked = selected.includes(permission.key);
          return (
            <button key={permission.key} type="button" aria-pressed={checked} className={`flex w-full items-center justify-between gap-2 rounded px-2 py-1.5 text-left text-xs ${checked ? 'bg-success/10 text-success' : 'text-text-secondary hover:bg-bg-subtle'}`} onClick={() => onChange(checked ? selected.filter((key) => key !== permission.key) : [...selected, permission.key].sort())}>
              <span className="font-mono">{permission.key}</span>
              <AccessRiskBadge risk={permission.risk} />
            </button>
          );
        })}
      </div>
    </section>
  );
}

function PermissionSummary({ permissions, expanded = false }: { permissions: string[]; expanded?: boolean }): React.ReactElement {
  const byDomain = permissions.reduce<Record<string, string[]>>((acc, permission) => {
    const domain = permission.split('.')[0] || 'other';
    acc[domain] = [...(acc[domain] ?? []), permission];
    return acc;
  }, {});
  if (permissions.length === 0) return <span className="text-xs text-text-muted">No permissions selected.</span>;
  return (
    <div className="mt-2 flex flex-wrap gap-1" data-testid="ram-role-permission-chips">
      {Object.entries(byDomain).map(([domain, values]) => (
        <span key={domain} className="rounded border border-border-base bg-bg-base px-1.5 py-0.5 font-mono text-[0.6875rem] text-text-secondary" title={values.join(', ')}>
          {expanded ? values.join(', ') : `${domain}.${values.length}`}
        </span>
      ))}
    </div>
  );
}

function Summary({ label, value, tone = 'default' }: { label: string; value: React.ReactNode; tone?: 'default' | 'warning' }): React.ReactElement {
  return (
    <div className="rounded border border-border-base bg-bg-elevated px-3 py-2">
      <div className="text-[0.6875rem] font-semibold uppercase text-text-muted">{label}</div>
      <div className={`mt-1 text-xl font-semibold ${tone === 'warning' ? 'text-warning' : 'text-text-primary'}`}>{value}</div>
    </div>
  );
}

function NoticeBanner({ notice, onClose }: { notice: NonNullable<Notice>; onClose: () => void }): React.ReactElement {
  const tone = notice.tone === 'success'
    ? 'border-success/30 bg-success/10 text-success'
    : notice.tone === 'warning'
      ? 'border-warning/40 bg-warning/10 text-warning'
      : 'border-danger/40 bg-danger/10 text-danger';
  return (
    <div className={`fixed right-4 top-4 z-50 flex max-w-lg items-start gap-3 rounded border px-4 py-3 text-sm shadow-2 ${tone}`} role="status" data-testid="ram-role-toast">
      <span className="font-medium">{notice.message}</span>
      <button type="button" aria-label="Dismiss notification" onClick={onClose}><IconClose className="h-3.5 w-3.5" /></button>
    </div>
  );
}

function Select({ label, value, options, onChange }: { label: string; value: string; options: string[]; onChange: (value: string) => void }): React.ReactElement {
  return (
    <label className="text-xs font-semibold uppercase text-text-muted">
      <span className="sr-only">{label}</span>
      <select className="w-full rounded border border-border-base bg-bg-base px-2 py-2 text-sm font-medium normal-case text-text-secondary" value={value} onChange={(event) => onChange(event.target.value)} data-testid={`ram-role-filter-${label.toLowerCase()}`}>
        {options.map((option) => <option key={option} value={option}>{option === 'all' ? `All ${label.toLowerCase()}` : option}</option>)}
      </select>
    </label>
  );
}

function TextField({ label, value, onChange, testId }: { label: string; value: string; onChange: (value: string) => void; testId: string }): React.ReactElement {
  return (
    <label className="block">
      <span className="text-xs font-semibold uppercase text-text-muted">{label}</span>
      <input className="mt-1 w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm text-text-primary" value={value} onChange={(event) => onChange(event.target.value)} data-testid={testId} />
    </label>
  );
}

function Spec({ label, value }: { label: string; value: React.ReactNode }): React.ReactElement {
  return <div className="rounded border border-border-base bg-bg-subtle p-2"><div className="text-[0.6875rem] font-semibold uppercase text-text-muted">{label}</div><div className="mt-1 font-mono text-sm text-text-primary">{value}</div></div>;
}

function Pagination({ page, totalPages, total, pageSize, onPage }: { page: number; totalPages: number; total: number; pageSize: number; onPage: (page: number) => void }): React.ReactElement {
  const start = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const end = Math.min(total, page * pageSize);
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border-base px-4 py-3 text-sm" data-testid="ram-role-pagination">
      <span className="text-text-muted">{start}-{end} of {total}</span>
      <div className="flex gap-2">
        <button type="button" className="rounded border border-border-base px-2.5 py-1 disabled:opacity-50" disabled={page <= 1} onClick={() => onPage(page - 1)} data-testid="ram-role-page-prev">Previous</button>
        <AccessMetaPill>Page {page} / {totalPages}</AccessMetaPill>
        <button type="button" className="rounded border border-border-base px-2.5 py-1 disabled:opacity-50" disabled={page >= totalPages} onClick={() => onPage(page + 1)} data-testid="ram-role-page-next">Next</button>
      </div>
    </div>
  );
}

function ramRoleReferences(entries: MappingEntry[]): Map<string, Array<{ team: TeamView; role: string }>> {
  const refs = new Map<string, Array<{ team: TeamView; role: string }>>();
  for (const entry of entries) {
    for (const id of entry.query.data?.ram_role_ids ?? []) {
      refs.set(id, [...(refs.get(id) ?? []), { team: entry.team, role: entry.role }]);
    }
  }
  return refs;
}

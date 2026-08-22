import type React from 'react';
import { useEffect, useMemo, useState } from 'react';
import { ApiError } from '@/api/client';
import {
  type AccessPermissionDefinition,
  type AccessResourceKind,
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
  useAllTeamMembers,
  useAllTeamRoleRAMMappings,
  useReplaceTeamRoleRAMMapping,
  useTeams,
} from '@/api/teams';
import { AccessMetaPill, AccessRiskBadge } from '@/components/access/kit';
import { ConfirmModal } from '@/components/ConfirmModal';
import { EmptyState } from '@/components/EmptyState';
import { IconClose, IconSearch } from '@/components/icons';
import { Skeleton } from '@/components/Skeleton';

type RAMKindFilter = 'all' | RAMRole['kind'];
type RefEntry = {
  team: TeamView;
  teamRole: string;
  mapping?: TeamRAMRoleMapping;
};
type MappingEntry = {
  team: TeamView;
  role: string;
  query: { data?: TeamRAMRoleMapping; isLoading: boolean; isError: boolean; error: unknown; refetch: () => unknown };
};
type Notice = { tone: 'success' | 'warning' | 'danger'; message: string } | null;
type DrawerState = { mode: 'create' | 'edit' | 'duplicate'; roleId?: string } | null;

const PAGE_SIZE = 8;
const RISK_OPTIONS: Array<AccessRisk | 'all'> = ['all', 'high', 'medium', 'low'];

export default function RAMRoles(): React.ReactElement {
  const overview = useAccessOverview();
  const ramRoles = useRAMRoles();
  const teams = useTeams();
  const memberEntries = useAllTeamMembers(teams.data ?? []);
  const rawMappings = useAllTeamRoleRAMMappings(teams.data ?? []) as MappingEntry[];
  const [query, setQuery] = useState('');
  const [kind, setKind] = useState<RAMKindFilter>('all');
  const [risk, setRisk] = useState<AccessRisk | 'all'>('all');
  const [scope, setScope] = useState('all');
  const [page, setPage] = useState(1);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [drawer, setDrawer] = useState<DrawerState>(null);
  const [notice, setNotice] = useState<Notice>(null);

  const roles = ramRoles.data?.roles ?? [];
  const refsByRole = useMemo(() => ramRoleReferences(rawMappings), [rawMappings]);
  const scopes = useMemo(() => Array.from(new Set(roles.map((role) => role.scope || 'mixed'))).sort(), [roles]);
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return roles.filter((role) => {
      const refs = refsByRole.get(role.id) ?? [];
      const haystack = [
        role.name,
        role.id,
        role.stable_key,
        role.description,
        role.scope,
        role.permissions.join(' '),
        refs.map((ref) => `${ref.team.name} ${ref.teamRole}`).join(' '),
      ].join(' ').toLowerCase();
      return (!q || haystack.includes(q))
        && (kind === 'all' || role.kind === kind)
        && (risk === 'all' || role.risk === risk)
        && (scope === 'all' || role.scope === scope);
    });
  }, [kind, query, refsByRole, risk, roles, scope]);

  useEffect(() => setPage(1), [query, kind, risk, scope]);
  useEffect(() => {
    if (!selectedId && roles.length > 0) setSelectedId(roles[0].id);
  }, [roles, selectedId]);
  useEffect(() => {
    if (selectedId && roles.length > 0 && !roles.some((role) => role.id === selectedId)) setSelectedId(roles[0].id);
  }, [roles, selectedId]);

  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const safePage = Math.min(page, pageCount);
  const visible = filtered.slice((safePage - 1) * PAGE_SIZE, safePage * PAGE_SIZE);
  const selectedRole = roles.find((role) => role.id === selectedId) ?? visible[0] ?? roles[0];
  const totalPermissions = new Set(roles.flatMap((role) => role.permissions)).size;
  const referencedRoles = roles.filter((role) => (refsByRole.get(role.id) ?? []).length > 0).length;
  const affectedMembers = useMemo(() => {
    const byTeamRole = new Map<string, number>();
    for (const { team, query: memberQuery } of memberEntries) {
      for (const role of team.roles) {
        byTeamRole.set(`${team.id}:${role.role}`, memberQuery.data?.filter((member) =>
          (member.roles?.length ? member.roles : [member.role]).includes(role.role),
        ).length ?? role.count ?? 0);
      }
    }
    let sum = 0;
    for (const refs of refsByRole.values()) {
      for (const ref of refs) sum += byTeamRole.get(`${ref.team.id}:${ref.teamRole}`) ?? 0;
    }
    return sum;
  }, [memberEntries, refsByRole]);

  return (
    <section className="space-y-4" data-testid="page-RAMRoles">
      {notice && <NoticeBanner notice={notice} onClose={() => setNotice(null)} />}
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs font-semibold uppercase text-text-muted">Teams / RAM Roles</p>
          <h1 className="mt-1 font-heading text-2xl font-semibold text-text-primary">RAM Roles</h1>
          <p className="mt-1 max-w-3xl text-sm text-text-secondary">
            Manage permission packages independently from Team Role runtime configuration and mappings.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm font-semibold text-text-primary hover:bg-bg-subtle" data-testid="ram-roles-refresh" onClick={() => {
            void ramRoles.refetch();
            void overview.refetch();
            for (const entry of rawMappings) void entry.query.refetch();
            setNotice({ tone: 'success', message: 'Refreshed RAM Roles, permissions, and Team Role references.' });
          }}>
            Refresh
          </button>
          <button type="button" className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-semibold text-btn-primary-fg" data-testid="ram-roles-create" onClick={() => setDrawer({ mode: 'create' })}>
            New RAM Role
          </button>
        </div>
      </header>

      <div className="grid gap-3 md:grid-cols-4">
        <Summary label="Active RAM Roles" value={roles.length} />
        <Summary label="Custom Roles" value={roles.filter((role) => role.kind === 'custom').length} />
        <Summary label="Referenced Roles" value={referencedRoles} />
        <Summary label="Permissions Covered" value={totalPermissions} detail={`${affectedMembers} mapped members`} />
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,0.98fr)_minmax(28rem,1.2fr)]">
        <section className="rounded border border-border-base bg-bg-elevated" data-testid="ram-roles-list">
          <div className="border-b border-border-base px-4 py-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <h2 className="text-sm font-semibold text-text-primary">Role catalog</h2>
                <p className="mt-1 text-xs text-text-muted">{filtered.length} matching roles · page {safePage} of {pageCount}</p>
              </div>
              {ramRoles.isLoading && <AccessMetaPill>loading</AccessMetaPill>}
            </div>
            <div className="mt-3 grid gap-2 md:grid-cols-[minmax(12rem,1fr)_8rem_8rem_9rem]">
              <label className="relative block">
                <span className="sr-only">Search RAM Roles</span>
                <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-muted"><IconSearch /></span>
                <input className="w-full rounded border border-border-base bg-bg-base py-2 pl-9 pr-3 text-sm" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search name, key, permission, Team Role" data-testid="ram-roles-search" />
              </label>
              <Select label="Kind" value={kind} onChange={(value) => setKind(value as RAMKindFilter)} options={['all', 'system', 'custom']} testId="ram-roles-kind-filter" />
              <Select label="Risk" value={risk} onChange={(value) => setRisk(value as AccessRisk | 'all')} options={RISK_OPTIONS} testId="ram-roles-risk-filter" />
              <Select label="Scope" value={scope} onChange={setScope} options={['all', ...scopes]} testId="ram-roles-scope-filter" />
            </div>
          </div>
          {ramRoles.isLoading && <div className="p-4"><Skeleton height="18rem" /></div>}
          {ramRoles.isError && <p className="m-4 rounded border border-danger/30 bg-danger/10 p-3 text-sm text-danger" role="alert">{(ramRoles.error as Error).message}</p>}
          {!ramRoles.isLoading && !ramRoles.isError && filtered.length === 0 && <EmptyState title="No RAM Roles match" body="Adjust search or filters to see permission packages." testId="ram-roles-empty" />}
          {!ramRoles.isLoading && !ramRoles.isError && filtered.length > 0 && (
            <>
              <div className="divide-y divide-border-base">
                {visible.map((role) => (
                  <button
                    key={role.id}
                    type="button"
                    className={`block w-full px-4 py-3 text-left hover:bg-bg-subtle ${selectedRole?.id === role.id ? 'bg-bg-subtle' : ''}`}
                    data-testid={`ram-role-row-${role.id}`}
                    onClick={() => setSelectedId(role.id)}
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="font-semibold text-text-primary">{role.name}</span>
                          <AccessMetaPill>{role.kind}</AccessMetaPill>
                          <AccessMetaPill>{role.scope}</AccessMetaPill>
                        </div>
                        <div className="mt-1 font-mono text-xs text-text-muted">{role.stable_key || role.id} · v{role.version}</div>
                        <p className="mt-1 line-clamp-2 text-xs text-text-secondary">{role.description}</p>
                      </div>
                      <div className="shrink-0 text-right">
                        <AccessRiskBadge risk={role.risk} />
                        <div className="mt-2 text-xs text-text-muted">{role.permissions.length} permissions</div>
                        <div className="text-xs text-text-muted">{refsByRole.get(role.id)?.length ?? 0} refs</div>
                      </div>
                    </div>
                  </button>
                ))}
              </div>
              <div className="flex items-center justify-between border-t border-border-base px-4 py-3 text-sm">
                <button type="button" className="rounded border border-border-base px-2.5 py-1 disabled:opacity-50" disabled={safePage <= 1} onClick={() => setPage((value) => Math.max(1, value - 1))} data-testid="ram-roles-prev-page">Previous</button>
                <span className="text-xs text-text-muted">{(safePage - 1) * PAGE_SIZE + 1}-{Math.min(safePage * PAGE_SIZE, filtered.length)} of {filtered.length}</span>
                <button type="button" className="rounded border border-border-base px-2.5 py-1 disabled:opacity-50" disabled={safePage >= pageCount} onClick={() => setPage((value) => Math.min(pageCount, value + 1))} data-testid="ram-roles-next-page">Next</button>
              </div>
            </>
          )}
        </section>

        <RAMRoleDetailPanel
          role={selectedRole}
          catalog={overview.data?.catalog ?? []}
          detailRefs={selectedRole ? refsByRole.get(selectedRole.id) ?? [] : []}
          onEdit={() => selectedRole && setDrawer({ mode: 'edit', roleId: selectedRole.id })}
          onDuplicate={() => selectedRole && setDrawer({ mode: 'duplicate', roleId: selectedRole.id })}
          onSelectedDeleted={() => setSelectedId(null)}
          onNotice={setNotice}
          allRoles={roles}
        />
      </div>

      {drawer && (
        <RAMRoleDrawer
          mode={drawer.mode}
          roleId={drawer.roleId}
          sourceRole={roles.find((role) => role.id === drawer.roleId)}
          catalog={overview.data?.catalog ?? []}
          refs={drawer.roleId ? refsByRole.get(drawer.roleId) ?? [] : []}
          onClose={() => setDrawer(null)}
          onNotice={setNotice}
        />
      )}
    </section>
  );
}

function RAMRoleDetailPanel({
  role,
  catalog,
  detailRefs,
  onEdit,
  onDuplicate,
  onSelectedDeleted,
  onNotice,
  allRoles,
}: {
  role?: RAMRole;
  catalog: AccessPermissionDefinition[];
  detailRefs: RefEntry[];
  onEdit: () => void;
  onDuplicate: () => void;
  onSelectedDeleted: () => void;
  onNotice: (notice: Notice) => void;
  allRoles: RAMRole[];
}): React.ReactElement {
  const detail = useRAMRole(role?.id ?? null);
  const current = detail.data?.latest ?? role;
  const versions = detail.data?.versions ?? (current ? [current] : []);
  const refs = mergeRefs(detail.data, detailRefs);

  if (!current) {
    return (
      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="ram-role-detail">
        <EmptyState title="Select a RAM Role" body="Choose a role from the catalog to inspect permissions, references, and history." />
      </section>
    );
  }

  return (
    <section className="rounded border border-border-base bg-bg-elevated" data-testid="ram-role-detail">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border-base px-4 py-3">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-lg font-semibold text-text-primary">{current.name}</h2>
            <AccessRiskBadge risk={current.risk} />
            <AccessMetaPill>{current.kind}</AccessMetaPill>
            <AccessMetaPill>{current.scope}</AccessMetaPill>
          </div>
          <p className="mt-1 font-mono text-xs text-text-muted">{current.stable_key || current.id} · latest v{current.version}</p>
          <p className="mt-2 max-w-2xl text-sm text-text-secondary">{current.description}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm font-semibold" onClick={onEdit} data-testid="ram-role-edit">Edit</button>
          <button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm font-semibold" onClick={onDuplicate} data-testid="ram-role-duplicate">Duplicate</button>
          <RAMRoleDeleteButton role={current} refs={refs} allRoles={allRoles} onDeleted={onSelectedDeleted} onNotice={onNotice} />
        </div>
      </div>
      <div className="grid gap-4 p-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <div className="space-y-4">
          <section className="rounded border border-border-base bg-bg-base p-3" data-testid="ram-role-permission-summary">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h3 className="text-sm font-semibold text-text-primary">Permission summary</h3>
              <AccessMetaPill>{current.permissions.length} permissions</AccessMetaPill>
            </div>
            <div className="mt-3 grid gap-2 sm:grid-cols-3">
              <Spec label="High risk" value={countRisk(current.permissions, catalog, 'high')} />
              <Spec label="Medium risk" value={countRisk(current.permissions, catalog, 'medium')} />
              <Spec label="Low risk" value={countRisk(current.permissions, catalog, 'low')} />
            </div>
            <div className="mt-3 flex flex-wrap gap-1.5">
              {current.permissions.map((permission) => {
                const definition = catalog.find((item) => item.key === permission);
                return (
                  <span key={permission} className="rounded border border-border-base bg-bg-subtle px-2 py-1 font-mono text-[0.6875rem] text-text-secondary" title={definition?.description}>
                    {permission}
                  </span>
                );
              })}
            </div>
          </section>
          <section className="rounded border border-border-base bg-bg-base p-3" data-testid="ram-role-team-references">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h3 className="text-sm font-semibold text-text-primary">Team Role references</h3>
              <AccessMetaPill>{refs.length} refs</AccessMetaPill>
            </div>
            {refs.length === 0 ? (
              <p className="mt-2 text-sm text-text-muted">No Team Role mappings reference this RAM Role.</p>
            ) : (
              <div className="mt-3 divide-y divide-border-base">
                {refs.map((ref) => (
                  <div key={`${ref.team.id}:${ref.teamRole}`} className="flex flex-wrap items-center justify-between gap-2 py-2 text-sm">
                    <div>
                      <div className="font-semibold text-text-primary">{ref.team.name}</div>
                      <div className="font-mono text-xs text-text-muted">{ref.team.id} / {ref.teamRole}</div>
                    </div>
                    <AccessMetaPill>mapping v{ref.mapping?.version ?? 'read pending'}</AccessMetaPill>
                  </div>
                ))}
              </div>
            )}
          </section>
        </div>
        <aside className="rounded border border-border-base bg-bg-base p-3" data-testid="ram-role-version-history">
          <h3 className="text-sm font-semibold text-text-primary">Version history</h3>
          {detail.isLoading && <div className="mt-3"><Skeleton height="8rem" /></div>}
          <div className="mt-3 space-y-2">
            {versions.map((version) => (
              <div key={`${version.id}:${version.version}`} className="rounded border border-border-base bg-bg-subtle p-2">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-mono text-sm font-semibold text-text-primary">v{version.version}</span>
                  <AccessRiskBadge risk={version.risk} />
                </div>
                <div className="mt-1 text-xs text-text-muted">{version.permissions.length} permissions · {version.updated_at ?? version.created_at ?? 'server history'}</div>
              </div>
            ))}
          </div>
        </aside>
      </div>
    </section>
  );
}

function RAMRoleDeleteButton({ role, refs, allRoles, onDeleted, onNotice }: { role: RAMRole; refs: RefEntry[]; allRoles: RAMRole[]; onDeleted: () => void; onNotice: (notice: Notice) => void }): React.ReactElement {
  const revoke = useRAMRoleRevoke();
  const deleteRole = useRAMRoleDelete();
  const replaceMapping = useReplaceTeamRoleRAMMapping();
  const [confirming, setConfirming] = useState(false);
  const [replacement, setReplacement] = useState('');
  const alternatives = allRoles.filter((item) => item.id !== role.id && !item.revoked_at);
  const blocked = refs.length > 0 && !replacement;
  const busy = revoke.isPending || deleteRole.isPending || replaceMapping.isPending;

  const remove = async () => {
    try {
      if (refs.length > 0) {
        if (!replacement) {
          onNotice({ tone: 'warning', message: `${role.name} is referenced by ${refs.length} Team Roles. Choose a migration target first.` });
          return;
        }
        for (const ref of refs) {
          if (!ref.mapping) throw new Error(`Mapping for ${ref.team.name}/${ref.teamRole} has not loaded.`);
          await replaceMapping.mutateAsync({
            team_id: ref.team.id,
            role: ref.teamRole,
            ram_role_ids: sorted(ref.mapping.ram_role_ids.map((id) => id === role.id ? replacement : id)),
            expected_version: ref.mapping.version,
          });
        }
      }
      if (role.kind === 'custom') {
        await deleteRole.mutateAsync({ id: role.id, expected_latest_version: role.version, confirm_unreferenced: true, reason: 'RAM Roles page delete safeguard' });
        onNotice({ tone: 'success', message: `Deleted RAM Role ${role.name}.` });
      } else {
        await revoke.mutateAsync({ id: role.id, expected_latest_version: role.version, reason: 'RAM Roles page revoke safeguard' });
        onNotice({ tone: 'success', message: `Revoked RAM Role ${role.name}.` });
      }
      onDeleted();
      setConfirming(false);
    } catch (error) {
      onNotice({ tone: 'danger', message: (error as Error).message });
    }
  };

  return (
    <>
      <button type="button" className="rounded border border-danger/40 px-3 py-1.5 text-sm font-semibold text-danger" onClick={() => setConfirming(true)} data-testid="ram-role-delete">Delete</button>
      <ConfirmModal
        open={confirming}
        title="Delete RAM Role"
        message={<div className="space-y-3" data-testid="ram-role-delete-confirm">
          <p>{refs.length > 0 ? `${role.name} is referenced by ${refs.length} Team Roles. Delete is blocked until mappings are migrated.` : `Delete ${role.name}? This requires the latest version and writes an audit event.`}</p>
          {refs.length > 0 && (
            <label className="block">
              <span className="text-xs font-semibold uppercase text-text-muted">Migrate references to</span>
              <select className="mt-1 w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm" value={replacement} onChange={(event) => setReplacement(event.target.value)} data-testid="ram-role-delete-migration">
                <option value="">Choose replacement</option>
                {alternatives.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
              </select>
            </label>
          )}
          {(deleteRole.isError || revoke.isError || replaceMapping.isError) && <p className="text-sm text-danger">{((deleteRole.error ?? revoke.error ?? replaceMapping.error) as Error).message}</p>}
        </div>}
        confirmLabel={blocked ? 'Migration required' : refs.length > 0 ? 'Migrate and delete' : 'Delete'}
        danger
        busy={busy}
        confirmDisabled={blocked}
        onCancel={() => setConfirming(false)}
        onConfirm={() => void remove()}
      />
    </>
  );
}

function RAMRoleDrawer({ mode, roleId, sourceRole, catalog, refs, onClose, onNotice }: { mode: 'create' | 'edit' | 'duplicate'; roleId?: string; sourceRole?: RAMRole; catalog: AccessPermissionDefinition[]; refs: RefEntry[]; onClose: () => void; onNotice: (notice: Notice) => void }): React.ReactElement {
  const detail = useRAMRole(roleId ?? null);
  const create = useRAMRoleCreate();
  const update = useRAMRoleUpdate();
  const newVersion = useRAMRoleNewVersion();
  const base = detail.data?.latest ?? sourceRole;
  const [name, setName] = useState('');
  const [stableKey, setStableKey] = useState('');
  const [description, setDescription] = useState('');
  const [scope, setScope] = useState<AccessResourceKind | 'mixed' | string>('team');
  const [permissions, setPermissions] = useState<string[]>([]);
  const conflict = (update.error instanceof ApiError && update.error.status === 409) || (newVersion.error instanceof ApiError && newVersion.error.status === 409);

  useEffect(() => {
    if (mode !== 'create' && !base) return;
    setName(mode === 'duplicate' && base ? `${base.name} copy` : base?.name ?? '');
    setStableKey(mode === 'duplicate' && base ? `${base.stable_key || base.id}.copy` : base?.stable_key ?? '');
    setDescription(base?.description ?? '');
    setScope(base?.scope ?? 'team');
    setPermissions(base?.permissions ?? []);
  }, [base, mode]);

  if (mode !== 'create' && !base && detail.isLoading) {
    return <Drawer title="RAM Role" subtitle="Loading role detail" onClose={onClose} testId="ram-role-drawer"><Skeleton height="12rem" /></Drawer>;
  }

  const payload = { name, stable_key: stableKey, description, scope, permissions, expected_latest_version: base?.version };
  const save = () => {
    if (mode === 'create' || mode === 'duplicate') {
      create.mutate(payload, { onSuccess: (created) => { onNotice({ tone: 'success', message: `Created RAM Role ${created.name}.` }); onClose(); } });
      return;
    }
    if (!roleId) return;
    update.mutate({ id: roleId, payload }, { onSuccess: (saved) => { onNotice({ tone: 'success', message: `Saved RAM Role ${saved.name} metadata.` }); onClose(); } });
  };
  const createVersion = () => {
    if (!roleId || mode !== 'edit') return;
    newVersion.mutate({ id: roleId, payload }, { onSuccess: (saved) => { onNotice({ tone: 'success', message: `Created v${saved.latest.version} for ${saved.name}.` }); onClose(); } });
  };

  return (
    <Drawer title={mode === 'create' ? 'Create RAM Role' : mode === 'duplicate' ? 'Duplicate RAM Role' : 'Edit RAM Role'} subtitle={base ? `${base.stable_key || base.id} · latest v${base.version}` : 'New role'} onClose={onClose} testId="ram-role-drawer">
      <div className="space-y-4">
        <div className="grid gap-3 md:grid-cols-2">
          <TextField label="Name" value={name} onChange={setName} testId="ram-role-name" />
          <TextField label="Stable key" value={stableKey} onChange={setStableKey} testId="ram-role-stable-key" />
          <TextField label="Scope" value={scope} onChange={setScope} testId="ram-role-scope" />
          <label className="block">
            <span className="text-xs font-semibold uppercase text-text-muted">Risk preview</span>
            <div className="mt-1 rounded border border-border-base bg-bg-subtle px-2 py-1.5 text-sm"><AccessRiskBadge risk={highestRisk(permissions, catalog)} /></div>
          </label>
        </div>
        <TextArea label="Description" value={description} onChange={setDescription} testId="ram-role-description" />
        <PermissionPicker catalog={catalog} selected={permissions} onChange={setPermissions} />
        <section className="rounded border border-border-base bg-bg-subtle p-3" data-testid="ram-role-form-summary">
          <h3 className="text-sm font-semibold text-text-primary">Permission summary</h3>
          <div className="mt-2 grid gap-2 text-sm sm:grid-cols-4">
            <Spec label="Selected" value={permissions.length} />
            <Spec label="High" value={countRisk(permissions, catalog, 'high')} />
            <Spec label="Medium" value={countRisk(permissions, catalog, 'medium')} />
            <Spec label="Low" value={countRisk(permissions, catalog, 'low')} />
          </div>
          <p className="mt-2 text-xs text-text-secondary">{refs.length > 0 ? `Referenced by ${refs.map((ref) => `${ref.team.name}/${ref.teamRole}`).join(', ')}` : 'No Team Role references.'}</p>
        </section>
        {(create.isError || update.isError || newVersion.isError) && (
          <div className="rounded border border-danger/40 bg-danger/10 p-3 text-sm text-danger" role="alert" data-testid="ram-role-error">
            {((create.error ?? update.error ?? newVersion.error) as Error).message}
            {conflict && <button type="button" className="ml-2 rounded border border-danger/40 px-2 py-1 text-xs font-semibold" onClick={() => { void detail.refetch(); update.reset(); newVersion.reset(); onNotice({ tone: 'warning', message: 'RAM Role CAS conflict detected. Refreshed latest version; review before saving.' }); }}>Refresh latest</button>}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm" onClick={onClose}>Cancel</button>
          {mode === 'edit' && <button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm font-semibold disabled:opacity-50" disabled={!roleId || permissions.length === 0 || newVersion.isPending} onClick={createVersion} data-testid="ram-role-create-version">Create version</button>}
          <button type="button" className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-semibold text-btn-primary-fg disabled:opacity-50" disabled={!name.trim() || permissions.length === 0 || create.isPending || update.isPending} onClick={save} data-testid="ram-role-save">{mode === 'edit' ? 'Save metadata' : 'Create'}</button>
        </div>
      </div>
    </Drawer>
  );
}

function PermissionPicker({ catalog, selected, onChange }: { catalog: AccessPermissionDefinition[]; selected: string[]; onChange: (values: string[]) => void }): React.ReactElement {
  const [q, setQ] = useState('');
  const [risk, setRisk] = useState<AccessRisk | 'all'>('all');
  const filtered = catalog.filter((permission) => {
    const text = `${permission.key} ${permission.label} ${permission.description}`.toLowerCase();
    return (!q.trim() || text.includes(q.trim().toLowerCase())) && (risk === 'all' || permission.risk === risk);
  });
  return (
    <div className="rounded border border-border-base p-3" data-testid="ram-role-permissions">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-xs font-semibold uppercase text-text-muted">Permissions</h3>
        <Select label="Permission risk" value={risk} onChange={(value) => setRisk(value as AccessRisk | 'all')} options={RISK_OPTIONS} testId="ram-role-permission-risk" />
      </div>
      <label className="relative mt-2 block">
        <span className="sr-only">Search permissions</span>
        <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-muted"><IconSearch /></span>
        <input className="w-full rounded border border-border-base bg-bg-base py-2 pl-9 pr-3 text-sm" value={q} onChange={(event) => setQ(event.target.value)} placeholder="Search permission key or description" data-testid="ram-role-permission-search" />
      </label>
      <div className="mt-2 max-h-72 space-y-1 overflow-y-auto">
        {filtered.map((permission) => {
          const checked = selected.includes(permission.key);
          return (
            <button key={permission.key} type="button" aria-pressed={checked} className={`flex w-full items-center justify-between gap-2 rounded px-2 py-1.5 text-left text-xs ${checked ? 'bg-success/10 text-success' : 'hover:bg-bg-subtle text-text-secondary'}`} onClick={() => onChange(checked ? selected.filter((key) => key !== permission.key) : [...selected, permission.key].sort())} data-testid={`ram-role-permission-${permission.key}`}>
              <span>
                <span className="block font-mono">{permission.key}</span>
                <span className="block text-[0.6875rem] opacity-75">{permission.description}</span>
              </span>
              <AccessRiskBadge risk={permission.risk} />
            </button>
          );
        })}
      </div>
    </div>
  );
}

function Drawer({ title, subtitle, children, onClose, testId }: { title: string; subtitle: string; children: React.ReactNode; onClose: () => void; testId: string }): React.ReactElement {
  return (
    <div className="fixed inset-0 z-50 bg-black/30" data-testid={`${testId}-backdrop`}>
      <aside className="ml-auto flex h-full w-full max-w-2xl flex-col bg-bg-elevated shadow-2" data-testid={testId}>
        <header className="flex items-start justify-between gap-3 border-b border-border-base px-5 py-4">
          <div>
            <h2 className="text-lg font-semibold text-text-primary">{title}</h2>
            <p className="mt-1 text-sm text-text-muted">{subtitle}</p>
          </div>
          <button type="button" aria-label="Close drawer" className="rounded border border-border-base p-2" onClick={onClose}><IconClose className="h-4 w-4" /></button>
        </header>
        <div className="flex-1 overflow-y-auto p-5">{children}</div>
      </aside>
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
    <div className={`fixed right-4 top-4 z-50 flex max-w-lg items-start gap-3 rounded border px-4 py-3 text-sm shadow-2 ${tone}`} role="status" data-testid="ram-role-notice">
      <span className="font-medium">{notice.message}</span>
      <button type="button" aria-label="Dismiss notification" onClick={onClose}><IconClose className="h-3.5 w-3.5" /></button>
    </div>
  );
}

function Summary({ label, value, detail }: { label: string; value: React.ReactNode; detail?: string }): React.ReactElement {
  return (
    <div className="rounded border border-border-base bg-bg-elevated px-3 py-2">
      <div className="text-[0.6875rem] font-semibold uppercase text-text-muted">{label}</div>
      <div className="mt-1 text-xl font-semibold text-text-primary">{value}</div>
      {detail && <div className="mt-0.5 text-xs text-text-muted">{detail}</div>}
    </div>
  );
}

function Select({ label, value, onChange, options, testId }: { label: string; value: string; onChange: (value: string) => void; options: string[]; testId: string }): React.ReactElement {
  return (
    <label className="block">
      <span className="sr-only">{label}</span>
      <select className="w-full rounded border border-border-base bg-bg-base px-2 py-2 text-sm text-text-primary" value={value} onChange={(event) => onChange(event.target.value)} data-testid={testId}>
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

function TextArea({ label, value, onChange, testId }: { label: string; value: string; onChange: (value: string) => void; testId: string }): React.ReactElement {
  return (
    <label className="block">
      <span className="text-xs font-semibold uppercase text-text-muted">{label}</span>
      <textarea className="mt-1 min-h-20 w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm text-text-primary" value={value} onChange={(event) => onChange(event.target.value)} data-testid={testId} />
    </label>
  );
}

function Spec({ label, value }: { label: string; value: React.ReactNode }): React.ReactElement {
  return <div><div className="text-[0.6875rem] font-semibold uppercase text-text-muted">{label}</div><div className="mt-0.5 font-mono text-sm text-text-primary">{value}</div></div>;
}

function ramRoleReferences(entries: MappingEntry[]): Map<string, RefEntry[]> {
  const refs = new Map<string, RefEntry[]>();
  for (const entry of entries) {
    for (const id of entry.query.data?.ram_role_ids ?? []) {
      refs.set(id, [...(refs.get(id) ?? []), { team: entry.team, teamRole: entry.role, mapping: entry.query.data }]);
    }
  }
  return refs;
}

function mergeRefs(detail: RAMRoleDetail | undefined, localRefs: RefEntry[]): RefEntry[] {
  if (!detail?.references?.length) return localRefs;
  const byKey = new Map(localRefs.map((ref) => [`${ref.team.id}:${ref.teamRole}`, ref]));
  for (const ref of detail.references) {
    const key = `${ref.team_id}:${ref.team_role}`;
    if (!byKey.has(key)) {
      byKey.set(key, {
        team: { id: ref.team_id, name: ref.team_name, org_id: '', description: '', roles: [], version: 0, glyph: ref.team_name.slice(0, 2).toUpperCase(), status: 'active', members_count: 0, projects_count: 0, created: '' },
        teamRole: ref.team_role,
      });
    }
  }
  return [...byKey.values()];
}

function countRisk(permissions: string[], catalog: AccessPermissionDefinition[], risk: AccessRisk): number {
  return permissions.filter((key) => catalog.find((permission) => permission.key === key)?.risk === risk).length;
}

function highestRisk(permissions: string[], catalog: AccessPermissionDefinition[]): AccessRisk {
  if (countRisk(permissions, catalog, 'high') > 0) return 'high';
  if (countRisk(permissions, catalog, 'medium') > 0) return 'medium';
  return 'low';
}

function sorted(values: string[]): string[] {
  return Array.from(new Set(values)).sort();
}

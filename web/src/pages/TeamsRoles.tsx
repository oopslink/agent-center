import type React from 'react';
import { useEffect, useMemo, useState } from 'react';
import { ApiError } from '@/api/client';
import {
  type AccessPermissionDefinition,
  type RAMRole,
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
  type RoleView,
  type TeamRAMRoleMapping,
  type TeamView,
  useAllTeamMembers,
  useAllTeamRoleRAMMappings,
  usePreviewTeamRoleRAMMapping,
  useReplaceTeamRoleRAMMapping,
  useTeams,
} from '@/api/teams';
import { AccessMetaPill, AccessRiskBadge } from '@/components/access/kit';
import { ConfirmModal } from '@/components/ConfirmModal';
import { EntityMultiSelect } from '@/components/EntityMultiSelect';
import { EmptyState } from '@/components/EmptyState';
import { IconClose, IconSearch } from '@/components/icons';
import { Skeleton } from '@/components/Skeleton';
import { Glyph, RoleBar, RoleLegend, roleColorChip } from '@/components/teams/teamsUi';

type MappingEntry = {
  team: TeamView;
  role: string;
  roleView: RoleView;
  query: { data?: TeamRAMRoleMapping; isLoading: boolean; isError: boolean; error: unknown; refetch: () => unknown };
};

type Notice = { tone: 'success' | 'warning' | 'danger'; message: string } | null;
type DrawerState =
  | { kind: 'mapping'; entry: MappingEntry }
  | { kind: 'role'; mode: 'create' | 'edit' | 'duplicate'; roleId?: string }
  | null;

export default function TeamsRoles(): React.ReactElement {
  const teams = useTeams();
  const ramRoles = useRAMRoles();
  const overview = useAccessOverview();
  const memberEntries = useAllTeamMembers(teams.data ?? []);
  const rawMappings = useAllTeamRoleRAMMappings(teams.data ?? []);
  const [teamFilter, setTeamFilter] = useState('all');
  const [query, setQuery] = useState('');
  const [drawer, setDrawer] = useState<DrawerState>(null);
  const [notice, setNotice] = useState<Notice>(null);

  const mappings = useMemo<MappingEntry[]>(() => rawMappings.map((entry) => ({
    ...entry,
    roleView: entry.team.roles.find((role) => role.role === entry.role) ?? entry.team.roles[0],
  })).filter((entry) => entry.roleView), [rawMappings]);
  const roleRefs = useMemo(() => ramRoleReferences(mappings), [mappings]);
  const roleRows = useMemo(() => teams.data?.flatMap((team) => team.roles.map((role) => ({
    team,
    role,
    members: memberEntries.find((entry) => entry.team.id === team.id)?.query.data?.filter((member) =>
      (member.roles?.length ? member.roles : [member.role]).includes(role.role),
    ).length ?? role.count ?? 0,
  }))) ?? [], [teams.data, memberEntries]);
  const filteredMappings = useMemo(() => {
    const q = query.trim().toLowerCase();
    return mappings.filter((entry) => {
      const teamMatch = teamFilter === 'all' || entry.team.id === teamFilter;
      const text = `${entry.team.name} ${entry.team.id} ${entry.role} ${entry.roleView.cli} ${entry.roleView.model}`.toLowerCase();
      return teamMatch && (!q || text.includes(q));
    });
  }, [mappings, query, teamFilter]);

  const customRoleCount = ramRoles.data?.roles.filter((role) => role.kind === 'custom').length ?? 0;
  const mappedCount = mappings.filter((entry) => (entry.query.data?.ram_role_ids ?? []).length > 0).length;
  const affectedMembers = roleRows.reduce((sum, row) => sum + row.members, 0);
  const activeMappingEntry = drawer?.kind === 'mapping'
    ? mappings.find((entry) => entry.team.id === drawer.entry.team.id && entry.role === drawer.entry.role) ?? drawer.entry
    : null;

  return (
    <section className="space-y-4" data-testid="page-TeamsRoles">
      {notice && <NoticeBanner notice={notice} onClose={() => setNotice(null)} />}
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs font-semibold uppercase text-text-muted">Teams / Platform Team / Roles / Developer</p>
          <h1 className="mt-1 font-heading text-2xl font-semibold text-text-primary">Team Roles</h1>
          <p className="mt-1 max-w-3xl text-sm text-text-secondary">
            Manage work configuration and RAM Role mappings from the Team IA. Canonical mockup ac://files/01M0HRMZEV7XS8A3MNGG64ZZW1 · SHA256 80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm font-semibold text-text-primary hover:bg-bg-subtle" data-testid="team-roles-refresh" onClick={() => {
            void teams.refetch();
            void ramRoles.refetch();
            for (const entry of mappings) void entry.query.refetch();
            setNotice({ tone: 'success', message: 'Refreshed Team Roles and RAM mappings from the server.' });
          }}>
            Refresh
          </button>
          <button type="button" className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-semibold text-btn-primary-fg" data-testid="team-roles-create-ram-role" onClick={() => setDrawer({ kind: 'role', mode: 'create' })}>
            New RAM Role
          </button>
        </div>
      </header>

      <div className="grid gap-3 md:grid-cols-4">
        <Summary label="Declared Team Roles" value={roleRows.length} />
        <Summary label="Mapped Role Slots" value={`${mappedCount}/${mappings.length || 0}`} />
        <Summary label="Affected Members" value={affectedMembers} />
        <Summary label="Custom RAM Roles" value={customRoleCount} />
      </div>

      <div className="grid gap-4 2xl:grid-cols-[minmax(0,0.92fr)_minmax(0,1.3fr)]">
        <section className="rounded border border-border-base bg-bg-elevated" data-testid="team-role-list">
          <div className="flex items-center justify-between gap-3 border-b border-border-base px-4 py-3">
            <div>
              <h2 className="text-sm font-semibold text-text-primary">Role list and work config</h2>
              <p className="mt-1 text-xs text-text-muted">Runtime profile, concurrency, capability tags, and inherited RAM keys.</p>
            </div>
            {teams.isLoading && <AccessMetaPill>loading</AccessMetaPill>}
          </div>
          {teams.isLoading && <div className="p-4"><Skeleton height="16rem" /></div>}
          {teams.isSuccess && roleRows.length === 0 && <EmptyState title="No Team Roles" body="Create a team with roles before mapping RAM Roles." testId="team-roles-empty" />}
          {teams.isSuccess && roleRows.length > 0 && (
            <div className="divide-y divide-border-base">
              {roleRows.map(({ team, role, members }) => (
                <button
                  key={`${team.id}:${role.role}`}
                  type="button"
                  className="block w-full px-4 py-3 text-left hover:bg-bg-subtle"
                  data-testid={`team-role-detail-${team.id}-${role.role}`}
                  onClick={() => {
                    const entry = mappings.find((item) => item.team.id === team.id && item.role === role.role);
                    if (entry) setDrawer({ kind: 'mapping', entry });
                  }}
                >
                  <div className="flex items-start gap-3">
                    <Glyph text={team.glyph} size="sm" />
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-semibold text-text-primary">{team.name}</span>
                        <span className="rounded border border-border-base bg-bg-subtle px-2 py-0.5 text-xs font-semibold" style={roleColorChip(role.role)}>{role.role}</span>
                        <AccessMetaPill>{members} members</AccessMetaPill>
                      </div>
                      <div className="mt-1 font-mono text-xs text-text-muted">{role.cli} · {role.model} · max {role.max_concurrency}</div>
                      <RoleBar roles={[role]} className="mt-2 max-w-xs" showCount={false} />
                      <RoleLegend roles={[role]} showCount={false} />
                      <div className="mt-2 flex flex-wrap gap-1">
                        {role.capability_tags.slice(0, 5).map((tag) => <span key={tag} className="rounded border border-border-base bg-bg-base px-1.5 py-0.5 text-[0.65rem] text-text-secondary">{tag}</span>)}
                        {(role.ram_role_keys ?? []).map((key) => <span key={key} className="rounded border border-success/30 bg-success/10 px-1.5 py-0.5 font-mono text-[0.65rem] text-success">{key}</span>)}
                      </div>
                    </div>
                  </div>
                </button>
              ))}
            </div>
          )}
        </section>

        <section className="rounded border border-border-base bg-bg-elevated" data-testid="team-role-ram-mappings">
          <div className="border-b border-border-base px-4 py-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h2 className="text-sm font-semibold text-text-primary">RAM Role mapping table</h2>
                <p className="mt-1 text-xs text-text-muted">Preview immediate impact before applying optimistic CAS writes.</p>
              </div>
              <select className="rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm" value={teamFilter} onChange={(event) => setTeamFilter(event.target.value)} data-testid="team-role-filter-team">
                <option value="all">All teams</option>
                {(teams.data ?? []).map((team) => <option key={team.id} value={team.id}>{team.name}</option>)}
              </select>
            </div>
            <label className="relative mt-3 block">
              <span className="sr-only">Search Team Role mappings</span>
              <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-muted"><IconSearch /></span>
              <input className="w-full rounded border border-border-base bg-bg-base py-2 pl-9 pr-3 text-sm" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search team, role, cli, model" data-testid="team-role-mapping-search" />
            </label>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full min-w-full table-fixed text-left text-sm">
              <thead className="border-b border-border-base text-[0.6875rem] uppercase text-text-muted">
                <tr>
                  <th className="w-[24%] px-4 py-2 font-semibold">Team Role</th>
                  <th className="w-[21%] px-4 py-2 font-semibold">Work config</th>
                  <th className="w-[25%] px-4 py-2 font-semibold">RAM Roles</th>
                  <th className="w-[20%] px-4 py-2 font-semibold">Audit</th>
                  <th className="w-[10%] px-4 py-2 font-semibold">Action</th>
                </tr>
              </thead>
              <tbody>
                {filteredMappings.map((entry) => (
                  <tr key={`${entry.team.id}:${entry.role}`} className="border-b border-border-base last:border-0" data-testid={`team-role-mapping-row-${entry.team.id}-${entry.role}`}>
                    <td className="px-4 py-3 align-top">
                      <div className="font-semibold text-text-primary">{entry.team.name}</div>
                      <div className="break-all font-mono text-xs text-text-muted">{entry.team.id} / {entry.role}</div>
                    </td>
                    <td className="break-words px-4 py-3 align-top font-mono text-xs text-text-secondary">{entry.roleView.cli} · {entry.roleView.model} · {entry.roleView.max_concurrency}</td>
                    <td className="px-4 py-3 align-top">
                      {entry.query.isLoading ? <span className="text-xs text-text-muted">Loading</span> : entry.query.isError ? <span className="text-xs text-danger">Unavailable</span> : (
                        <RAMRoleChips ids={entry.query.data?.ram_role_ids ?? []} roles={ramRoles.data?.roles ?? []} />
                      )}
                    </td>
                    <td className="break-words px-4 py-3 align-top text-xs text-text-muted">
                      <div>v{entry.query.data?.version ?? '...'}</div>
                      <div>{entry.query.data?.updated_by ?? 'server'} · {entry.query.data?.updated_at ?? 'latest read'}</div>
                    </td>
                    <td className="px-4 py-3 align-top">
                      <button type="button" className="rounded border border-border-base px-2.5 py-1 text-xs font-semibold hover:bg-bg-subtle" onClick={() => setDrawer({ kind: 'mapping', entry })} data-testid={`team-role-edit-mapping-${entry.team.id}-${entry.role}`}>Edit</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      </div>

      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="team-role-ram-role-detail">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <div>
            <h2 className="text-sm font-semibold text-text-primary">RAM Roles, versions, safeguards</h2>
            <p className="mt-1 text-xs text-text-muted">Delete is blocked while Team Role references exist; duplicate creates a new editable RAM Role.</p>
          </div>
          <AccessMetaPill>{overview.data?.generated_at ? `audit ${overview.data.generated_at}` : 'audit ready'}</AccessMetaPill>
        </div>
        {ramRoles.isLoading && <Skeleton height="12rem" />}
        {ramRoles.isSuccess && (
          <div className="grid gap-3 lg:grid-cols-2 2xl:grid-cols-3">
            {ramRoles.data.roles.map((role) => (
              <RAMRoleCard
                key={role.id}
                role={role}
                refs={roleRefs.get(role.id) ?? []}
                onEdit={() => setDrawer({ kind: 'role', mode: 'edit', roleId: role.id })}
                onDuplicate={() => setDrawer({ kind: 'role', mode: 'duplicate', roleId: role.id })}
                onNotice={setNotice}
              />
            ))}
          </div>
        )}
      </section>

      {drawer?.kind === 'mapping' && (
        <MappingDrawer
          entry={activeMappingEntry ?? drawer.entry}
          roles={ramRoles.data?.roles ?? []}
          onClose={() => setDrawer(null)}
          onNotice={setNotice}
        />
      )}
      {drawer?.kind === 'role' && (
        <RAMRoleDrawer
          mode={drawer.mode}
          roleId={drawer.roleId}
          catalog={overview.data?.catalog ?? []}
          sourceRole={ramRoles.data?.roles.find((role) => role.id === drawer.roleId)}
          refs={drawer.roleId ? roleRefs.get(drawer.roleId) ?? [] : []}
          onClose={() => setDrawer(null)}
          onNotice={setNotice}
        />
      )}
    </section>
  );
}

function Summary({ label, value }: { label: string; value: React.ReactNode }): React.ReactElement {
  return (
    <div className="rounded border border-border-base bg-bg-elevated px-3 py-2">
      <div className="text-[0.6875rem] font-semibold uppercase text-text-muted">{label}</div>
      <div className="mt-1 text-xl font-semibold text-text-primary">{value}</div>
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
    <div className={`fixed right-4 top-4 z-50 flex max-w-lg items-start gap-3 rounded border px-4 py-3 text-sm shadow-2 ${tone}`} role="status" data-testid="team-role-notice">
      <span className="font-medium">{notice.message}</span>
      <button type="button" aria-label="Dismiss notification" onClick={onClose}><IconClose className="h-3.5 w-3.5" /></button>
    </div>
  );
}

function RAMRoleChips({ ids, roles }: { ids: string[]; roles: RAMRole[] }): React.ReactElement {
  if (ids.length === 0) return <span className="text-xs text-text-muted">No RAM Roles</span>;
  return (
    <div className="flex flex-wrap gap-1">
      {ids.map((id) => {
        const role = roles.find((item) => item.id === id);
        return <span key={id} className="rounded border border-border-base bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] text-text-secondary">{role?.name ?? id}</span>;
      })}
    </div>
  );
}

function MappingDrawer({ entry, roles, onClose, onNotice }: { entry: MappingEntry; roles: RAMRole[]; onClose: () => void; onNotice: (notice: Notice) => void }): React.ReactElement {
  const preview = usePreviewTeamRoleRAMMapping();
  const replace = useReplaceTeamRoleRAMMapping();
  const current = entry.query.data;
  const [draft, setDraft] = useState<string[]>(current?.ram_role_ids ?? []);
  const [confirming, setConfirming] = useState(false);
  const changed = sorted(draft).join('|') !== sorted(current?.ram_role_ids ?? []).join('|');
  const conflict = replace.error instanceof ApiError && replace.error.status === 409;
  const currentSignature = `${current?.version ?? 'unread'}:${sorted(current?.ram_role_ids ?? []).join('|')}`;

  useEffect(() => {
    setDraft(current?.ram_role_ids ?? []);
    preview.reset();
    replace.reset();
  }, [currentSignature]);

  const runPreview = () => preview.mutate({ team_id: entry.team.id, role: entry.role, ram_role_ids: draft });
  const save = () => {
    if (!current || !preview.data) return;
    replace.mutate({
      team_id: entry.team.id,
      role: entry.role,
      ram_role_ids: draft,
      expected_version: current.version,
    }, {
      onSuccess: () => {
        onNotice({ tone: 'success', message: `Applied ${entry.team.name} / ${entry.role}. Effective permissions refresh immediately.` });
        onClose();
      },
    });
  };

  return (
    <Drawer title="Edit Team Role mapping" subtitle={`${entry.team.name} / ${entry.role}`} onClose={onClose} testId="team-role-mapping-drawer">
      <div className="space-y-4">
        <section className="rounded border border-border-base bg-bg-subtle p-3" data-testid="team-role-work-config">
          <h3 className="text-xs font-semibold uppercase text-text-muted">Work config</h3>
          <div className="mt-2 grid gap-2 text-sm sm:grid-cols-3">
            <Spec label="CLI" value={entry.roleView.cli} />
            <Spec label="Model" value={entry.roleView.model} />
            <Spec label="Max concurrency" value={entry.roleView.max_concurrency} />
          </div>
          <p className="mt-2 text-xs text-text-muted">Runtime work config remains separate from authorization. This drawer changes only the RAM mapping.</p>
        </section>
        <EntityMultiSelect
          testId="team-role-drawer-ram-roles"
          options={roles.map((role) => ({ value: role.id, label: role.name, hint: `${role.stable_key} · v${role.version}` }))}
          values={draft}
          onChange={(values) => { setDraft(values); preview.reset(); replace.reset(); }}
          placeholder="Select RAM Roles"
          searchPlaceholder="Search RAM Roles"
          emptyLabel="No RAM Roles"
          ariaLabel="RAM Roles"
        />
        <div className="rounded border border-border-base p-3" data-testid="team-role-immediate-impact">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-sm font-semibold text-text-primary">Immediate impact</h3>
            <AccessMetaPill>CAS v{current?.version ?? 'unread'}</AccessMetaPill>
          </div>
          <p className="mt-1 text-xs text-text-muted">Preview uses server state and reports changed roles, affected members, linked projects, and the version to write.</p>
          {preview.data && (
            <div className="mt-3 grid gap-2 text-sm sm:grid-cols-3">
              <Spec label="Members" value={preview.data.affected_members} />
              <Spec label="Added / removed" value={`${preview.data.added_ram_role_ids.length} / ${preview.data.removed_ram_role_ids.length}`} />
              <Spec label="Projects" value={preview.data.affected_project_ids.length} />
            </div>
          )}
        </div>
        {(preview.isError || replace.isError) && (
          <div className="rounded border border-danger/40 bg-danger/10 p-3 text-sm text-danger" role="alert" data-testid="team-role-mapping-error">
            {((preview.error ?? replace.error) as Error).message}
            {conflict && (
              <button type="button" className="ml-2 rounded border border-danger/40 px-2 py-1 text-xs font-semibold" onClick={() => { void entry.query.refetch(); replace.reset(); preview.reset(); onNotice({ tone: 'warning', message: 'CAS conflict detected. Mapping refreshed; preview again before saving.' }); }}>
                Refresh server version
              </button>
            )}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm" onClick={onClose}>Cancel</button>
          <button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm font-semibold disabled:opacity-50" disabled={!changed || preview.isPending} onClick={runPreview}>Preview impact</button>
          <button type="button" className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-semibold text-btn-primary-fg disabled:opacity-50" disabled={!preview.data || replace.isPending} onClick={() => setConfirming(true)}>Apply mapping</button>
        </div>
      </div>
      <ConfirmModal
        open={confirming}
        title="Apply RAM mapping"
        message={<div className="space-y-2" data-testid="team-role-mapping-confirm"><p>This updates effective access for {preview.data?.affected_members ?? 0} members immediately.</p><p className="font-mono text-xs">{(preview.data?.current_ram_role_ids ?? []).join(', ') || 'none'} -&gt; {(preview.data?.next_ram_role_ids ?? []).join(', ') || 'none'}</p><p className="text-xs">Audit records include actor, Team Role, RAM Role ids, version, and affected project scope.</p></div>}
        confirmLabel="Apply now"
        danger
        busy={replace.isPending}
        onCancel={() => setConfirming(false)}
        onConfirm={() => { setConfirming(false); save(); }}
      />
    </Drawer>
  );
}

function RAMRoleCard({ role, refs, onEdit, onDuplicate, onNotice }: { role: RAMRole; refs: Array<{ team: TeamView; role: string }>; onEdit: () => void; onDuplicate: () => void; onNotice: (notice: Notice) => void }): React.ReactElement {
  const revoke = useRAMRoleRevoke();
  const deleteRole = useRAMRoleDelete();
  const [confirming, setConfirming] = useState(false);
  const blocked = refs.length > 0;
  const remove = () => {
    if (blocked) {
      onNotice({ tone: 'warning', message: `${role.name} is still referenced by ${refs.length} Team Roles. Remove or migrate mappings first.` });
      return;
    }
    if (role.kind === 'custom') {
      deleteRole.mutate({ id: role.id, expected_latest_version: role.version, confirm_unreferenced: true, reason: 'Team Roles RAM Role delete safeguard' }, {
        onSuccess: () => onNotice({ tone: 'success', message: `Deleted RAM Role ${role.name}.` }),
      });
    } else {
      revoke.mutate({ id: role.id, expected_latest_version: role.version, reason: 'Team Roles RAM Role revoke safeguard' }, {
        onSuccess: () => onNotice({ tone: 'success', message: `Revoked RAM Role ${role.name}.` }),
      });
    }
  };
  return (
    <article className="rounded border border-border-base bg-bg-base p-3" data-testid={`team-ram-role-${role.id}`}>
      <div className="flex items-start justify-between gap-2">
        <div>
          <h3 className="font-semibold text-text-primary">{role.name}</h3>
          <p className="mt-0.5 font-mono text-[0.6875rem] text-text-muted">{role.stable_key || role.id} · v{role.version}</p>
        </div>
        <AccessRiskBadge risk={role.risk} />
      </div>
      <p className="mt-2 line-clamp-2 text-xs text-text-secondary">{role.description}</p>
      <div className="mt-2 flex flex-wrap gap-1">
        <AccessMetaPill>{role.kind}</AccessMetaPill>
        <AccessMetaPill>{role.scope}</AccessMetaPill>
        <AccessMetaPill>{refs.length} refs</AccessMetaPill>
      </div>
      <div className="mt-2 font-mono text-[0.6875rem] text-text-muted">{role.permissions.slice(0, 4).join(', ')}{role.permissions.length > 4 ? ` +${role.permissions.length - 4}` : ''}</div>
      {blocked && <p className="mt-2 text-xs text-warning" data-testid={`team-ram-role-delete-blocked-${role.id}`}>Safeguard: referenced by {refs.map((ref) => `${ref.team.name}/${ref.role}`).join(', ')}</p>}
      <div className="mt-3 flex flex-wrap gap-2">
        <button type="button" className="rounded border border-border-base px-2 py-1 text-xs font-semibold" onClick={onEdit}>Edit</button>
        <button type="button" className="rounded border border-border-base px-2 py-1 text-xs font-semibold" onClick={onDuplicate}>Duplicate</button>
        <button type="button" className="rounded border border-danger/40 px-2 py-1 text-xs font-semibold text-danger disabled:opacity-50" onClick={() => setConfirming(true)} disabled={deleteRole.isPending || revoke.isPending}>Delete</button>
      </div>
      {(deleteRole.isError || revoke.isError) && <p className="mt-2 text-xs text-danger">{((deleteRole.error ?? revoke.error) as Error).message}</p>}
      <ConfirmModal
        open={confirming}
        title="Delete RAM Role"
        message={blocked ? 'This RAM Role is referenced. Delete is blocked until mappings are removed or migrated.' : `Delete ${role.name}? This writes an audit event and cannot be applied without the latest version.`}
        confirmLabel={blocked ? 'Blocked' : 'Delete'}
        danger
        busy={deleteRole.isPending || revoke.isPending}
        onCancel={() => setConfirming(false)}
        onConfirm={() => { setConfirming(false); remove(); }}
      />
    </article>
  );
}

function RAMRoleDrawer({ mode, roleId, sourceRole, catalog, refs, onClose, onNotice }: { mode: 'create' | 'edit' | 'duplicate'; roleId?: string; sourceRole?: RAMRole; catalog: AccessPermissionDefinition[]; refs: Array<{ team: TeamView; role: string }>; onClose: () => void; onNotice: (notice: Notice) => void }): React.ReactElement {
  const detail = useRAMRole(roleId ?? null);
  const create = useRAMRoleCreate();
  const update = useRAMRoleUpdate();
  const newVersion = useRAMRoleNewVersion();
  const base = detail.data?.latest ?? sourceRole;
  const [name, setName] = useState(mode === 'duplicate' && base ? `${base.name} copy` : base?.name ?? '');
  const [stableKey, setStableKey] = useState(mode === 'duplicate' && base ? `${base.stable_key || base.id}.copy` : base?.stable_key ?? '');
  const [description, setDescription] = useState(base?.description ?? '');
  const [scope, setScope] = useState(base?.scope ?? 'team');
  const [permissions, setPermissions] = useState<string[]>(base?.permissions ?? []);
  const conflict = (update.error instanceof ApiError && update.error.status === 409) || (newVersion.error instanceof ApiError && newVersion.error.status === 409);

  if (mode !== 'create' && !base && detail.isLoading) {
    return <Drawer title="RAM Role" subtitle="Loading role detail" onClose={onClose} testId="team-ram-role-drawer"><Skeleton height="12rem" /></Drawer>;
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
    <Drawer title={mode === 'create' ? 'Create RAM Role' : mode === 'duplicate' ? 'Duplicate RAM Role' : 'Edit RAM Role'} subtitle={base ? `${base.stable_key || base.id} · latest v${base.version}` : 'New role'} onClose={onClose} testId="team-ram-role-drawer">
      <div className="space-y-4">
        <TextField label="Name" value={name} onChange={setName} testId="team-ram-role-name" />
        <TextField label="Stable key" value={stableKey} onChange={setStableKey} testId="team-ram-role-stable-key" />
        <TextField label="Description" value={description} onChange={setDescription} testId="team-ram-role-description" />
        <TextField label="Scope" value={scope} onChange={setScope} testId="team-ram-role-scope" />
        <PermissionPicker catalog={catalog} selected={permissions} onChange={setPermissions} />
        <section className="rounded border border-border-base bg-bg-subtle p-3" data-testid="team-ram-role-audit">
          <h3 className="text-sm font-semibold text-text-primary">Safeguard and audit</h3>
          <p className="mt-1 text-xs text-text-muted">Versioned writes include expected latest version. Referenced roles cannot be deleted until mappings are changed.</p>
          <p className="mt-2 text-xs text-text-secondary">{refs.length > 0 ? `Referenced by ${refs.map((ref) => `${ref.team.name}/${ref.role}`).join(', ')}` : 'No Team Role references.'}</p>
        </section>
        {(create.isError || update.isError || newVersion.isError) && (
          <div className="rounded border border-danger/40 bg-danger/10 p-3 text-sm text-danger" role="alert" data-testid="team-ram-role-error">
            {((create.error ?? update.error ?? newVersion.error) as Error).message}
            {conflict && <button type="button" className="ml-2 rounded border border-danger/40 px-2 py-1 text-xs font-semibold" onClick={() => { void detail.refetch(); update.reset(); newVersion.reset(); onNotice({ tone: 'warning', message: 'RAM Role CAS conflict detected. Refreshed latest version; review before saving.' }); }}>Refresh latest</button>}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm" onClick={onClose}>Cancel</button>
          {mode === 'edit' && <button type="button" className="rounded border border-border-base px-3 py-1.5 text-sm font-semibold disabled:opacity-50" disabled={!roleId || permissions.length === 0 || newVersion.isPending} onClick={createVersion}>Create version</button>}
          <button type="button" className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-semibold text-btn-primary-fg disabled:opacity-50" disabled={!name.trim() || permissions.length === 0 || create.isPending || update.isPending} onClick={save}>{mode === 'edit' ? 'Save metadata' : 'Create'}</button>
        </div>
      </div>
    </Drawer>
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

function TextField({ label, value, onChange, testId }: { label: string; value: string; onChange: (value: string) => void; testId: string }): React.ReactElement {
  return (
    <label className="block">
      <span className="text-xs font-semibold uppercase text-text-muted">{label}</span>
      <input className="mt-1 w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm text-text-primary" value={value} onChange={(event) => onChange(event.target.value)} data-testid={testId} />
    </label>
  );
}

function PermissionPicker({ catalog, selected, onChange }: { catalog: AccessPermissionDefinition[]; selected: string[]; onChange: (values: string[]) => void }): React.ReactElement {
  return (
    <div className="rounded border border-border-base p-3" data-testid="team-ram-role-permissions">
      <h3 className="text-xs font-semibold uppercase text-text-muted">Permissions</h3>
      <div className="mt-2 max-h-64 space-y-1 overflow-y-auto">
        {catalog.map((permission) => {
          const checked = selected.includes(permission.key);
          return (
            <button key={permission.key} type="button" aria-pressed={checked} className={`flex w-full items-center justify-between gap-2 rounded px-2 py-1.5 text-left text-xs ${checked ? 'bg-success/10 text-success' : 'hover:bg-bg-subtle text-text-secondary'}`} onClick={() => onChange(checked ? selected.filter((key) => key !== permission.key) : [...selected, permission.key].sort())}>
              <span className="font-mono">{permission.key}</span>
              <AccessRiskBadge risk={permission.risk} />
            </button>
          );
        })}
      </div>
    </div>
  );
}

function Spec({ label, value }: { label: string; value: React.ReactNode }): React.ReactElement {
  return <div><div className="text-[0.6875rem] font-semibold uppercase text-text-muted">{label}</div><div className="mt-0.5 font-mono text-sm text-text-primary">{value}</div></div>;
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

function sorted(values: string[]): string[] {
  return [...values].sort();
}

// Teams directory — Agents (/organizations/:slug/teams/agents).
//
// MERGED surface (members-into-teams): the union of the old Teams directory
// Agents view (TEAMS column + by-team filter + working/idle chips + search +
// Model/Load/Backlog/Last-active) and the live Agents page (worker "Running on"
// binding, lifecycle/availability, the + Add Agent modal, agent-detail links,
// mobile cards, DM, and per-row delete) plus the member-list org role +
// membership status.
//
// DATA is a client-side OUTER JOIN keyed by normalizeIdentityRef, unioned across
// THREE sources — the directory (team membership + runtime working/idle), the
// live agents (lifecycle, worker, load), and the org members (org role +
// membership). normalizeIdentityRef(directory.ref) === normalizeIdentityRef
// (agent.identity_member_id) === normalizeIdentityRef(member.identity_id). A row
// shows if it appears in ANY source; a missing source degrades to em-dash. A
// standalone live agent with no member identity keys on its own agent id so it
// never collides and never drops.
import { useCallback, useMemo, useState } from 'react';
import type React from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import { OrgLink } from '@/OrgContext';
import { useDirectoryAgents, useTeams, type DirectoryAgent } from '@/api/teams';
import { useAgents, useDeleteAgent, useBatchAgentLifecycle, type AgentBatchAction } from '@/api/agents';
import { BatchToolbar, batchLabel } from '@/components/AgentBatchToolbar';
import {
  useMembers,
  normalizeIdentityRef,
  identityRefOf,
  type MemberResult,
} from '@/api/members';
import { useWorkers } from '@/api/workers';
import type { Agent } from '@/api/types';
import { ApiError } from '@/api/client';
import { AgentCreateModal } from '@/components/AgentCreateModal';
import { ConfirmModal } from '@/components/ConfirmModal';
import { EntityRef } from '@/components/EntityRef';
import { Avatar } from '@/components/Avatar';
import {
  AgentBacklogBadge,
  AgentLoadBadge,
  AvailabilityBadge,
  LifecycleBadge,
} from '@/components/AgentBadges';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { MembersSegmentControl } from '@/components/MembersSegmentControl';
import { Note } from '@/components/teams/kit';
import { Glyph } from '@/components/teams/teamsUi';
import { formatLocalTime } from '@/utils/time';

type StatusFilter = 'all' | 'working' | 'idle';
type RuntimeStatus = 'working' | 'idle' | null;

interface AgentRow {
  key: string; // join key (normalizeIdentityRef, or a synthetic id for standalone agents)
  dir?: DirectoryAgent;
  agent?: Agent;
  member?: MemberResult;
  name: string;
  identityRef: string; // prefixed "agent:<id>" — for DM
  agentId?: string; // live execution agent id → /agents/:id
}

const EM_DASH = <span className="text-text-muted">—</span>;

function isAgentMember(m: MemberResult): boolean {
  return m.kind === 'agent' || m.identity_id.startsWith('agent');
}

// v2.7 #197: map the backend's delete-guard codes to friendly copy (reused
// `members` namespace) so the UI never shows a raw error code (Rule 9).
function agentDeleteErrorMessage(err: unknown, t: TFunction): string {
  if (err instanceof ApiError) {
    if (err.code === 'agent_running') return t('agents.delete.errorRunning');
    if (err.code === 'agent_has_active_work') return t('agents.delete.errorActiveWork');
    if (err.code === 'not_found') return t('agents.delete.errorNotFound');
  }
  return err instanceof Error ? err.message : t('agents.delete.errorGeneric');
}

export default function TeamsDirectoryAgents(): React.ReactElement {
  const { t } = useTranslation('teams');
  const { t: tm } = useTranslation('members');
  const dirAgents = useDirectoryAgents();
  const agents = useAgents();
  const members = useMembers();
  const teams = useTeams();
  const workers = useWorkers();
  const del = useDeleteAgent();
  // T232 batch lifecycle (start/stop/restart/reset), ported from the retired
  // Agents page. Selection is a Set of LIVE agent ids — only rows backed by a
  // real execution Agent can be lifecycled, so directory-only / member-only rows
  // carry no checkbox and never enter the set.
  const batch = useBatchAgentLifecycle();

  const [query, setQuery] = useState('');
  const [status, setStatus] = useState<StatusFilter>('all');
  const [team, setTeam] = useState('all');
  const [createOpen, setCreateOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [pendingBatch, setPendingBatch] = useState<AgentBatchAction | null>(null);

  const workerName = (id: string): string | undefined =>
    (workers.data ?? []).find((w) => w.worker_id === id)?.name || undefined;

  const allRows = useMemo(() => {
    const byKey = new Map<string, AgentRow>();
    const ensure = (key: string): AgentRow => {
      let r = byKey.get(key);
      if (!r) {
        r = { key, name: '', identityRef: '' };
        byKey.set(key, r);
      }
      return r;
    };
    for (const m of members.data ?? []) {
      if (!isAgentMember(m)) continue;
      ensure(normalizeIdentityRef(m.identity_id)).member = m;
    }
    for (const a of agents.data ?? []) {
      const key = a.identity_member_id ? normalizeIdentityRef(a.identity_member_id) : `agent-id:${a.id}`;
      ensure(key).agent = a;
    }
    for (const d of dirAgents.data ?? []) {
      ensure(normalizeIdentityRef(d.ref)).dir = d;
    }
    for (const r of byKey.values()) {
      r.name = r.dir?.name || r.agent?.name || r.member?.display_name || r.key;
      r.agentId = r.agent?.id;
      r.identityRef = r.member
        ? identityRefOf(r.member)
        : r.dir?.ref ??
          (r.agent?.identity_member_id
            ? `agent:${normalizeIdentityRef(r.agent.identity_member_id)}`
            : `agent:${r.agent?.id ?? r.key}`);
    }
    return [...byKey.values()];
  }, [members.data, agents.data, dirAgents.data]);

  // Runtime working/idle for the chip + count. Prefer the directory's runtime
  // flag; a live-agent-only row derives it from running+busy so it is not
  // wrongly hidden by a Working/Idle filter.
  const runtimeStatus = useCallback((r: AgentRow): RuntimeStatus => {
    if (r.agent && r.agent.lifecycle !== 'running') return null;
    if (r.dir) return r.dir.status;
    if (r.agent) return r.agent.lifecycle === 'running' && r.agent.availability === 'busy' ? 'working' : 'idle';
    return 'idle';
  }, []);

  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    return allRows.filter((r) => {
      if (status !== 'all' && runtimeStatus(r) !== status) return false;
      if (team !== 'all' && !(r.dir?.teams ?? []).includes(team)) return false;
      if (q) {
        const role = `${r.dir?.role ?? ''} ${r.member?.role ?? ''}`.toLowerCase();
        if (!(r.name.toLowerCase().includes(q) || role.includes(q))) return false;
      }
      return true;
    });
  }, [allRows, query, status, team, runtimeStatus]);

  const workingCount = allRows.filter((r) => runtimeStatus(r) === 'working').length;
  const idleCount = allRows.filter((r) => runtimeStatus(r) === 'idle').length;
  const isLoading = dirAgents.isLoading || agents.isLoading || members.isLoading;
  const isError = dirAgents.isError || agents.isError || members.isError;

  // Batch selection is scoped to the LIVE agent ids that are currently VISIBLE
  // (respecting the active search / status / team filter) — mirroring the old
  // Agents page's order-preserving selection + indeterminate logic.
  const visibleAgentIds = useMemo(
    () => rows.map((r) => r.agentId).filter((id): id is string => !!id),
    [rows],
  );
  const selectedIds = useMemo(() => visibleAgentIds.filter((id) => selected.has(id)), [visibleAgentIds, selected]);
  const allSelected = visibleAgentIds.length > 0 && selectedIds.length === visibleAgentIds.length;
  const running = batch.progress.running;

  const toggleOne = (id: string): void =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const toggleAll = (): void => setSelected(() => (allSelected ? new Set() : new Set(visibleAgentIds)));
  const clearSelection = (): void => setSelected(new Set());

  // start is non-destructive → run immediately; stop/restart/reset → confirm first.
  const requestBatch = (action: AgentBatchAction): void => {
    if (selectedIds.length === 0 || running) return;
    if (action === 'start') void batch.run(selectedIds, action);
    else setPendingBatch(action);
  };
  const confirmBatch = (): void => {
    if (!pendingBatch) return;
    const action = pendingBatch;
    setPendingBatch(null);
    void batch.run(selectedIds, action);
  };

  return (
    <section className="space-y-4" data-testid="page-TeamsDirectoryAgents">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">{t('agents.title')}</h1>
          <p className="mt-1 font-mono text-xs text-text-muted">/organizations/:slug/teams/agents</p>
        </div>
        <div className="flex items-center gap-3">
          <span className="rounded-full border border-success/40 bg-success/10 px-2.5 py-1 text-[0.65rem] font-semibold text-success">
            {t('agents.count', { working: workingCount, total: allRows.length })}
          </span>
          <button
            type="button"
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
            onClick={() => setCreateOpen(true)}
            data-testid="agents-add-btn"
          >
            {t('agents.addAgent')}
          </button>
        </div>
      </header>

      {createOpen && <AgentCreateModal onClose={() => setCreateOpen(false)} />}

      {/* Mobile (col② hidden <md): segmented Humans/Agents switch. */}
      <MembersSegmentControl active="agents" />

      {/* T232: batch toolbar — shown while a selection exists OR a finished batch
          still has a result summary to surface. */}
      {(selectedIds.length > 0 || batch.progress.results.length > 0) && (
        <BatchToolbar
          selectedCount={selectedIds.length}
          progress={batch.progress}
          onAction={requestBatch}
          onClear={() => {
            clearSelection();
            batch.reset();
          }}
        />
      )}

      <div className="flex flex-wrap items-center gap-3">
        <input
          className="w-72 rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
          placeholder={t('agents.searchPlaceholder')}
          value={query}
          data-testid="agents-search"
          onChange={(e) => setQuery(e.target.value)}
        />
        <div className="ml-auto flex flex-wrap gap-2">
          {(['all', 'working', 'idle'] as const).map((s) => (
            <FilterChip key={s} on={status === s} testId={`agents-filter-${s}`} onClick={() => setStatus(s)}>
              {s === 'all'
                ? t('agents.filter.all', { count: allRows.length })
                : s === 'working'
                  ? t('agents.filter.working', { count: workingCount })
                  : t('agents.filter.idle', { count: idleCount })}
            </FilterChip>
          ))}
          <select
            className="rounded border border-border-base bg-bg-elevated px-2.5 py-1.5 text-xs font-semibold text-text-secondary focus-visible:border-accent focus-visible:outline-none"
            value={team}
            data-testid="agents-team-filter"
            onChange={(e) => setTeam(e.target.value)}
          >
            <option value="all">{t('common.teamFilterAll')}</option>
            {(teams.data ?? []).map((tt) => (
              <option key={tt.id} value={tt.name}>
                {tt.name}
              </option>
            ))}
          </select>
        </div>
      </div>

      {isLoading && <Skeleton height="12rem" />}
      {isError && (
        <p className="text-sm text-danger" data-testid="agents-error">
          {t('common.loadFailed', { error: String(dirAgents.error ?? agents.error ?? members.error) })}
        </p>
      )}
      {!isLoading && !isError && rows.length === 0 && (
        <EmptyState title={t('agents.empty.title')} body={t('agents.empty.body')} testId="agents-empty" />
      )}

      {/* Mobile (<md): card rows — avatar (tap → DM) + name link → AgentDetail. */}
      {!isLoading && !isError && rows.length > 0 && (
        <ul className="space-y-2 md:hidden" data-testid="agents-cards">
          {rows.map((r) => (
            <AgentCard key={r.key} row={r} runtime={runtimeStatus(r)} />
          ))}
        </ul>
      )}

      {/* Desktop (≥md): the full dual-dimension table. */}
      {!isLoading && !isError && rows.length > 0 && (
        <div className="hidden overflow-x-auto rounded-lg border border-border-base md:block">
          <table className="w-full min-w-[88rem] table-fixed text-sm" data-testid="agents-table">
            <colgroup>
              <col className="w-12" />
              <col className="w-72" />
              <col className="w-28" />
              <col className="w-32" />
              <col className="w-20" />
              <col className="w-24" />
              <col className="w-28" />
              <col className="w-28" />
              <col className="w-28" />
              <col className="w-24" />
              <col className="w-36" />
              <col className="w-28" />
              <col className="w-16" />
            </colgroup>
            <thead>
              <tr className="border-b border-border-base text-left text-[0.6875rem] uppercase tracking-wide text-text-muted">
                {/* T232: select-all — selects every VISIBLE row backed by a live agent. */}
                <th className="px-4 py-3">
                  <input
                    type="checkbox"
                    aria-label={tm('agents.list.selectAllAria')}
                    data-testid="agents-select-all"
                    className="h-4 w-4 cursor-pointer align-middle accent-brand"
                    checked={allSelected}
                    disabled={running || visibleAgentIds.length === 0}
                    ref={(el) => {
                      if (el) el.indeterminate = selectedIds.length > 0 && !allSelected;
                    }}
                    onChange={toggleAll}
                  />
                </th>
                <th className="px-3 py-3 font-semibold whitespace-nowrap">{t('agents.col.agent')}</th>
                <th className="px-3 py-3 font-semibold whitespace-nowrap">{t('agents.col.runtimeStatus')}</th>
                <th className="px-3 py-3 font-semibold whitespace-nowrap">{t('agents.col.lifecycle')}</th>
                <th className="px-3 py-3 font-semibold whitespace-nowrap">{t('agents.col.orgRole')}</th>
                <th className="px-3 py-3 font-semibold whitespace-nowrap">{t('agents.col.membership')}</th>
                <th className="px-3 py-3 font-semibold whitespace-nowrap">{t('agents.col.teams')}</th>
                <th className="px-3 py-3 font-semibold whitespace-nowrap">{t('agents.col.model')}</th>
                <th className="px-3 py-3 font-semibold whitespace-nowrap">{t('agents.col.load')}</th>
                <th className="px-3 py-3 font-semibold whitespace-nowrap">{t('agents.col.backlog')}</th>
                <th className="px-3 py-3 font-semibold whitespace-nowrap">{t('agents.col.worker')}</th>
                <th className="px-3 py-3 font-semibold whitespace-nowrap">{t('agents.col.lastActive')}</th>
                <th className="px-4 py-3 font-semibold" />
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <AgentTableRow
                  key={r.key}
                  row={r}
                  runtime={runtimeStatus(r)}
                  workerName={workerName}
                  selected={!!r.agentId && selected.has(r.agentId)}
                  selectDisabled={running}
                  onToggleSelect={r.agentId ? () => toggleOne(r.agentId!) : undefined}
                  onDelete={(id, name) => {
                    del.reset();
                    setPendingDelete({ id, name });
                  }}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {del.isError && (
        <p className="text-sm text-danger" data-testid="agent-delete-error" role="alert">
          {agentDeleteErrorMessage(del.error, tm)}
        </p>
      )}

      <ConfirmModal
        open={pendingDelete !== null}
        danger
        busy={del.isPending}
        title={tm('agents.delete.title')}
        message={pendingDelete ? tm('agents.delete.message', { name: pendingDelete.name }) : undefined}
        confirmLabel={tm('agents.delete.confirm')}
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

      {/* T232: confirm gate for destructive batch actions (stop/restart/reset). */}
      <ConfirmModal
        open={pendingBatch !== null}
        danger={pendingBatch === 'reset'}
        title={pendingBatch ? tm('agents.batch.confirmTitle', { action: batchLabel(pendingBatch, tm), count: selectedIds.length }) : ''}
        message={
          pendingBatch
            ? pendingBatch === 'reset'
              ? tm('agents.batch.resetMessage', { count: selectedIds.length })
              : tm('agents.batch.confirmMessage', { action: batchLabel(pendingBatch, tm), count: selectedIds.length })
            : undefined
        }
        confirmLabel={pendingBatch ? batchLabel(pendingBatch, tm) : tm('agents.batch.confirmFallback')}
        onCancel={() => setPendingBatch(null)}
        onConfirm={confirmBatch}
      />

      <Note>{t('agents.note')}</Note>
    </section>
  );
}

function AgentTableRow({
  row,
  runtime,
  workerName,
  selected,
  selectDisabled,
  onToggleSelect,
  onDelete,
}: {
  row: AgentRow;
  runtime: RuntimeStatus;
  workerName: (id: string) => string | undefined;
  selected: boolean;
  selectDisabled: boolean;
  onToggleSelect?: () => void;
  onDelete: (id: string, name: string) => void;
}): React.ReactElement {
  const { t } = useTranslation('teams');
  const { t: tm } = useTranslation('members');
  const { dir, agent, member } = row;
  const teamRole = dir?.role || member?.role;
  return (
    <tr
      data-testid={`agent-row-${row.name}`}
      data-selected={selected ? 'true' : 'false'}
      className={['border-b border-border-base last:border-0 hover:bg-bg-subtle', selected ? 'bg-brand/5' : ''].join(' ')}
    >
      {/* T232: per-row select — only a row backed by a live agent is selectable. */}
      <td className="px-3 py-3">
        {onToggleSelect ? (
          <input
            type="checkbox"
            aria-label={tm('agents.list.selectAgentAria', { name: row.name })}
            data-testid="agent-select-checkbox"
            data-agent-id={agent?.id}
            className="h-4 w-4 cursor-pointer align-middle accent-brand"
            checked={selected}
            disabled={selectDisabled}
            onChange={onToggleSelect}
          />
        ) : null}
      </td>
      <td className="px-3 py-3">
        <div className="flex items-center gap-2.5">
          <Glyph text={row.name.replace('agent-center-', '').slice(0, 2).toUpperCase()} size="sm" kind="agent" />
          <div className="min-w-0">
            {row.agentId ? (
              <OrgLink
                to={`/agents/${encodeURIComponent(row.agentId)}`}
                className="block truncate font-semibold text-accent hover:underline"
                data-testid="agent-name-link"
                title={row.name}
              >
                {row.name}
              </OrgLink>
            ) : (
              <div className="font-semibold text-text-primary">{row.name}</div>
            )}
            {teamRole && <div className="text-[0.6875rem] text-text-muted">{teamRole}</div>}
          </div>
        </div>
      </td>
      {/* Runtime working/idle (directory dimension). */}
      <td className="px-3 py-3">
        {runtime ? (
          <span className={['inline-flex items-center gap-1.5 font-semibold', runtime === 'working' ? 'text-status-blue-fg' : 'text-success'].join(' ')} data-testid="agent-runtime">
            <span className={['h-1.5 w-1.5 rounded-full', runtime === 'working' ? 'bg-status-blue-solid' : 'bg-success'].join(' ')} aria-hidden="true" />
            {runtime === 'working' ? t('agents.runtime.working') : t('agents.runtime.idle')}
          </span>
        ) : (
          EM_DASH
        )}
      </td>
      {/* Lifecycle + availability (live-agent dimension). */}
      <td className="px-3 py-3">
        {agent ? (
          <span className="inline-flex flex-wrap items-center gap-1 whitespace-nowrap">
            <LifecycleBadge lifecycle={agent.lifecycle} />
            <AvailabilityBadge availability={agent.availability} />
          </span>
        ) : (
          EM_DASH
        )}
      </td>
      {/* Org permission role (member dimension). */}
      <td className="px-3 py-3 text-xs text-text-secondary whitespace-nowrap" data-testid="agent-role">
        {member ? tm(`humans.role.${member.role}`, { defaultValue: member.role }) : EM_DASH}
      </td>
      {/* Membership status (member dimension): joined / disabled. */}
      <td className="px-3 py-3">
        <MembershipStatus status={member?.status} />
      </td>
      <td className="px-3 py-3">{dir ? <TeamsCell teams={dir.teams} /> : EM_DASH}</td>
      <td className="px-3 py-3 font-mono text-xs text-text-muted whitespace-nowrap">{dir?.model || agent?.model || '—'}</td>
      <td className="px-3 py-3">
        {dir ? (
          <>
            <span className="relative inline-block h-1.5 w-14 overflow-hidden rounded border border-border-base bg-bg-subtle align-middle">
              <span className="absolute inset-y-0 left-0 rounded bg-status-blue-solid" style={{ width: `${Math.round(dir.load * 100)}%` }} />
            </span>
            <span className="ml-1.5 font-mono text-[0.6875rem] text-text-muted">{dir.load.toFixed(1)}</span>
          </>
        ) : agent ? (
          <AgentLoadBadge agent={agent} />
        ) : (
          EM_DASH
        )}
      </td>
      <td className="px-3 py-3 font-mono text-xs text-text-muted whitespace-nowrap">
        {dir ? dir.backlog : agent ? <AgentBacklogBadge agent={agent} /> : EM_DASH}
      </td>
      {/* Worker "Running on" binding (live-agent dimension). */}
      <td className="px-3 py-3 text-xs text-text-muted">
        {agent?.worker_id ? (
          <EntityRef id={agent.worker_id} name={workerName(agent.worker_id)} fallback={agent.worker_id} testId="agent-worker-ref" />
        ) : (
          '—'
        )}
      </td>
      <td className="px-3 py-3 font-mono text-xs text-text-muted whitespace-nowrap">
        {dir?.last || (agent?.last_activity_at ? formatLocalTime(agent.last_activity_at) : '—')}
      </td>
      <td className="px-3 py-3 text-right">
        {agent && (
          <button
            type="button"
            data-testid="agent-delete-button"
            data-agent-id={agent.id}
            aria-label={tm('agents.list.deleteAgentAria', { name: row.name })}
            title={tm('agents.list.deleteAgentTitle')}
            onClick={() => onDelete(agent.id, row.name)}
            className="rounded px-2 py-1 text-xs text-text-muted whitespace-nowrap hover:bg-danger/10 hover:text-danger"
          >
            {tm('agents.list.delete')}
          </button>
        )}
      </td>
    </tr>
  );
}

function AgentCard({ row, runtime }: { row: AgentRow; runtime: RuntimeStatus }): React.ReactElement {
  const teamRole = row.dir?.role || row.member?.role;
  const cardBody = (
    <>
      <Avatar name={row.name} kind="agent" size="md" />
      <AgentCardBody row={row} subtitle={teamRole} runtime={runtime} />
    </>
  );
  return (
    <li data-testid="agent-member-card" data-identity={row.identityRef}>
      {row.agentId ? (
        <OrgLink
          to={`/agents/${encodeURIComponent(row.agentId)}`}
          className="flex items-start gap-3 rounded-lg border border-border-base bg-bg-elevated p-3 hover:border-border-strong hover:bg-bg-subtle"
          data-testid="agent-card-link"
        >
          {cardBody}
        </OrgLink>
      ) : (
        <div className="flex items-start gap-3 rounded-lg border border-border-base bg-bg-elevated p-3">{cardBody}</div>
      )}
    </li>
  );
}

function AgentCardBody({
  row,
  subtitle,
  runtime,
}: {
  row: AgentRow;
  subtitle?: string;
  runtime: RuntimeStatus;
}): React.ReactElement {
  const { t } = useTranslation('teams');
  const model = row.dir?.model || row.agent?.model;
  const load = row.dir ? row.dir.load.toFixed(1) : undefined;
  return (
    <span className="min-w-0 flex-1 space-y-2">
      <span>
        <span className="block truncate text-sm font-medium text-text-primary">{row.name}</span>
        {(subtitle || row.agent?.lifecycle) && (
          <span className="block truncate text-xs text-text-muted">
            {[subtitle, row.agent?.lifecycle].filter(Boolean).join(' · ')}
          </span>
        )}
      </span>
      <span className="flex flex-wrap items-center gap-1.5">
        {runtime && (
          <span
            className={[
              'inline-flex items-center gap-1 rounded px-2 py-0.5 text-[0.6875rem] font-semibold',
              runtime === 'working' ? 'bg-status-blue-bg text-status-blue-fg' : 'bg-success/10 text-success',
            ].join(' ')}
            data-testid="agent-card-runtime"
          >
            <span className={['h-1.5 w-1.5 rounded-full', runtime === 'working' ? 'bg-status-blue-solid' : 'bg-success'].join(' ')} aria-hidden="true" />
            {runtime === 'working' ? t('agents.runtime.working') : t('agents.runtime.idle')}
          </span>
        )}
        {row.agent ? (
          <AgentLoadBadge agent={row.agent} />
        ) : load ? (
          <span
            className="rounded bg-bg-subtle px-2 py-0.5 font-mono text-[0.6875rem] text-text-secondary"
            data-testid="agent-card-load"
          >
            {t('agents.col.load')} {load}
          </span>
        ) : null}
        {model && (
          <span className="rounded bg-bg-subtle px-2 py-0.5 font-mono text-[0.6875rem] text-text-secondary" data-testid="agent-card-model">
            {model}
          </span>
        )}
      </span>
      {!runtime && !row.agent && !model && (
        <span className="block truncate text-xs text-text-muted">
          {t('agents.col.runtimeStatus')} —
        </span>
      )}
    </span>
  );
}

// MembershipStatus — membership joined/disabled chip (folded from the retired
// /members/agents page). Conveyed by TEXT (never color-only). No member match
// (standalone agent / directory-only row) → a neutral em dash, never a
// misleading "Disabled". Reuses the `members` namespace strings.
function MembershipStatus({ status }: { status: MemberResult['status'] | undefined }): React.ReactElement {
  const { t } = useTranslation('members');
  if (!status) {
    return (
      <span className="text-xs text-text-muted" data-testid="agent-status" data-status="unknown">
        —
      </span>
    );
  }
  const joined = status === 'joined';
  return (
    <span
      className={[
        'rounded px-2 py-0.5 text-[0.6875rem] uppercase tracking-wide',
        joined ? 'bg-status-green-bg text-status-green-fg' : 'bg-status-slate-bg text-status-slate-fg-soft',
      ].join(' ')}
      data-testid="agent-status"
      data-status={status}
    >
      {joined ? t('agents.status.joined') : t('agents.status.disabled')}
    </span>
  );
}

// TeamsCell — the TEAMS column chips (shared with the Humans directory page).
export function TeamsCell({ teams }: { teams: string[] }): React.ReactElement {
  const { t } = useTranslation('teams');
  if (teams.length === 0) return <span className="text-text-muted">{t('teamsCell.unassigned')}</span>;
  return (
    <span className="flex flex-wrap gap-1">
      {teams.map((tname) => (
        <span key={tname} className="rounded bg-success/15 px-2 py-0.5 text-[0.65rem] font-semibold text-success">
          {tname}
        </span>
      ))}
    </span>
  );
}

export function FilterChip({
  on,
  onClick,
  children,
  testId,
}: {
  on: boolean;
  onClick: () => void;
  children: React.ReactNode;
  testId?: string;
}): React.ReactElement {
  return (
    <button
      type="button"
      data-testid={testId}
      onClick={onClick}
      className={[
        'rounded border px-3 py-1.5 text-xs font-semibold motion-safe:transition-colors',
        on ? 'border-accent bg-brand/10 text-brand-hover' : 'border-border-base bg-bg-elevated text-text-muted hover:border-border-strong hover:text-text-primary',
      ].join(' ')}
    >
      {children}
    </button>
  );
}

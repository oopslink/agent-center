import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useMemo, useState } from 'react';

import { ApiError } from '@/api/client';
import {
  useAgents,
  useDeleteAgent,
  useBatchAgentLifecycle,
  AGENT_BATCH_ACTIONS,
  type AgentBatchAction,
  type BatchLifecycleProgress,
} from '@/api/agents';
import { useMembers, normalizeIdentityRef, type MemberResult } from '@/api/members';
import { useWorkers } from '@/api/workers';
import { AgentCreateModal } from '@/components/AgentCreateModal';
import { ConfirmModal } from '@/components/ConfirmModal';
import { EntityRef } from '@/components/EntityRef';
import { AvailabilityBadge, LifecycleBadge, ProviderBadge } from '@/components/AgentBadges';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { formatLocalTime } from '@/utils/time';

// v2.7 #197: map the backend's delete-guard codes to friendly copy so the UI
// never shows a raw error code or fails silently (Rule 9).
function agentDeleteErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === 'agent_running') return 'This agent must be stopped before it can be deleted.';
    if (err.code === 'agent_has_active_work') {
      return 'This agent has active work items and cannot be deleted until they finish.';
    }
    if (err.code === 'not_found') return 'This agent no longer exists.';
  }
  return err instanceof Error ? err.message : 'Delete failed, please try again.';
}

// Agents page (/agents). Agent BC (v2.7 #101) — lists org-scoped agents
// with lifecycle + availability badges and worker, plus an "+ Add Agent"
// modal. Rows link to /agents/{id}. Replaces the retired
// workforce.AgentInstance list.
export default function Agents(): React.ReactElement {
  const agents = useAgents();
  const workers = useWorkers();
  const workerName = (id: string): string | undefined =>
    (workers.data ?? []).find((w) => w.worker_id === id)?.name || undefined;
  // dev2/v281 canonical-fold: the enhanced /agents page is now the single
  // agents surface, so the retired /members/agents page's Role + membership
  // Status (Joined/Disabled) columns are folded in here so no info is lost.
  // The Agent DTO (useAgents) carries neither field — they are member-level
  // only — so we join the org member list keyed by identity, exactly like the
  // old MembersAgents page did. The execution Agent's identity_member_id ==
  // the agent member's identity_id (the #157 unified-create link); a standalone
  // agent with no member (identity_member_id empty) → no match → '—' fallback.
  const members = useMembers();
  const memberByIdentity = useMemo(() => {
    const m = new Map<string, MemberResult>();
    for (const mem of members.data ?? []) {
      m.set(normalizeIdentityRef(mem.identity_id), mem);
    }
    return m;
  }, [members.data]);
  const memberForAgent = (identityMemberId: string | undefined): MemberResult | undefined =>
    identityMemberId ? memberByIdentity.get(normalizeIdentityRef(identityMemberId)) : undefined;
  const [createOpen, setCreateOpen] = useState(false);
  // v2.7 #197: per-row hard-delete gated behind a confirm dialog.
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null);
  const del = useDeleteAgent();

  // T232: multi-select + batch lifecycle (start/stop/restart/reset). Selection is
  // a Set of agent ids; the batch runner fans out client-side and exposes
  // progress. Destructive actions (everything but start) go through a confirm.
  const batch = useBatchAgentLifecycle();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [pendingBatch, setPendingBatch] = useState<AgentBatchAction | null>(null);
  const agentIds = useMemo(() => (agents.data ?? []).map((a) => a.id), [agents.data]);
  // Order-preserving list of the currently selected ids (only those still present
  // in the list — a row that vanished after a refetch drops out cleanly).
  const selectedIds = useMemo(() => agentIds.filter((id) => selected.has(id)), [agentIds, selected]);
  const allSelected = agentIds.length > 0 && selectedIds.length === agentIds.length;
  const running = batch.progress.running;

  const toggleOne = (id: string): void =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const toggleAll = (): void =>
    setSelected(() => (allSelected ? new Set() : new Set(agentIds)));
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
    <section className="space-y-4" data-testid="page-Agents">
      <header className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-semibold">Agents</h1>
          <p className="text-xs text-text-muted">
            Organization-scoped agents with a managed lifecycle.
          </p>
        </div>
        <button
          type="button"
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
          onClick={() => setCreateOpen(true)}
          data-testid="agents-add-btn"
        >
          + Add Agent
        </button>
      </header>

      {createOpen && <AgentCreateModal onClose={() => setCreateOpen(false)} />}

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

      {agents.isLoading && (
        <div className="space-y-2" data-testid="agents-loading">
          <Skeleton height="2.5rem" />
          <Skeleton height="2.5rem" />
        </div>
      )}
      {agents.isError && (
        <p className="text-sm text-danger" data-testid="agents-error">
          {(agents.error as Error).message}
        </p>
      )}
      {agents.isSuccess && agents.data.length === 0 && (
        <EmptyState
          testId="agents-empty"
          title="No agents yet"
          body="Agents are org-scoped entities with a managed lifecycle. Click + Add Agent to create one and bind it to a fleet worker."
        />
      )}
      {agents.isSuccess && agents.data.length > 0 && (
        // T80 (Tester2 §3.7 finding): this 9-column table lives in the narrow
        // col③ of the three-column desktop shell. The old `w-1/12` columns were
        // too narrow for their single-word uppercase headers ("AVAILABILITY",
        // "LIFECYCLE"), so the header text overflowed the cell and overlapped the
        // next one (rendered "AVAILABILITROLE"). Fix = make the table horizontally
        // scrollable with a min-width so columns never collapse below their header
        // width, rebalance the per-column widths so each header fits, and pin the
        // headers to a single line. At a wide col③ the table fits without scroll;
        // in a cramped column it scrolls instead of overlapping.
        <div className="overflow-x-auto">
          <table
            className="w-full min-w-[60rem] table-fixed border-separate border-spacing-0 rounded border border-border-base bg-bg-elevated text-text-primary"
            data-testid="agents-table"
          >
            <thead>
              <tr className="whitespace-nowrap text-left text-xs uppercase tracking-wide text-text-muted">
                {/* T232: select-all checkbox (selects every visible agent). */}
                <th className="w-[4%] border-b border-border-base px-3 py-2">
                  <input
                    type="checkbox"
                    aria-label="Select all agents"
                    data-testid="agents-select-all"
                    className="h-4 w-4 cursor-pointer align-middle accent-brand"
                    checked={allSelected}
                    disabled={running}
                    ref={(el) => {
                      // indeterminate when a partial (non-empty, non-full) selection.
                      if (el) el.indeterminate = selectedIds.length > 0 && !allSelected;
                    }}
                    onChange={toggleAll}
                  />
                </th>
                <th className="w-[14%] border-b border-border-base px-3 py-2">Name</th>
                <th className="w-[13%] border-b border-border-base px-3 py-2">Provider</th>
                <th className="w-[11%] border-b border-border-base px-3 py-2">Lifecycle</th>
                <th className="w-[13%] border-b border-border-base px-3 py-2">Availability</th>
                {/* dev2/v281 canonical-fold: Role + Status folded from the retired
                    /members/agents page so the merge loses no information. */}
                <th className="w-[8%] border-b border-border-base px-3 py-2">Role</th>
                <th className="w-[9%] border-b border-border-base px-3 py-2">Status</th>
                <th className="w-[13%] border-b border-border-base px-3 py-2">Last activity</th>
                <th className="w-[12%] border-b border-border-base px-3 py-2">Worker</th>
                <th className="w-[7%] border-b border-border-base px-3 py-2 text-right" />
              </tr>
            </thead>
            <tbody>
            {agents.data.map((a) => (
              <tr
                key={a.id}
                className={['text-sm', selected.has(a.id) ? 'bg-brand/5' : ''].join(' ')}
                data-testid="agent-row"
                data-agent-id={a.id}
                data-lifecycle={a.lifecycle}
                data-availability={a.availability}
                data-selected={selected.has(a.id) ? 'true' : 'false'}
              >
                <td className="border-b border-border-base px-3 py-2">
                  <input
                    type="checkbox"
                    aria-label={`Select agent ${a.name}`}
                    data-testid="agent-select-checkbox"
                    data-agent-id={a.id}
                    className="h-4 w-4 cursor-pointer align-middle accent-brand"
                    checked={selected.has(a.id)}
                    disabled={running}
                    onChange={() => toggleOne(a.id)}
                  />
                </td>
                <td className="border-b border-border-base px-3 py-2 font-medium">
                  {/* T133: the agent NAME is the row's open affordance — click it to
                      reach AgentDetail (replaces the separate "Open →" link). Styled
                      clickable (accent + hover underline + pointer). */}
                  <OrgLink
                    to={`/agents/${encodeURIComponent(a.id)}`}
                    className="block truncate text-accent hover:underline"
                    data-testid="agent-name-link"
                    title={a.name}
                  >
                    {a.name}
                  </OrgLink>
                </td>
                <td className="border-b border-border-base px-3 py-2">
                  {/* v2.8.1 list-enrich: provider = CLI + model badges (text
                      labels, not color-only; reuse the AgentBadges chip style).
                      Each chip omitted gracefully when the value is blank. */}
                  <div className="flex flex-wrap items-center gap-1" data-testid="agent-provider">
                    {a.cli && <ProviderBadge label={a.cli} testId="agent-cli-badge" />}
                    {a.model && <ProviderBadge label={a.model} testId="agent-model-badge" />}
                    {!a.cli && !a.model && <span className="text-xs text-text-muted">—</span>}
                  </div>
                </td>
                <td className="border-b border-border-base px-3 py-2">
                  <LifecycleBadge lifecycle={a.lifecycle} />
                </td>
                <td className="border-b border-border-base px-3 py-2">
                  <AvailabilityBadge availability={a.availability} />
                </td>
                {/* dev2/v281 canonical-fold: Role + membership Status resolved
                    via the member-list join (see memberForAgent). */}
                <td
                  className="border-b border-border-base px-3 py-2 text-xs text-text-secondary"
                  data-testid="agent-role"
                >
                  {memberForAgent(a.identity_member_id)?.role ?? (
                    <span className="text-text-muted">—</span>
                  )}
                </td>
                <td className="border-b border-border-base px-3 py-2">
                  <MembershipStatus status={memberForAgent(a.identity_member_id)?.status} />
                </td>
                <td className="border-b border-border-base px-3 py-2 align-top">
                  <AgentLastActivity
                    at={a.last_activity_at}
                    content={a.last_activity_content}
                  />
                </td>
                <td className="border-b border-border-base px-3 py-2 text-xs text-text-muted">
                  {a.worker_id ? (
                    <EntityRef
                      id={a.worker_id}
                      name={workerName(a.worker_id)}
                      fallback={a.worker_id}
                      testId="agent-worker-ref"
                    />
                  ) : (
                    '—'
                  )}
                </td>
                <td className="border-b border-border-base px-3 py-2 text-right">
                  {/* T133: the "Open →" link is gone — the agent NAME is now the
                      open affordance. Only Delete remains in the actions cell. */}
                  <div className="flex items-center justify-end gap-3">
                    <button
                      type="button"
                      data-testid="agent-delete-button"
                      data-agent-id={a.id}
                      aria-label={`Delete agent ${a.name}`}
                      title="Delete agent"
                      onClick={() => {
                        del.reset();
                        setPendingDelete({ id: a.id, name: a.name });
                      }}
                      className="rounded px-2 py-1 text-xs text-text-muted hover:bg-danger/10 hover:text-danger"
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            </tbody>
          </table>
        </div>
      )}

      {del.isError && (
        <p className="text-sm text-danger" data-testid="agent-delete-error" role="alert">
          {agentDeleteErrorMessage(del.error)}
        </p>
      )}

      <ConfirmModal
        open={pendingDelete !== null}
        danger
        busy={del.isPending}
        title="Delete agent"
        message={
          pendingDelete
            ? `Delete the agent "${pendingDelete.name}"? This permanently removes the agent and its membership. The agent must be stopped with no active work. This cannot be undone.`
            : undefined
        }
        confirmLabel="Delete"
        onCancel={() => {
          if (del.isPending) return;
          setPendingDelete(null);
          del.reset();
        }}
        onConfirm={() => {
          if (!pendingDelete) return;
          del.mutate(pendingDelete.id, {
            // Close on both outcomes; an error surfaces as a page-level alert
            // (Rule 9: never silent) that the next delete attempt resets.
            onSettled: () => setPendingDelete(null),
          });
        }}
      />

      {/* T232: confirm gate for destructive batch actions (stop/restart/reset). */}
      <ConfirmModal
        open={pendingBatch !== null}
        danger={pendingBatch === 'reset'}
        title={pendingBatch ? `${BATCH_LABELS[pendingBatch]} ${selectedIds.length} agent(s)` : undefined}
        message={
          pendingBatch
            ? pendingBatch === 'reset'
              ? `Reset ${selectedIds.length} selected agent(s)? This clears each agent's memory and workspace and cannot be undone.`
              : `${BATCH_LABELS[pendingBatch]} ${selectedIds.length} selected agent(s)?`
            : undefined
        }
        confirmLabel={pendingBatch ? BATCH_LABELS[pendingBatch] : 'Confirm'}
        onCancel={() => setPendingBatch(null)}
        onConfirm={confirmBatch}
      />
    </section>
  );
}

// T232: user-facing verb for each batch action (buttons, confirm copy, progress).
const BATCH_LABELS: Record<AgentBatchAction, string> = {
  start: 'Start',
  stop: 'Stop',
  restart: 'Restart',
  reset: 'Reset',
};

// BatchToolbar — the selection action bar above the Agents table (T232). Shows
// the selected count, the four lifecycle actions, a live progress bar while a
// batch runs, and a succeeded/failed summary once it finishes. Buttons are
// disabled mid-run so a batch can't overlap itself.
function BatchToolbar({
  selectedCount,
  progress,
  onAction,
  onClear,
}: {
  selectedCount: number;
  progress: BatchLifecycleProgress;
  onAction: (action: AgentBatchAction) => void;
  onClear: () => void;
}): React.ReactElement {
  const { running, total, done, results, action } = progress;
  const failed = results.filter((r) => !r.ok);
  const succeeded = results.length - failed.length;
  const pct = total > 0 ? Math.round((done / total) * 100) : 0;
  return (
    <div
      className="flex flex-wrap items-center gap-3 rounded border border-border-base bg-bg-subtle px-3 py-2"
      data-testid="agents-batch-toolbar"
    >
      <span className="text-sm font-medium" data-testid="agents-batch-selected-count">
        {selectedCount} selected
      </span>
      <div className="flex flex-wrap items-center gap-2">
        {AGENT_BATCH_ACTIONS.map((act) => (
          <button
            key={act}
            type="button"
            data-testid={`agents-batch-${act}`}
            disabled={running || selectedCount === 0}
            onClick={() => onAction(act)}
            className={[
              'rounded px-2.5 py-1 text-xs font-medium motion-safe:transition-colors disabled:opacity-50',
              act === 'reset'
                ? 'text-danger hover:bg-danger/10'
                : 'text-text-secondary hover:bg-bg-elevated hover:text-text-primary',
            ].join(' ')}
          >
            {BATCH_LABELS[act]}
          </button>
        ))}
      </div>

      {running && (
        <div className="flex min-w-[10rem] flex-1 items-center gap-2" data-testid="agents-batch-progress">
          <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-bg-elevated">
            <div
              className="h-full bg-brand motion-safe:transition-[width]"
              style={{ width: `${pct}%` }}
            />
          </div>
          <span className="whitespace-nowrap text-xs text-text-muted">
            {action ? BATCH_LABELS[action] : ''} {done}/{total}
          </span>
        </div>
      )}

      {!running && results.length > 0 && (
        <span className="text-xs text-text-muted" data-testid="agents-batch-summary">
          {succeeded} succeeded{failed.length > 0 ? `, ${failed.length} failed` : ''}
        </span>
      )}

      <button
        type="button"
        data-testid="agents-batch-clear"
        onClick={onClear}
        disabled={running}
        className="ml-auto rounded px-2 py-1 text-xs text-text-muted hover:bg-bg-elevated hover:text-text-primary disabled:opacity-50"
      >
        Clear
      </button>
    </div>
  );
}

// MembershipStatus — membership Status chip (dev2/v281 canonical-fold). Folds
// the retired /members/agents "Status" column: a member-level joined/disabled
// flag the Agent DTO does not carry, resolved via the member-list join. The
// state is conveyed by the TEXT label ("Joined"/"Disabled"), never color-only.
// COLORS = the curated SOLID X-100/X-800 pairs (theme-INDEPENDENT, AA in BOTH
// light + dark, no dark: variant): joined = green-100/green-800, disabled =
// slate-100/slate-700 (muted gray, NOT red — a11y guardrail). Do NOT use
// `bg-success/10 text-success` — that alpha-tint renders transparent + the green
// token is green-500 #22c55e = 2.28 on white = light-AA FAIL (Tester2 §3.3 catch;
// the recurring both-mode alpha-tint-over-token 命门).
// No member match (standalone agent / member list still loading) → a neutral
// "—" placeholder, never blank and never a misleading "Disabled".
function MembershipStatus({
  status,
}: {
  status: MemberResult['status'] | undefined;
}): React.ReactElement {
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
      {joined ? 'Joined' : 'Disabled'}
    </span>
  );
}

// AgentLastActivity — last-activity cell for an Agents row (v2.8.1 list-enrich).
// Shows the timestamp via formatLocalTime (LOCAL tz, not raw GMT/Z) + a SINGLE
// LINE truncated PLAIN-TEXT preview of the content (ellipsis; never grows the
// row height) with a `title` carrying the full text on hover. No activity (both
// fields absent) → a friendly "No recent activity" placeholder, never blank.
// Soft-ref safe: the content is rendered as a text node — a stale/deleted entity
// reference is just inert text, no lookup, no crash, no raw ref painted.
function AgentLastActivity({
  at,
  content,
}: {
  at: string | undefined;
  content: string | undefined;
}): React.ReactElement {
  const preview = content?.replace(/\s+/g, ' ').trim();
  if (!at && !preview) {
    return (
      <span className="text-xs italic text-text-muted" data-testid="agent-no-activity">
        No recent activity
      </span>
    );
  }
  return (
    <div className="min-w-0" data-testid="agent-last-activity">
      {at && (
        <div
          className="text-xs text-text-muted"
          data-testid="agent-last-activity-at"
          title={formatLocalTime(at)}
        >
          {formatLocalTime(at)}
        </div>
      )}
      {preview && (
        <div
          className="truncate text-xs text-text-secondary"
          data-testid="agent-last-activity-content"
          title={preview}
        >
          {preview}
        </div>
      )}
    </div>
  );
}

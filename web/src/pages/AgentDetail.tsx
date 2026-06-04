import type React from 'react';
import { useState } from 'react';
import { OrgLink } from '@/OrgContext';
import { useParams, useSearchParams } from 'react-router-dom';
import {
  useAgent,
  useAgentActivity,
  useResetAgent,
  useRestartAgent,
  useStartAgent,
  useStopAgent,
  type ResetScope,
} from '@/api/agents';
import { useWorkers } from '@/api/workers';
import { AvailabilityBadge, LifecycleBadge } from '@/components/AgentBadges';
import { EntityRef } from '@/components/EntityRef';
import { EmptyState } from '@/components/EmptyState';
import { AgentActivityRow } from '@/components/AgentActivityRow';
import { AgentProfile } from '@/components/AgentProfile';
import { AgentWorkItems } from '@/components/AgentWorkItems';
import { Breadcrumb } from '@/components/Breadcrumb';

// v2.7.1 #228: AgentDetail is a 4-tab surface. Workspace is a v2.8 placeholder;
// Profile/Activity/WorkItems get fleshed out in follow-up PRs (b/c/d).
const AGENT_TABS = [
  { key: 'profile', label: 'Profile' },
  { key: 'activity', label: 'Activity' },
  { key: 'workspace', label: 'Workspace' },
  { key: 'workitems', label: 'Work items' },
] as const;
type AgentTab = (typeof AGENT_TABS)[number]['key'];

// AgentDetail (/agents/:id). Agent BC (v2.7 #101). Header (name, lifecycle,
// availability, worker) + lifecycle controls gated by state, a Reset modal
// (scope + double-confirm), a WorkItem queue and an Activity stream.
// No profile-edit (no backend update-profile endpoint in #101 scope).
export default function AgentDetail(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const agent = useAgent(id);
  const activity = useAgentActivity(id);
  // v2.7 #192: resolve the bound worker_id to its name (raw id on hover).
  const workers = useWorkers();

  const start = useStartAgent(id);
  const stop = useStopAgent(id);
  const restart = useRestartAgent(id);
  const reset = useResetAgent(id);

  const [resetOpen, setResetOpen] = useState(false);
  // v2.7.1 #228: active tab synced to ?tab= so a tab is shareable/bookmarkable.
  const [searchParams, setSearchParams] = useSearchParams();
  const tabParam = searchParams.get('tab');
  const tab: AgentTab = (AGENT_TABS.some((t) => t.key === tabParam) ? tabParam : 'profile') as AgentTab;
  const setTab = (t: AgentTab) =>
    setSearchParams(
      (prev) => {
        const p = new URLSearchParams(prev);
        p.set('tab', t);
        return p;
      },
      { replace: true },
    );

  if (agent.isLoading) {
    return (
      <section className="text-sm text-text-muted" data-testid="page-AgentDetail">
        Loading agent…
      </section>
    );
  }
  if (agent.isError) {
    return (
      <section className="space-y-3" data-testid="page-AgentDetail">
        <p className="text-sm text-danger" data-testid="agent-not-found">
          {(agent.error as Error).message}
        </p>
        <OrgLink to="/agents" className="text-accent hover:underline">
          Back to agents
        </OrgLink>
      </section>
    );
  }
  if (!agent.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-AgentDetail">
        Agent lookup failed.
      </section>
    );
  }

  const a = agent.data;
  const lc = a.lifecycle;
  const transient = lc === 'stopping' || lc === 'resetting';
  const canStart = lc === 'stopped' || lc === 'error';
  const canStopRestart = lc === 'running';
  // Reset is available unless the agent is already resetting.
  const canReset = lc !== 'resetting';

  const lifecyclePending =
    start.isPending || stop.isPending || restart.isPending;
  const lifecycleError =
    (start.error as Error | null)?.message ??
    (stop.error as Error | null)?.message ??
    (restart.error as Error | null)?.message ??
    (reset.error as Error | null)?.message ??
    null;

  return (
    <section
      className="space-y-4"
      data-testid="page-AgentDetail"
      data-agent-id={a.id}
      data-lifecycle={a.lifecycle}
    >
      <Breadcrumb
        items={[{ label: 'Members' }, { label: 'Agents', to: '/members/agents' }, { label: a.name }]}
      />
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border-base pb-3">
        <div className="space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-xl font-semibold">{a.name}</h2>
            <LifecycleBadge lifecycle={a.lifecycle} />
            <AvailabilityBadge availability={a.availability} />
          </div>
          <p className="text-xs text-text-muted">
            {a.worker_id ? (
              <>
                worker{' '}
                <EntityRef
                  id={a.worker_id}
                  name={(workers.data ?? []).find((w) => w.worker_id === a.worker_id)?.name || undefined}
                  fallback={a.worker_id}
                  testId="agent-detail-worker"
                />
              </>
            ) : (
              'no worker'
            )}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2" data-testid="agent-controls">
          {canStart && (
            <button
              type="button"
              onClick={() => start.mutate()}
              disabled={lifecyclePending}
              className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
              data-testid="agent-start-btn"
            >
              Start
            </button>
          )}
          {canStopRestart && (
            <>
              <button
                type="button"
                onClick={() => stop.mutate()}
                disabled={lifecyclePending}
                className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle disabled:opacity-50"
                data-testid="agent-stop-btn"
              >
                Stop
              </button>
              <button
                type="button"
                onClick={() => restart.mutate()}
                disabled={lifecyclePending}
                className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle disabled:opacity-50"
                data-testid="agent-restart-btn"
              >
                Restart
              </button>
            </>
          )}
          {canReset && (
            <button
              type="button"
              onClick={() => setResetOpen(true)}
              disabled={transient || reset.isPending}
              className="rounded border border-danger/40 px-3 py-1.5 text-sm text-danger hover:bg-danger/10 disabled:opacity-50"
              data-testid="agent-reset-btn"
            >
              Reset
            </button>
          )}
          {transient && (
            <span className="text-xs text-text-muted" data-testid="agent-transient-note">
              {lc}…
            </span>
          )}
        </div>
      </header>

      {a.lifecycle_error && (
        <p className="text-xs text-danger" data-testid="agent-lifecycle-error">
          {a.lifecycle_error}
        </p>
      )}
      {lifecycleError && (
        <p className="text-xs text-danger" data-testid="agent-action-error">
          {lifecycleError}
        </p>
      )}

      {/* v2.7.1 #228: tab bar. */}
      <nav className="flex gap-1 border-b border-border-base" role="tablist" data-testid="agent-tabs">
        {AGENT_TABS.map((t) => (
          <button
            key={t.key}
            type="button"
            role="tab"
            aria-selected={tab === t.key}
            onClick={() => setTab(t.key)}
            data-testid={`agent-tab-${t.key}`}
            className={`-mb-px border-b-2 px-3 py-2 text-sm font-medium ${
              tab === t.key
                ? 'border-brand text-text-primary'
                : 'border-transparent text-text-muted hover:text-text-primary'
            }`}
          >
            {t.label}
          </button>
        ))}
      </nav>

      {tab === 'profile' && <AgentProfile agent={a} />}

      {tab === 'workspace' && (
        <EmptyState testId="agent-tabpanel-workspace" title="Workspace" body="Coming in v2.8." />
      )}

      {/* WorkItem queue (v2.7.1 #228 PR(d): read-only table). */}
      {tab === 'workitems' && <AgentWorkItems agentId={id} />}

      {/* Activity stream */}
      {tab === 'activity' && (
      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="agent-tabpanel-activity">
        <div className="mb-2 flex items-center justify-between">
          <h3 className="text-xs font-semibold uppercase tracking-wide text-text-muted">Activity Diagnostics</h3>
          <button
            type="button"
            className="rounded border border-border-strong px-2 py-1 text-xs text-text-secondary hover:bg-bg-subtle disabled:opacity-50"
            data-testid="agent-activity-refresh"
            onClick={() => void activity.refetch()}
            disabled={activity.isFetching}
            aria-busy={activity.isFetching}
          >
            {activity.isFetching ? 'Refreshing…' : 'Refresh'}
          </button>
        </div>
        {activity.isLoading && (
          <p className="text-xs text-text-muted" data-testid="agent-activity-loading">
            Loading activity…
          </p>
        )}
        {activity.isError && (
          <p className="text-xs text-danger" data-testid="agent-activity-error">
            {(activity.error as Error).message}
          </p>
        )}
        {activity.isSuccess && activity.data.length === 0 && (
          <p className="text-xs text-text-muted" data-testid="agent-activity-empty">
            No activity yet.
          </p>
        )}
        {activity.isSuccess && activity.data.length > 0 && (
          <ul className="divide-y divide-border-base" data-testid="agent-activity-list">
            {activity.data.map((ev) => (
              <AgentActivityRow key={ev.id} event={ev} />
            ))}
          </ul>
        )}
      </section>
      )}

      {resetOpen && (
        <ResetModal
          pending={reset.isPending}
          onClose={() => setResetOpen(false)}
          onConfirm={(scope) => {
            reset.mutate(
              { scope, confirm: true },
              { onSuccess: () => setResetOpen(false) },
            );
          }}
        />
      )}
    </section>
  );
}

// ResetModal — scope select + a SECOND confirmation checkbox before the
// destructive reset fires with confirm:true.
function ResetModal({
  pending,
  onClose,
  onConfirm,
}: {
  pending: boolean;
  onClose: () => void;
  onConfirm: (scope: ResetScope) => void;
}): React.ReactElement {
  const [scope, setScope] = useState<ResetScope>('memory');
  const [confirmed, setConfirmed] = useState(false);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      role="dialog"
      aria-modal="true"
      data-testid="agent-reset-modal"
    >
      <div className="w-full max-w-md rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <h2 className="text-lg font-semibold">Reset agent</h2>
        <p className="mt-1 text-xs text-text-muted">
          Resetting is destructive and cannot be undone. Choose a scope and
          confirm to proceed.
        </p>

        <label className="mt-4 mb-1 block text-xs font-medium text-text-primary">
          Scope
        </label>
        <select
          value={scope}
          onChange={(e) => setScope(e.target.value as ResetScope)}
          className="block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary focus:border-accent"
          data-testid="agent-reset-scope"
        >
          <option value="memory">Memory</option>
          <option value="workspace">Workspace</option>
          <option value="all">All</option>
        </select>

        <label className="mt-4 flex items-center gap-2 text-xs text-text-primary">
          <input
            type="checkbox"
            checked={confirmed}
            onChange={(e) => setConfirmed(e.target.checked)}
            data-testid="agent-reset-confirm"
          />
          I understand this will reset the agent&apos;s {scope}.
        </label>

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            data-testid="agent-reset-cancel"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={!confirmed || pending}
            onClick={() => onConfirm(scope)}
            className="rounded bg-danger px-3 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
            data-testid="agent-reset-submit"
          >
            {pending ? 'Resetting…' : 'Reset'}
          </button>
        </div>
      </div>
    </div>
  );
}

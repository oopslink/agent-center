import type React from 'react';
import { useState } from 'react';
import { OrgLink } from '@/OrgContext';
import { useParams } from 'react-router-dom';
import {
  useAgent,
  useAgentActivity,
  useAgentWorkItems,
  useResetAgent,
  useRestartAgent,
  useStartAgent,
  useStopAgent,
  type ResetScope,
} from '@/api/agents';
import { useWorkers } from '@/api/workers';
import { AvailabilityBadge, LifecycleBadge } from '@/components/AgentBadges';
import { EntityRef } from '@/components/EntityRef';
import { AgentActivityRow } from '@/components/AgentActivityRow';

// AgentDetail (/agents/:id). Agent BC (v2.7 #101). Header (name, lifecycle,
// availability, worker) + lifecycle controls gated by state, a Reset modal
// (scope + double-confirm), a WorkItem queue and an Activity stream.
// No profile-edit (no backend update-profile endpoint in #101 scope).
export default function AgentDetail(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const agent = useAgent(id);
  const workItems = useAgentWorkItems(id);
  const activity = useAgentActivity(id);
  // v2.7 #192: resolve the bound worker_id to its name (raw id on hover).
  const workers = useWorkers();

  const start = useStartAgent(id);
  const stop = useStopAgent(id);
  const restart = useRestartAgent(id);
  const reset = useResetAgent(id);

  const [resetOpen, setResetOpen] = useState(false);

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

      <dl className="grid grid-cols-2 gap-x-4 gap-y-2 rounded border border-border-base bg-bg-elevated p-4 text-sm text-text-primary">
        <dt className="text-text-muted">Description</dt>
        <dd>{a.description || <span className="italic text-text-muted">none</span>}</dd>
        <dt className="text-text-muted">Model</dt>
        <dd className="font-mono text-xs">{a.model || '—'}</dd>
        <dt className="text-text-muted">CLI</dt>
        <dd className="font-mono text-xs">{a.cli || '—'}</dd>
        <dt className="text-text-muted">Skills</dt>
        <dd className="font-mono text-xs">{a.skills && a.skills.length > 0 ? a.skills.join(', ') : '—'}</dd>
      </dl>

      {/* WorkItem queue */}
      <section className="rounded border border-border-base bg-bg-elevated p-4">
        <h3 className="mb-2 text-sm font-semibold text-text-primary">Work items</h3>
        {workItems.isLoading && (
          <p className="text-xs text-text-muted" data-testid="agent-workitems-loading">
            Loading work items…
          </p>
        )}
        {workItems.isError && (
          <p className="text-xs text-danger" data-testid="agent-workitems-error">
            {(workItems.error as Error).message}
          </p>
        )}
        {workItems.isSuccess && workItems.data.length === 0 && (
          <p className="text-xs text-text-muted" data-testid="agent-workitems-empty">
            No work items in the queue.
          </p>
        )}
        {workItems.isSuccess && workItems.data.length > 0 && (
          <ul className="divide-y divide-border-base" data-testid="agent-workitems-list">
            {workItems.data.map((w) => {
              // v2.7.1 #206: show the task title (links to task detail); the raw
              // pm task ref stays on hover (#192). Prefer the bare task_id field;
              // fall back to parsing task_ref (pm://tasks/<task-id>).
              const taskId = w.task_id || w.task_ref?.replace(/^pm:\/\/tasks\//, '') || '';
              const linkable = Boolean(w.task_title && w.project_id && taskId);
              return (
                <li
                  key={w.id}
                  className="flex items-center justify-between py-2 text-xs"
                  data-testid="agent-workitem-row"
                  data-workitem-id={w.id}
                  data-status={w.status}
                  title={w.task_ref}
                >
                  {linkable ? (
                    <OrgLink
                      to={`/projects/${encodeURIComponent(w.project_id as string)}/tasks/${encodeURIComponent(taskId)}`}
                      className="truncate text-text-secondary hover:text-accent"
                      data-testid="agent-workitem-task"
                    >
                      {w.task_title}
                    </OrgLink>
                  ) : (
                    <span className="text-text-secondary">{w.task_title || 'Work item'}</span>
                  )}
                  <span className="rounded bg-bg-subtle px-2 py-0.5 uppercase text-text-secondary">
                    {w.status}
                  </span>
                </li>
              );
            })}
          </ul>
        )}
      </section>

      {/* Activity stream */}
      <section className="rounded border border-border-base bg-bg-elevated p-4">
        <h3 className="mb-2 text-sm font-semibold text-text-primary">Activity</h3>
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

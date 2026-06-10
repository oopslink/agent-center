import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useState } from 'react';

import { ApiError } from '@/api/client';
import { useAgents, useDeleteAgent } from '@/api/agents';
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
  const [createOpen, setCreateOpen] = useState(false);
  // v2.7 #197: per-row hard-delete gated behind a confirm dialog.
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null);
  const del = useDeleteAgent();

  return (
    <section className="space-y-4" data-testid="page-Agents">
      <header className="flex items-start justify-between">
        <div>
          <h2 className="text-xl font-semibold">Agents</h2>
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
        <table
          className="w-full table-fixed border-separate border-spacing-0 rounded border border-border-base bg-bg-elevated text-text-primary"
          data-testid="agents-table"
        >
          <thead>
            <tr className="text-left text-xs uppercase tracking-wide text-text-muted">
              <th className="w-1/5 border-b border-border-base px-3 py-2">Name</th>
              <th className="w-1/6 border-b border-border-base px-3 py-2">Provider</th>
              <th className="w-1/12 border-b border-border-base px-3 py-2">Lifecycle</th>
              <th className="w-1/12 border-b border-border-base px-3 py-2">Availability</th>
              <th className="border-b border-border-base px-3 py-2">Last activity</th>
              <th className="w-1/6 border-b border-border-base px-3 py-2">Worker</th>
              <th className="border-b border-border-base px-3 py-2 text-right" />
            </tr>
          </thead>
          <tbody>
            {agents.data.map((a) => (
              <tr
                key={a.id}
                className="text-sm"
                data-testid="agent-row"
                data-agent-id={a.id}
                data-lifecycle={a.lifecycle}
                data-availability={a.availability}
              >
                <td className="border-b border-border-base px-3 py-2 font-medium">
                  <span className="block truncate">{a.name}</span>
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
                  <div className="flex items-center justify-end gap-3">
                    <OrgLink
                      to={`/agents/${encodeURIComponent(a.id)}`}
                      className="text-xs text-accent hover:underline"
                    >
                      Open →
                    </OrgLink>
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
    </section>
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

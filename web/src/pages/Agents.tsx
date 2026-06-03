import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useState } from 'react';

import { ApiError } from '@/api/client';
import { useAgents, useDeleteAgent } from '@/api/agents';
import { AgentCreateModal } from '@/components/AgentCreateModal';
import { ConfirmModal } from '@/components/ConfirmModal';
import { AvailabilityBadge, LifecycleBadge } from '@/components/AgentBadges';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';

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
              <th className="w-1/4 border-b border-border-base px-3 py-2">Name</th>
              <th className="w-1/6 border-b border-border-base px-3 py-2">Lifecycle</th>
              <th className="w-1/6 border-b border-border-base px-3 py-2">Availability</th>
              <th className="w-1/4 border-b border-border-base px-3 py-2">Worker</th>
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
                <td className="border-b border-border-base px-3 py-2 font-medium">{a.name}</td>
                <td className="border-b border-border-base px-3 py-2">
                  <LifecycleBadge lifecycle={a.lifecycle} />
                </td>
                <td className="border-b border-border-base px-3 py-2">
                  <AvailabilityBadge availability={a.availability} />
                </td>
                <td className="border-b border-border-base px-3 py-2 font-mono text-xs text-text-muted">
                  {a.worker_id || '—'}
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

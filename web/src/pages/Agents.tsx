import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useState } from 'react';

import { useAgents } from '@/api/agents';
import { AgentCreateModal } from '@/components/AgentCreateModal';
import { AvailabilityBadge, LifecycleBadge } from '@/components/AgentBadges';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';

// Agents page (/agents). Agent BC (v2.7 #101) — lists org-scoped agents
// with lifecycle + availability badges and worker, plus an "+ Add Agent"
// modal. Rows link to /agents/{id}. Replaces the retired
// workforce.AgentInstance list.
export default function Agents(): React.ReactElement {
  const agents = useAgents();
  const [createOpen, setCreateOpen] = useState(false);

  return (
    <section className="space-y-4" data-testid="page-Agents">
      <header className="flex items-start justify-between">
        <div>
          <h2 className="text-xl font-semibold">Agents</h2>
          <p className="text-xs text-text-muted">
            Org-scoped agents with a managed lifecycle.
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
                  <OrgLink
                    to={`/agents/${encodeURIComponent(a.id)}`}
                    className="text-xs text-accent hover:underline"
                  >
                    Open →
                  </OrgLink>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

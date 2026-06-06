import { OrgLink } from '@/OrgContext';
import { useAgents, useRestartAgent } from '@/api/agents';
import { AvailabilityBadge, LifecycleBadge } from '@/components/AgentBadges';
import { EmptyState } from '@/components/EmptyState';
import type { Agent } from '@/api/types';

// BoundAgents — the #273 "Bound Agents" tab. Lists the agents bound to this
// worker (filtered from the org agent list by worker_id, same client-side filter
// as the Environment page) + per-agent Restart + a link into AgentDetail.
//
// NOTE (backend-grounded): there is NO unbind/rebind endpoint — an agent's worker
// binding is immutable (only Archive clears it, #272). So PD's "restart/unbind"
// reduces to Restart here; "remove an agent from this worker" = archive it, which
// lives on AgentDetail (Open →). Unbind-as-such is deferred (flagged to PD).
function BoundAgentRow({ agent }: { agent: Agent }): React.ReactElement {
  const restart = useRestartAgent(agent.id);
  const canRestart = agent.lifecycle === 'running';
  return (
    <tr
      className="text-sm"
      data-testid="bound-agent-row"
      data-agent-id={agent.id}
      data-lifecycle={agent.lifecycle}
    >
      <td className="border-b border-border-base px-3 py-2 font-medium">{agent.name}</td>
      <td className="border-b border-border-base px-3 py-2">
        <LifecycleBadge lifecycle={agent.lifecycle} />
      </td>
      <td className="border-b border-border-base px-3 py-2">
        <AvailabilityBadge availability={agent.availability} />
      </td>
      <td className="border-b border-border-base px-3 py-2 text-right">
        <div className="flex items-center justify-end gap-3">
          {canRestart && (
            <button
              type="button"
              data-testid="bound-agent-restart"
              disabled={restart.isPending}
              onClick={() => restart.mutate()}
              className="text-xs text-accent hover:underline disabled:text-text-muted"
            >
              {restart.isPending ? 'Restarting…' : 'Restart'}
            </button>
          )}
          <OrgLink
            to={`/agents/${encodeURIComponent(agent.id)}`}
            className="text-xs text-accent hover:underline"
          >
            Open →
          </OrgLink>
        </div>
      </td>
    </tr>
  );
}

export function BoundAgents({ workerId }: { workerId: string }): React.ReactElement {
  const agents = useAgents();
  if (agents.isLoading) {
    return (
      <p className="text-sm text-text-muted" data-testid="bound-agents-loading">
        Loading agents…
      </p>
    );
  }
  if (agents.isError) {
    return (
      <p className="text-sm text-danger" data-testid="bound-agents-error">
        {(agents.error as Error).message}
      </p>
    );
  }
  const bound = (agents.data ?? []).filter((a) => a.worker_id === workerId);
  if (bound.length === 0) {
    return (
      <EmptyState
        testId="bound-agents-empty"
        title="No bound agents"
        body="No agents are bound to this worker."
      />
    );
  }
  return (
    <table className="w-full border-collapse" data-testid="bound-agents-table">
      <thead>
        <tr className="text-left text-xs text-text-muted">
          <th className="border-b border-border-base px-3 py-2">Name</th>
          <th className="border-b border-border-base px-3 py-2">Lifecycle</th>
          <th className="border-b border-border-base px-3 py-2">Availability</th>
          <th className="border-b border-border-base px-3 py-2 text-right" />
        </tr>
      </thead>
      <tbody>
        {bound.map((a) => (
          <BoundAgentRow key={a.id} agent={a} />
        ))}
      </tbody>
    </table>
  );
}

import type React from 'react';
import { useParams, Link } from 'react-router-dom';
import { useAgent } from '@/api/agents';
import { useFleet } from '@/api/fleet';

// AgentDetail (/agents/:name). Read-only profile + the agent's current
// executions pulled from the fleet snapshot (matched by agent_cli +
// worker_id since the snapshot doesn't carry agent_instance_id today).
export default function AgentDetail(): React.ReactElement {
  const { name = '' } = useParams<{ name: string }>();
  const agent = useAgent(name);
  const fleet = useFleet();

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
        <Link to="/agents" className="text-accent hover:underline">
          Back to agents
        </Link>
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
  const ownExecs = (fleet.data?.executions ?? []).filter(
    (e) => e.worker_id === a.worker_id && e.agent_cli === a.agent_cli,
  );

  return (
    <section className="space-y-4" data-testid="page-AgentDetail" data-agent-name={a.name}>
      <header className="border-b border-border-base pb-3">
        <h2 className="text-xl font-semibold">{a.name}</h2>
        <p className="text-xs text-text-muted">
          identity <span className="font-mono">{a.identity_id}</span>
        </p>
      </header>

      <dl className="grid grid-cols-2 gap-x-4 gap-y-2 rounded border border-border-base bg-bg-elevated p-4 text-sm text-text-primary">
        <dt className="text-text-muted">CLI</dt>
        <dd className="font-mono">{a.agent_cli}</dd>
        <dt className="text-text-muted">State</dt>
        <dd>
          <span className="rounded bg-bg-subtle px-2 py-0.5 text-xs uppercase">{a.state}</span>
        </dd>
        <dt className="text-text-muted">Worker</dt>
        <dd className="font-mono text-xs">{a.worker_id || '—'}</dd>
        <dt className="text-text-muted">Max concurrent</dt>
        <dd className="font-mono text-xs">{a.max_concurrent ?? '—'}</dd>
        <dt className="text-text-muted">Builtin</dt>
        <dd className="font-mono text-xs">{a.is_builtin ? 'yes' : 'no'}</dd>
      </dl>

      <section className="rounded border border-border-base bg-bg-elevated p-4">
        <h3 className="mb-2 text-sm font-semibold text-text-primary">Active executions</h3>
        {fleet.isLoading && (
          <p className="text-xs text-text-muted" data-testid="agent-exec-loading">
            Loading fleet…
          </p>
        )}
        {fleet.isError && (
          <p className="text-xs text-danger" data-testid="agent-exec-error">
            {(fleet.error as Error).message}
          </p>
        )}
        {fleet.isSuccess && ownExecs.length === 0 && (
          <p className="text-xs text-text-muted" data-testid="agent-exec-empty">
            No active executions for this agent right now.
          </p>
        )}
        {ownExecs.length > 0 && (
          <ul
            className="divide-y divide-border-base"
            data-testid="agent-exec-list"
          >
            {ownExecs.map((e) => (
              <li
                key={e.execution_id}
                className="flex items-center justify-between py-2 text-xs"
                data-testid="agent-exec-row"
                data-execution-id={e.execution_id}
              >
                <span>
                  <span className="font-mono">{e.execution_id}</span>{' '}
                  <span className="text-text-muted">on task</span>{' '}
                  <Link
                    to={`/tasks/${encodeURIComponent(e.task_id)}`}
                    className="font-mono text-accent hover:underline"
                  >
                    {e.task_id}
                  </Link>
                </span>
                <span className="rounded bg-bg-subtle px-2 py-0.5 uppercase text-text-secondary">
                  {e.status}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </section>
  );
}

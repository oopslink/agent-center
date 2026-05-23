import type React from 'react';
import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useAgents } from '@/api/agents';
import type { AgentInstance } from '@/api/types';

type StateFilter = 'all' | AgentInstance['state'];

const TABS: Array<{ label: string; value: StateFilter }> = [
  { label: 'All', value: 'all' },
  { label: 'Idle', value: 'idle' },
  { label: 'Active', value: 'active' },
  { label: 'Sleeping', value: 'sleeping' },
  { label: 'Archived', value: 'archived' },
];

// Agents page (/agents). Read-only list with state filter + link to
// profile. Mutations (create / archive) go through the CLI per
// ADR-0029; the empty state surfaces that.
export default function Agents(): React.ReactElement {
  const [filter, setFilter] = useState<StateFilter>('all');
  const agents = useAgents();
  const filtered = useMemo(() => {
    const list = agents.data ?? [];
    if (filter === 'all') return list;
    return list.filter((a) => a.state === filter);
  }, [agents.data, filter]);

  return (
    <section className="space-y-4" data-testid="page-Agents">
      <header>
        <h2 className="text-xl font-semibold">Agents</h2>
      </header>

      <div className="flex gap-1" role="tablist" aria-label="state filter">
        {TABS.map((t) => (
          <button
            key={t.value}
            type="button"
            role="tab"
            aria-selected={filter === t.value}
            onClick={() => setFilter(t.value)}
            className={[
              'rounded px-3 py-1 text-xs uppercase tracking-wide',
              filter === t.value
                ? 'bg-slate-900 text-white'
                : 'bg-slate-100 text-slate-600 hover:bg-slate-200',
            ].join(' ')}
            data-testid="agents-state-tab"
            data-state={t.value}
          >
            {t.label}
          </button>
        ))}
      </div>

      {agents.isLoading && (
        <p className="text-sm text-slate-500" data-testid="agents-loading">
          Loading…
        </p>
      )}
      {agents.isError && (
        <p className="text-sm text-red-600" data-testid="agents-error">
          {(agents.error as Error).message}
        </p>
      )}
      {agents.isSuccess && filtered.length === 0 && (
        <div
          className="rounded border border-dashed border-slate-300 bg-white p-6 text-center text-sm text-slate-500"
          data-testid="agents-empty"
        >
          <p>No agents for this filter.</p>
          <p className="mt-1 text-xs">
            Create one with{' '}
            <code className="rounded bg-slate-100 px-1.5 py-0.5 font-mono">
              agent-center agent create --name=… --agent-cli=…
            </code>
            .
          </p>
        </div>
      )}
      {filtered.length > 0 && (
        <table
          className="w-full table-fixed border-separate border-spacing-0 rounded border border-slate-200 bg-white"
          data-testid="agents-table"
        >
          <thead>
            <tr className="text-left text-xs uppercase tracking-wide text-slate-500">
              <th className="w-1/4 border-b border-slate-200 px-3 py-2">Name</th>
              <th className="w-1/6 border-b border-slate-200 px-3 py-2">CLI</th>
              <th className="w-1/6 border-b border-slate-200 px-3 py-2">State</th>
              <th className="w-1/4 border-b border-slate-200 px-3 py-2">Worker</th>
              <th className="border-b border-slate-200 px-3 py-2 text-right" />
            </tr>
          </thead>
          <tbody>
            {filtered.map((a) => (
              <tr
                key={a.id}
                className="text-sm"
                data-testid="agent-row"
                data-agent-id={a.id}
                data-agent-state={a.state}
              >
                <td className="border-b border-slate-100 px-3 py-2 font-medium">{a.name}</td>
                <td className="border-b border-slate-100 px-3 py-2 font-mono text-xs">
                  {a.agent_cli}
                </td>
                <td className="border-b border-slate-100 px-3 py-2">
                  <span className="rounded bg-slate-100 px-2 py-0.5 text-xs uppercase text-slate-700">
                    {a.state}
                  </span>
                </td>
                <td className="border-b border-slate-100 px-3 py-2 font-mono text-xs text-slate-500">
                  {a.worker_id || '—'}
                </td>
                <td className="border-b border-slate-100 px-3 py-2 text-right">
                  <Link
                    to={`/agents/${encodeURIComponent(a.name)}`}
                    className="text-xs text-blue-600 hover:underline"
                  >
                    Open profile →
                  </Link>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

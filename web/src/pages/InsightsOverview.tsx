import type React from 'react';
import { Link, useParams } from 'react-router-dom';
import { useInsightsOverview } from '@/api/insights';
import type { InsightsExecution, InsightsRankRow } from '@/api/types';
import { ErrorState } from '@/components/ErrorState';

export default function InsightsOverview(): React.ReactElement {
  const { slug } = useParams<{ slug: string }>();
  const query = useInsightsOverview();
  const base = `/organizations/${slug ?? ''}`;

  if (query.isLoading) {
    return <div className="p-6 text-sm text-text-secondary" data-testid="insights-loading">Loading insights...</div>;
  }
  if (query.isError) {
    return (
      <main className="p-6">
        <ErrorState message="Could not load insights." error={query.error} testId="insights-error" />
        <button type="button" className="mt-3 rounded border border-border-base px-3 py-1.5 text-sm" onClick={() => void query.refetch()}>
          Retry
        </button>
      </main>
    );
  }
  const data = query.data;
  if (!data) {
    return <div className="p-6 text-sm text-text-secondary" data-testid="insights-empty">No execution data in the past 24h.</div>;
  }
  const empty = data.executions.length === 0;

  return (
    <main className="mx-auto max-w-7xl space-y-6 p-6">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary">Insight Overview</h1>
          <p className="mt-1 text-sm text-text-secondary" data-testid="insights-window">
            Past 24h: {formatDateTime(data.window.from)} - {formatDateTime(data.window.to)}
          </p>
        </div>
        <div className="flex items-center gap-2 text-xs text-text-secondary">
          <span data-testid="insights-refreshed">Refreshed {formatDateTime(data.refreshed_at)}</span>
          <span className={data.freshness === 'fresh' ? 'text-success' : 'text-warning'} data-testid="insights-freshness">
            {data.freshness}
          </span>
          <button type="button" className="rounded border border-border-base px-2 py-1 text-xs" onClick={() => void query.refetch()}>
            Refresh
          </button>
        </div>
      </header>

      <section className="grid gap-3 md:grid-cols-4" aria-label="Execution metrics">
        <Metric label="Executions" value={String(data.metrics.execution_count)} />
        <Metric label="Failure rate" value={formatPct(data.metrics.failure_rate)} />
        <Metric label="Slot utilization" value={formatPct(data.metrics.slot_utilization)} />
        <Metric label="Queue wait" value={`${formatMs(data.metrics.queue_wait_p50_ms)} / ${formatMs(data.metrics.queue_wait_p95_ms)}`} hint="p50 / p95" />
        <Metric label="Duration" value={`${formatMs(data.metrics.execution_duration_p50_ms)} / ${formatMs(data.metrics.execution_duration_p95_ms)}`} hint="p50 / p95" />
      </section>

      {empty ? (
        <div className="rounded border border-border-base bg-bg-subtle p-6 text-sm text-text-secondary" data-testid="insights-empty">
          No execution data in the past 24h.
        </div>
      ) : (
        <>
          <section className="grid gap-4 lg:grid-cols-2">
            <RankTable title="Agents" rows={data.agents} testId="insights-agents" />
            <RankTable title="Projects" rows={data.projects} testId="insights-projects" />
          </section>
          <section>
            <h2 className="mb-2 text-sm font-semibold text-text-primary">TaskExecution detail</h2>
            <div className="overflow-x-auto rounded border border-border-base">
              <table className="min-w-full divide-y divide-border-base text-sm">
                <thead className="bg-bg-subtle text-left text-xs uppercase text-text-muted">
                  <tr>
                    <th className="px-3 py-2">Execution</th>
                    <th className="px-3 py-2">Task</th>
                    <th className="px-3 py-2">Agent</th>
                    <th className="px-3 py-2">Project</th>
                    <th className="px-3 py-2">Status</th>
                    <th className="px-3 py-2">Attempt</th>
                    <th className="px-3 py-2">Queue</th>
                    <th className="px-3 py-2">Duration</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border-base">
                  {data.executions.map((row) => (
                    <ExecutionRow key={row.execution_id} row={row} base={base} />
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        </>
      )}
    </main>
  );
}

function Metric({ label, value, hint }: { label: string; value: string; hint?: string }): React.ReactElement {
  return (
    <div className="rounded border border-border-base bg-bg-surface p-4">
      <div className="text-xs font-medium uppercase text-text-muted">{label}</div>
      <div className="mt-2 text-2xl font-semibold text-text-primary">{value}</div>
      {hint && <div className="mt-1 text-xs text-text-secondary">{hint}</div>}
    </div>
  );
}

function RankTable({ title, rows, testId }: { title: string; rows: InsightsRankRow[]; testId: string }): React.ReactElement {
  return (
    <div className="rounded border border-border-base" data-testid={testId}>
      <h2 className="border-b border-border-base px-3 py-2 text-sm font-semibold text-text-primary">{title}</h2>
      {rows.length === 0 ? (
        <div className="p-3 text-sm text-text-secondary">No rows.</div>
      ) : (
        <table className="min-w-full text-sm">
          <tbody>
            {rows.map((row) => (
              <tr key={row.id} className="border-b border-border-base last:border-b-0">
                <td className="px-3 py-2 text-text-primary">{row.name || row.id}</td>
                <td className="px-3 py-2 text-right text-text-secondary">{row.executions}</td>
                <td className="px-3 py-2 text-right text-text-secondary">{formatPct(row.failure_rate)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

function ExecutionRow({ row, base }: { row: InsightsExecution; base: string }): React.ReactElement {
  return (
    <tr data-testid="insights-execution-row">
      <td className="px-3 py-2 font-mono text-xs">
        <Link className="text-brand hover:underline" to={`${base}/insights/executions/${encodeURIComponent(row.execution_id)}`} data-testid="insights-execution-link">
          {row.execution_id}
        </Link>
      </td>
      <td className="px-3 py-2 text-text-primary">{row.task_org_ref || row.task_id} {row.task_title || ''}</td>
      <td className="px-3 py-2 text-text-secondary">{row.agent_id || '-'}</td>
      <td className="px-3 py-2 text-text-secondary">{row.project_name || row.project_id || '-'}</td>
      <td className="px-3 py-2 text-text-secondary">{row.status}</td>
      <td className="px-3 py-2 text-text-secondary">{row.attempt}</td>
      <td className="px-3 py-2 text-text-secondary">{formatMs(row.queue_wait_ms)}</td>
      <td className="px-3 py-2 text-text-secondary">{formatMs(row.duration_ms)}</td>
    </tr>
  );
}

function formatPct(v: number): string {
  return `${Math.round(v * 100)}%`;
}

function formatMs(v: number): string {
  if (!v) return '0 ms';
  if (v < 1000) return `${v} ms`;
  return `${(v / 1000).toFixed(1)} s`;
}

function formatDateTime(raw: string): string {
  if (!raw) return '-';
  return new Date(raw).toLocaleString();
}

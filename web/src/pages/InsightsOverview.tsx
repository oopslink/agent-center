import type React from 'react';
import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  useInsightOverview,
  useInsightTaskExecutions,
  type InsightDrilldownParams,
} from '@/api/insights';
import type { InsightLeaderboardRow, InsightOverview, InsightTaskExecution } from '@/api/types';
import { formatLocalTime } from '@/utils/time';

type MetricKey = 'executions' | 'failures' | 'slot_utilization' | 'queue_wait' | 'execution_duration';

const metricLabels: Record<MetricKey, string> = {
  executions: 'Executions',
  failures: 'Failure rate',
  slot_utilization: 'Slot utilization',
  queue_wait: 'Queue wait',
  execution_duration: 'Execution duration',
};

export default function InsightsOverviewPage(): React.ReactElement {
  const overview = useInsightOverview();
  const [drill, setDrill] = useState<InsightDrilldownParams | null>(null);
  const details = useInsightTaskExecutions(drill);

  if (overview.isLoading) {
    return <PageState testId="insights-loading" title="Loading Insight overview" />;
  }
  if (overview.isError) {
    return <PageState testId="insights-error" title="Insight overview unavailable" detail={(overview.error as Error).message} tone="danger" />;
  }
  if (!overview.data) {
    return <PageState testId="insights-empty" title="No Insight data" />;
  }

  const data = overview.data;
  const isEmpty = data.summary.executions === 0;

  return (
    <div className="flex h-full min-h-0 flex-col bg-bg-base" data-testid="insights-overview">
      <header className="border-b border-border-base px-6 py-4">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="font-heading text-xl font-semibold text-text-primary">Insight</h1>
            <p className="mt-1 text-sm text-text-muted" data-testid="insights-window">
              {formatLocalTime(data.window.started_at)} to {formatLocalTime(data.window.ended_at)}
            </p>
          </div>
          <div className="text-right text-xs text-text-muted">
            <div data-testid="insights-refreshed">Refreshed {formatLocalTime(data.refreshed_at)}</div>
            <span
              className={data.stale ? 'mt-1 inline-flex rounded-full bg-warning/15 px-2 py-0.5 font-semibold text-warning' : 'mt-1 inline-flex rounded-full bg-success/15 px-2 py-0.5 font-semibold text-success'}
              data-testid="insights-freshness"
            >
              {data.freshness}
            </span>
          </div>
        </div>
      </header>

      <main className="min-h-0 flex-1 overflow-auto px-6 py-5">
        {data.stale && (
          <div className="mb-4 rounded border border-warning/30 bg-warning/10 px-3 py-2 text-sm text-warning" data-testid="insights-stale">
            Backend marked this snapshot stale. Values below are last known and are not replaced with zero.
          </div>
        )}
        {isEmpty ? (
          <PageState testId="insights-empty" title="No TaskExecution records in this backend window" />
        ) : (
          <>
            <MetricGrid data={data} onDrill={(value) => setDrill({ filter: 'metric', value })} />
            <section className="mt-6 grid grid-cols-1 gap-5 xl:grid-cols-2">
              <Leaderboard title="Agents" rows={data.leaderboards.agents} onOpen={(id) => setDrill({ filter: 'agent', value: id })} />
              <Leaderboard title="Projects" rows={data.leaderboards.projects} onOpen={(id) => setDrill({ filter: 'project', value: id })} />
            </section>
          </>
        )}
        <DrilldownPanel params={drill} onClose={() => setDrill(null)} query={details} />
      </main>
    </div>
  );
}

function MetricGrid({ data, onDrill }: { data: InsightOverview; onDrill: (value: MetricKey) => void }): React.ReactElement {
  const s = data.summary;
  const cards = [
    { key: 'executions' as const, value: String(s.executions), meta: `${s.failures} failed` },
    { key: 'failures' as const, value: formatPercent(s.failure_rate), meta: `${s.failures} / ${s.executions}` },
    {
      key: 'slot_utilization' as const,
      value: formatPercent(s.slot_utilization.utilization),
      meta: `${s.slot_utilization.running} / ${s.slot_utilization.capacity} slots`,
    },
    {
      key: 'queue_wait' as const,
      value: formatPair(s.queue_wait.p50_seconds, s.queue_wait.p95_seconds),
      meta: 'p50 / p95',
    },
    {
      key: 'execution_duration' as const,
      value: formatPair(s.execution_duration.p50_seconds, s.execution_duration.p95_seconds),
      meta: 'p50 / p95',
    },
  ];
  return (
    <section className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-5" data-testid="insights-metrics">
      {cards.map((card) => (
        <button
          key={card.key}
          type="button"
          onClick={() => onDrill(card.key)}
          className="rounded border border-border-base bg-bg-elevated p-4 text-left transition hover:border-brand"
          data-testid={`insights-card-${card.key}`}
        >
          <div className="text-xs font-medium uppercase text-text-muted">{metricLabels[card.key]}</div>
          <div className="mt-2 text-2xl font-semibold text-text-primary">{card.value}</div>
          <div className="mt-1 text-xs text-text-muted">{card.meta}</div>
        </button>
      ))}
    </section>
  );
}

function Leaderboard({ title, rows, onOpen }: { title: string; rows: InsightLeaderboardRow[]; onOpen: (id: string) => void }): React.ReactElement {
  return (
    <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid={`insights-leaderboard-${title.toLowerCase()}`}>
      <h2 className="text-sm font-semibold text-text-primary">{title}</h2>
      {rows.length === 0 ? (
        <p className="py-6 text-sm text-text-muted">No rows</p>
      ) : (
        <div className="mt-3 divide-y divide-border-base">
          {rows.map((row) => (
            <button
              key={row.id}
              type="button"
              onClick={() => onOpen(row.id)}
              className="grid w-full grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-3 py-3 text-left hover:text-brand"
              data-testid={`insights-leaderboard-row-${row.id}`}
            >
              <span className="truncate text-sm font-medium text-text-primary">{row.name || row.id}</span>
              <span className="text-xs text-text-muted">{row.executions} exec</span>
              <span className="text-xs text-text-muted">{formatPercent(row.failure_rate)}</span>
            </button>
          ))}
        </div>
      )}
    </section>
  );
}

function DrilldownPanel({
  params,
  onClose,
  query,
}: {
  params: InsightDrilldownParams | null;
  onClose: () => void;
  query: ReturnType<typeof useInsightTaskExecutions>;
}): React.ReactElement | null {
  const title = useMemo(() => {
    if (!params) return '';
    if (params.filter === 'metric') return metricLabels[params.value as MetricKey] ?? params.value;
    return params.value;
  }, [params]);
  if (!params) return null;
  return (
    <section className="mt-6 rounded border border-border-base bg-bg-elevated" data-testid="insights-drilldown">
      <div className="flex items-center justify-between border-b border-border-base px-4 py-3">
        <h2 className="text-sm font-semibold text-text-primary">TaskExecution details: {title}</h2>
        <button type="button" className="rounded border border-border-base px-2 py-1 text-xs text-text-secondary hover:text-text-primary" onClick={onClose}>
          Close
        </button>
      </div>
      {query.isLoading && <PageState testId="insights-drilldown-loading" title="Loading TaskExecution records" compact />}
      {query.isError && <PageState testId="insights-drilldown-error" title="TaskExecution details unavailable" detail={(query.error as Error).message} tone="danger" compact />}
      {!query.isLoading && !query.isError && query.data && query.data.items.length === 0 && (
        <PageState testId="insights-drilldown-empty" title="No constituent TaskExecution records" compact />
      )}
      {!query.isLoading && !query.isError && query.data && query.data.items.length > 0 && (
        <TaskExecutionTable rows={query.data.items} />
      )}
    </section>
  );
}

function TaskExecutionTable({ rows }: { rows: InsightTaskExecution[] }): React.ReactElement {
  return (
    <div className="overflow-auto">
      <table className="min-w-full text-sm" data-testid="insights-drilldown-table">
        <thead className="bg-bg-subtle text-xs text-text-muted">
          <tr>
            <th className="px-4 py-2 text-left font-medium">TaskExecution</th>
            <th className="px-4 py-2 text-left font-medium">Project</th>
            <th className="px-4 py-2 text-left font-medium">Agent</th>
            <th className="px-4 py-2 text-left font-medium">Status</th>
            <th className="px-4 py-2 text-right font-medium">Queue wait</th>
            <th className="px-4 py-2 text-right font-medium">Duration</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border-base">
          {rows.map((row) => (
            <tr key={row.task_id}>
              <td className="px-4 py-3">
                <Link className="font-medium text-brand hover:underline" to={`/organizations/${locationSlug()}/projects/${row.project_id}/tasks/${row.task_id}`}>
                  {row.org_ref || row.task_id}
                </Link>
                <div className="max-w-[22rem] truncate text-xs text-text-muted">{row.title}</div>
              </td>
              <td className="px-4 py-3 text-text-secondary">{row.project_name}</td>
              <td className="px-4 py-3 text-text-secondary">{row.agent_name || row.agent_id}</td>
              <td className="px-4 py-3 text-text-secondary">{row.status}</td>
              <td className="px-4 py-3 text-right text-text-secondary">{formatDuration(row.queue_wait_seconds)}</td>
              <td className="px-4 py-3 text-right text-text-secondary">{formatDuration(row.duration_seconds)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function PageState({ testId, title, detail, tone = 'muted', compact = false }: { testId: string; title: string; detail?: string; tone?: 'muted' | 'danger'; compact?: boolean }): React.ReactElement {
  return (
    <div className={`${compact ? 'p-6' : 'flex h-full items-center justify-center p-8'} text-center`} data-testid={testId}>
      <div>
        <p className={tone === 'danger' ? 'text-sm font-medium text-danger' : 'text-sm font-medium text-text-secondary'}>{title}</p>
        {detail && <p className="mt-1 text-xs text-text-muted">{detail}</p>}
      </div>
    </div>
  );
}

function formatPercent(value: number | null): string {
  if (value == null) return 'n/a';
  return `${Math.round(value * 1000) / 10}%`;
}

function formatPair(p50: number | null, p95: number | null): string {
  return `${formatDuration(p50)} / ${formatDuration(p95)}`;
}

function formatDuration(seconds?: number | null): string {
  if (seconds == null) return 'n/a';
  if (seconds < 60) return `${seconds}s`;
  const mins = Math.floor(seconds / 60);
  const rem = seconds % 60;
  return rem ? `${mins}m ${rem}s` : `${mins}m`;
}

function locationSlug(): string {
  const m = window.location.pathname.match(/^\/organizations\/([^/]+)/);
  return m?.[1] ?? '';
}

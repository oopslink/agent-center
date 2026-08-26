import type React from 'react';
import { Link, useLocation, useSearchParams } from 'react-router-dom';
import { ApiError } from '@/api/client';
import {
  useInsightExecutions,
  useInsightOverview,
  type InsightExecutionFilters,
  type InsightExecutionRow,
  type InsightFreshness,
  type InsightOverview as InsightOverviewDTO,
  type InsightSummary,
} from '@/api/insights';
import { formatLocalTime } from '@/utils/time';

const EMPTY = '-';

export default function InsightOverview(): React.ReactElement {
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const isExecutionsRoute = location.pathname.endsWith('/insights/executions');
  const insightBase = insightBasePath(location.pathname);
  const overview = useInsightOverview(!isExecutionsRoute);
  const executionFilters = filtersFromSearch(searchParams);
  const executions = useInsightExecutions(executionFilters, isExecutionsRoute);

  const unavailableEnvelope = envelopeFromError(overview.error);

  return (
    <section className="space-y-4" data-testid="page-InsightOverview">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs font-semibold uppercase tracking-wide text-text-muted">Insight</p>
          <h1 className="text-xl font-semibold text-text-primary">
            {isExecutionsRoute ? 'TaskExecution details' : 'Overview'}
          </h1>
        </div>
        <Link
          to={isExecutionsRoute ? `${insightBase}/overview` : `${insightBase}/executions`}
          className="rounded border border-border-base bg-bg-elevated px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
        >
          {isExecutionsRoute ? 'Overview' : 'Execution details'}
        </Link>
      </header>

      {!isExecutionsRoute && overview.isLoading && (
        <StatePanel testId="insight-loading" title="Loading Insight overview" />
      )}

      {!isExecutionsRoute && overview.data && (
        <>
          <WindowBar data={overview.data} />
          {overview.data.freshness.state === 'stale' && (
            <StatePanel
              testId="insight-stale"
              tone="warn"
              title="Stale data"
              body="The projector is behind its freshness threshold. Values remain readable but may not represent the latest executions."
            />
          )}
          <SummaryCards summary={overview.data.summary} />
          <section className="grid gap-4 xl:grid-cols-2">
            <Leaderboard
              title="Agent leaderboard"
              rows={overview.data.agents.map((a) => ({
                id: a.agent_ref,
                name: a.display_name ?? a.agent_ref,
                summary: a.summary,
                href: `${insightBase}/executions?agent_ref=${encodeURIComponent(a.agent_ref)}`,
              }))}
              empty="No agent executions in the past 24 hours."
            />
            <Leaderboard
              title="Project leaderboard"
              rows={overview.data.projects.map((p) => ({
                id: p.project_id,
                name: p.name ?? p.project_id,
                summary: p.summary,
                href: `${insightBase}/executions?project_id=${encodeURIComponent(p.project_id)}`,
              }))}
              empty="No project executions in the past 24 hours."
            />
          </section>
          {isOverviewEmpty(overview.data) && (
            <StatePanel
              testId="insight-empty"
              title="No executions in the past 24 hours"
              body="The backend returned an empty 24-hour Insight window."
            />
          )}
        </>
      )}

      {!isExecutionsRoute && overview.isError && !unavailableEnvelope && (
        <StatePanel
          testId={isAuthError(overview.error) ? 'insight-auth-error' : 'insight-error'}
          tone="danger"
          title={isAuthError(overview.error) ? 'Insight access is not authorized' : 'Insight overview failed'}
          body={overview.error instanceof Error ? overview.error.message : 'The overview request failed.'}
        />
      )}

      {!isExecutionsRoute && unavailableEnvelope && (
        <>
          <WindowBar data={unavailableEnvelope} />
          <StatePanel
            testId={
              unavailableEnvelope.freshness.state === 'rebuilding'
                ? 'insight-rebuilding'
                : 'insight-unavailable'
            }
            tone="danger"
            title={
              unavailableEnvelope.freshness.state === 'rebuilding'
                ? 'Insight read model is rebuilding'
                : 'Insight is unavailable'
            }
            body={overview.error instanceof Error ? overview.error.message : 'The backend rejected the Insight request.'}
          />
        </>
      )}

      {isExecutionsRoute && (
        <ExecutionTable title={executionTitle(executionFilters)} query={executions} filters={executionFilters} />
      )}
    </section>
  );
}

function WindowBar({ data }: { data: Pick<InsightOverviewDTO, 'window' | 'refreshed_at' | 'freshness'> }): React.ReactElement {
  return (
    <div className="flex flex-wrap items-center gap-3 rounded border border-border-base bg-bg-elevated px-3 py-2 text-xs text-text-secondary" data-testid="insight-window">
      <strong className="text-text-primary">Past 24 hours</strong>
      <span title={data.window.start}>{formatLocalTime(data.window.start)}</span>
      <span aria-hidden="true">to</span>
      <span title={data.window.end}>{formatLocalTime(data.window.end)}</span>
      <span className="text-text-muted">Refreshed {formatLocalTime(data.refreshed_at)}</span>
      <FreshnessBadge freshness={data.freshness} />
    </div>
  );
}

function FreshnessBadge({ freshness }: { freshness: InsightFreshness }): React.ReactElement {
  const tone =
    freshness.state === 'fresh'
      ? 'border-success/30 bg-success/10 text-success'
      : freshness.state === 'stale'
        ? 'border-warning/30 bg-warning/10 text-warning'
        : 'border-danger/30 bg-danger/10 text-danger';
  return (
    <span className={`rounded-full border px-2 py-0.5 font-medium ${tone}`} data-testid="insight-freshness">
      {freshness.state} {formatMs(freshness.age_ms)} / {formatMs(freshness.threshold_ms)}
    </span>
  );
}

function SummaryCards({ summary }: { summary: InsightSummary }): React.ReactElement {
  return (
    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-5" data-testid="insight-summary">
      <MetricCard label="Completed executions" value={String(summary.completed_executions)} sub={`${summary.failed_executions} failed`} />
      <MetricCard label="Failure rate" value={formatRatio(summary.failure_rate)} sub={summary.failure_rate === null ? 'No completed executions' : 'Failed / completed'} title={summary.failure_rate === null ? 'No completed executions in the denominator.' : undefined} />
      <MetricCard label="Slot utilization" value={formatRatio(summary.slot_utilization)} sub={`Coverage ${formatRatio(summary.slot_coverage_ratio)}`} title={summary.slot_utilization === null ? 'No known admissible slot capacity in this window.' : undefined} />
      <MetricCard label="Queue wait" value={`p50 ${formatMs(summary.queue_wait_ms.p50)}`} sub={`p95 ${formatMs(summary.queue_wait_ms.p95)} - ${summary.queue_wait_ms.samples} samples`} title={summary.queue_wait_ms.samples === 0 ? 'No real executor starts in this window.' : undefined} />
      <MetricCard label="Execution duration" value={`p50 ${formatMs(summary.execution_duration_ms.p50)}`} sub={`p95 ${formatMs(summary.execution_duration_ms.p95)} - ${summary.execution_duration_ms.samples} samples`} title={summary.execution_duration_ms.samples === 0 ? 'No terminal executions with a real start in this window.' : undefined} />
    </div>
  );
}

function MetricCard({ label, value, sub, title }: { label: string; value: string; sub: string; title?: string }): React.ReactElement {
  return (
    <div className="rounded border border-border-base bg-bg-elevated p-3" title={title}>
      <div className="text-xs font-medium text-text-muted">{label}</div>
      <div className="mt-1 text-2xl font-semibold tabular-nums text-text-primary">{value}</div>
      <div className="mt-1 text-xs text-text-secondary">{sub}</div>
    </div>
  );
}

function Leaderboard({
  title,
  rows,
  empty,
}: {
  title: string;
  rows: Array<{ id: string; name: string; summary: InsightSummary; href: string }>;
  empty: string;
}): React.ReactElement {
  return (
    <div className="rounded border border-border-base bg-bg-elevated" data-testid={title.toLowerCase().replaceAll(' ', '-')}>
      <div className="border-b border-border-base px-3 py-2 text-sm font-semibold text-text-primary">{title}</div>
      {rows.length === 0 ? (
        <p className="p-3 text-sm text-text-muted">{empty}</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wide text-text-muted">
              <tr>
                <th className="px-3 py-2 font-medium">Name</th>
                <th className="px-3 py-2 font-medium">Completed</th>
                <th className="px-3 py-2 font-medium">Failure</th>
                <th className="px-3 py-2 font-medium">Queue</th>
                <th className="px-3 py-2 font-medium">Duration</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id} className="border-t border-border-base">
                  <td className="px-3 py-2">
                    <Link to={row.href} className="font-medium text-brand hover:underline">
                      {row.name}
                    </Link>
                    <div className="font-mono text-xs text-text-muted">{row.id}</div>
                  </td>
                  <td className="px-3 py-2 tabular-nums">{row.summary.completed_executions}</td>
                  <td className="px-3 py-2 tabular-nums">{formatRatio(row.summary.failure_rate)}</td>
                  <td className="px-3 py-2 tabular-nums">{formatMs(row.summary.queue_wait_ms.p50)} / {formatMs(row.summary.queue_wait_ms.p95)}</td>
                  <td className="px-3 py-2 tabular-nums">{formatMs(row.summary.execution_duration_ms.p50)} / {formatMs(row.summary.execution_duration_ms.p95)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function ExecutionTable({
  title,
  query,
  filters,
}: {
  title: string;
  query: ReturnType<typeof useInsightExecutions>;
  filters: InsightExecutionFilters;
}): React.ReactElement {
  return (
    <div className="rounded border border-border-base bg-bg-elevated" data-testid="insight-executions-table">
      <div className="flex items-center justify-between gap-3 border-b border-border-base px-3 py-2">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">TaskExecution details</h2>
          <p className="text-xs text-text-muted">{title}</p>
        </div>
        <ExecutionFilterChips filters={filters} />
      </div>
      {query.isLoading && <StatePanel testId="insight-drilldown-loading" title="Loading execution details" />}
      {query.isError && (
        <StatePanel
          testId={isAuthError(query.error) ? 'insight-drilldown-auth-error' : 'insight-drilldown-error'}
          tone="danger"
          title={isAuthError(query.error) ? 'Execution access is not authorized' : 'Execution detail request failed'}
          body={query.error instanceof Error ? query.error.message : undefined}
        />
      )}
      {query.data && (
        <>
          <WindowBar data={query.data} />
          {query.data.freshness.state === 'stale' && (
            <StatePanel testId="insight-drilldown-stale" tone="warn" title="Execution details are stale" />
          )}
          {query.data.executions.length === 0 ? (
            <StatePanel testId="insight-drilldown-empty" title="No matching execution attempts" />
          ) : (
            <>
              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs">
                  <thead className="uppercase tracking-wide text-text-muted">
                    <tr>
                      <th className="px-3 py-2 font-medium">Execution</th>
                      <th className="px-3 py-2 font-medium">Task</th>
                      <th className="px-3 py-2 font-medium">Agent</th>
                      <th className="px-3 py-2 font-medium">Outcome</th>
                      <th className="px-3 py-2 font-medium">Queue</th>
                      <th className="px-3 py-2 font-medium">Duration</th>
                      <th className="px-3 py-2 font-medium">Quality</th>
                    </tr>
                  </thead>
                  <tbody>
                    {query.data.executions.map((row) => <ExecutionRow key={row.execution_id} row={row} />)}
                  </tbody>
                </table>
              </div>
              {query.data.next_cursor && (
                <div className="border-t border-border-base px-3 py-2">
                  <Link
                    to={`?${executionQueryString({ ...filters, cursor: query.data.next_cursor })}`}
                    className="text-sm font-medium text-brand hover:underline"
                  >
                    Next page
                  </Link>
                </div>
              )}
            </>
          )}
        </>
      )}
    </div>
  );
}

function ExecutionRow({ row }: { row: InsightExecutionRow }): React.ReactElement {
  return (
    <tr className="border-t border-border-base" data-testid="insight-execution-row">
      <td className="px-3 py-2 align-top">
        <div className="font-mono text-text-primary">{row.execution_id}</div>
        <div className="font-mono text-text-muted">{row.command_id ?? EMPTY}</div>
        <div className="mt-1 text-[11px] text-text-muted" data-testid="insight-execution-detail-unavailable">
          TaskExecution detail route unavailable
        </div>
      </td>
      <td className="px-3 py-2 align-top">
        <div className="text-text-primary">{row.task_title ?? row.task_ref ?? row.task_id ?? EMPTY}</div>
        <div className="font-mono text-text-muted">{row.project_name ?? row.project_id ?? EMPTY}</div>
      </td>
      <td className="px-3 py-2 align-top">
        <div>{row.agent_name ?? row.agent_ref}</div>
        <div className="font-mono text-text-muted">{row.worker_id ?? EMPTY}</div>
      </td>
      <td className="px-3 py-2 align-top">
        <div>{row.outcome ?? EMPTY}</div>
        <div className="text-text-muted">{row.failure_reason ?? (row.recovered ? 'recovered' : '')}</div>
      </td>
      <td className="px-3 py-2 align-top tabular-nums">{formatMs(row.queue_wait_ms)}</td>
      <td className="px-3 py-2 align-top tabular-nums">{formatMs(row.duration_ms)}</td>
      <td className="px-3 py-2 align-top">{row.quality}</td>
    </tr>
  );
}

function ExecutionFilterChips({ filters }: { filters: InsightExecutionFilters }): React.ReactElement {
  const chips: string[] = [];
  if (filters.agent_ref) chips.push(`agent_ref=${filters.agent_ref}`);
  if (filters.project_id) chips.push(`project_id=${filters.project_id}`);
  if (filters.cursor) chips.push('paged');
  if (chips.length === 0) chips.push('unfiltered');
  return (
    <div className="flex flex-wrap justify-end gap-1">
      {chips.map((chip) => (
        <span key={chip} className="rounded border border-border-base px-2 py-0.5 text-xs text-text-secondary">
          {chip}
        </span>
      ))}
    </div>
  );
}

function StatePanel({
  testId,
  title,
  body,
  tone = 'muted',
}: {
  testId: string;
  title: string;
  body?: string;
  tone?: 'muted' | 'warn' | 'danger';
}): React.ReactElement {
  const cls =
    tone === 'danger'
      ? 'border-danger/30 bg-danger/5 text-danger'
      : tone === 'warn'
        ? 'border-warning/30 bg-warning/5 text-warning'
        : 'border-border-base bg-bg-elevated text-text-secondary';
  return (
    <div className={`rounded border p-3 text-sm ${cls}`} data-testid={testId}>
      <div className="font-medium">{title}</div>
      {body && <p className="mt-1 text-xs">{body}</p>}
    </div>
  );
}

function formatRatio(value: number | null): string {
  if (value === null) return EMPTY;
  return `${Math.round(value * 1000) / 10}%`;
}

function formatMs(value: number | null): string {
  if (value === null) return EMPTY;
  if (value < 1000) return `${Math.round(value)} ms`;
  return `${Math.round(value / 100) / 10} s`;
}

function isOverviewEmpty(data: InsightOverviewDTO): boolean {
  return data.summary.completed_executions === 0 && data.agents.length === 0 && data.projects.length === 0;
}

function filtersFromSearch(params: URLSearchParams): InsightExecutionFilters {
  const filters: InsightExecutionFilters = { limit: 50 };
  const agentRef = params.get('agent_ref');
  const projectID = params.get('project_id');
  const cursor = params.get('cursor');
  if (agentRef) filters.agent_ref = agentRef;
  if (projectID) filters.project_id = projectID;
  if (cursor) filters.cursor = cursor;
  return filters;
}

function insightBasePath(pathname: string): string {
  const marker = '/insights/';
  const idx = pathname.indexOf(marker);
  if (idx >= 0) return pathname.slice(0, idx + '/insights'.length);
  return '/insights';
}

function executionQueryString(filters: InsightExecutionFilters): string {
  const params = new URLSearchParams();
  if (filters.agent_ref) params.set('agent_ref', filters.agent_ref);
  if (filters.project_id) params.set('project_id', filters.project_id);
  if (filters.cursor) params.set('cursor', filters.cursor);
  return params.toString();
}

function executionTitle(filters: InsightExecutionFilters): string {
  if (filters.agent_ref) return `agent_ref=${filters.agent_ref}`;
  if (filters.project_id) return `project_id=${filters.project_id}`;
  return 'All executions';
}

function isAuthError(err: unknown): boolean {
  return err instanceof ApiError && (err.status === 401 || err.status === 403);
}

function envelopeFromError(err: unknown): Pick<InsightOverviewDTO, 'window' | 'refreshed_at' | 'freshness'> | null {
  const body = err instanceof ApiError ? err.body : null;
  if (!body || typeof body !== 'object') return null;
  const envelope = body as Partial<InsightOverviewDTO>;
  if (!envelope.window || !envelope.refreshed_at || !envelope.freshness) return null;
  if (envelope.freshness.state !== 'rebuilding' && envelope.freshness.state !== 'unavailable') return null;
  return {
    window: envelope.window,
    refreshed_at: envelope.refreshed_at,
    freshness: envelope.freshness,
  };
}

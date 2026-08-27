import type React from 'react';
import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ApiError } from '@/api/client';
import {
  useInsightExecution,
  useInsightExecutions,
  useInsightOverview,
  type InsightExecutionFilters,
  type InsightExecutionRow,
  type InsightFreshness,
  type InsightOverview as InsightOverviewDTO,
  type InsightSummary,
} from '@/api/insights';
import { formatLocalTime } from '@/utils/time';

type Drilldown =
  | { label: string; filters: InsightExecutionFilters }
  | { label: string; filters: InsightExecutionFilters; agent_ref: string }
  | { label: string; filters: InsightExecutionFilters; project_id: string };

const EMPTY = '-';

export default function InsightOverview(): React.ReactElement {
  const overview = useInsightOverview();
  const [drilldown, setDrilldown] = useState<Drilldown | null>(null);
  const executions = useInsightExecutions({ ...(drilldown?.filters ?? {}), limit: 50 }, drilldown !== null);
  const unavailableEnvelope = envelopeFromError(overview.error);

  return (
    <section className="space-y-4" data-testid="page-InsightOverview">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs font-semibold uppercase tracking-wide text-text-muted">Insight</p>
          <h1 className="text-xl font-semibold text-text-primary">Overview</h1>
        </div>
        <button
          type="button"
          onClick={() => setDrilldown({ label: 'All executions', filters: {} })}
          className="rounded border border-border-base bg-bg-elevated px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
        >
          Execution details
        </button>
      </header>

      {overview.isLoading && <StatePanel testId="insight-loading" title="Loading Insight overview" />}

      {overview.data && (
        <>
          <WindowBar data={overview.data} />
          {overview.data.freshness.state === 'stale' && (
            <StatePanel
              testId="insight-stale"
              tone="warn"
              title="Stale data"
              body="The projector is behind its freshness threshold."
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
                onOpen: () => setDrilldown({ label: a.display_name ?? a.agent_ref, filters: { agent_ref: a.agent_ref }, agent_ref: a.agent_ref }),
              }))}
              empty="No agent executions in the past 24 hours."
            />
            <Leaderboard
              title="Project leaderboard"
              rows={overview.data.projects.map((p) => ({
                id: p.project_id,
                name: p.name ?? p.project_id,
                summary: p.summary,
                onOpen: () => setDrilldown({ label: p.name ?? p.project_id, filters: { project_id: p.project_id }, project_id: p.project_id }),
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

      {overview.isError && !unavailableEnvelope && (
        <StatePanel
          testId={isAuthError(overview.error) ? 'insight-auth-error' : 'insight-error'}
          tone="danger"
          title={isAuthError(overview.error) ? 'Insight access is not authorized' : 'Insight overview failed'}
          body={overview.error instanceof Error ? overview.error.message : 'The overview request failed.'}
        />
      )}

      {unavailableEnvelope && (
        <>
          <WindowBar data={unavailableEnvelope} />
          <StatePanel
            testId={unavailableEnvelope.freshness.state === 'rebuilding' ? 'insight-rebuilding' : 'insight-unavailable'}
            tone="danger"
            title={unavailableEnvelope.freshness.state === 'rebuilding' ? 'Insight read model is rebuilding' : 'Insight is unavailable'}
            body={overview.error instanceof Error ? overview.error.message : 'The backend rejected the Insight request.'}
          />
        </>
      )}

      {drilldown && <ExecutionDrilldown title={drilldown.label} query={executions} onClose={() => setDrilldown(null)} />}
    </section>
  );
}

export function InsightExecutionDetailPage(): React.ReactElement {
  const { executionId } = useParams<{ executionId: string }>();
  const query = useInsightExecution(executionId);
  const isNotFound = query.error instanceof ApiError && query.error.status === 404;
  return (
    <section className="space-y-4" data-testid="page-InsightExecutionDetail">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <Link to="../../overview" relative="path" className="text-sm text-brand hover:underline">
            Insight overview
          </Link>
          <h1 className="mt-1 text-xl font-semibold text-text-primary">TaskExecution detail</h1>
          <p className="mt-1 font-mono text-xs text-text-muted">{executionId ?? EMPTY}</p>
        </div>
        <button
          type="button"
          onClick={() => void query.refetch()}
          disabled={!executionId || query.isFetching}
          className="rounded border border-border-base bg-bg-elevated px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle disabled:cursor-not-allowed disabled:opacity-60"
        >
          {query.isFetching ? 'Refreshing' : 'Refresh'}
        </button>
      </header>
      {query.isLoading && <StatePanel testId="insight-execution-loading" title="Loading execution detail" />}
      {isNotFound && (
        <StatePanel
          testId="insight-execution-not-found"
          title="Execution not found"
          body="This TaskExecution does not exist in the current organization Insight window."
        />
      )}
      {query.isError && !isNotFound && (
        <StatePanel
          testId={isAuthError(query.error) ? 'insight-execution-auth-error' : 'insight-execution-error'}
          tone="danger"
          title={isAuthError(query.error) ? 'Execution access is not authorized' : 'Execution detail failed'}
          body={query.error instanceof Error ? query.error.message : undefined}
        />
      )}
      {query.data && (
        <>
          <WindowBar data={query.data} />
          {query.data.freshness.state === 'stale' && <StatePanel testId="insight-execution-stale" tone="warn" title="Execution detail is stale" />}
          <div className="rounded border border-border-base bg-bg-elevated" data-testid="insight-execution-detail">
            <ExecutionTable rows={[query.data.execution]} />
          </div>
        </>
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
      <MetricCard label="Failure rate" value={formatRatio(summary.failure_rate)} sub={summary.failure_rate === null ? 'No completed executions' : 'Failed / completed'} />
      <MetricCard label="Slot utilization" value={formatRatio(summary.slot_utilization)} sub={`Coverage ${formatRatio(summary.slot_coverage_ratio)}`} />
      <MetricCard label="Queue wait" value={`p50 ${formatMs(summary.queue_wait_ms.p50)}`} sub={`p95 ${formatMs(summary.queue_wait_ms.p95)} / ${summary.queue_wait_ms.samples} samples`} />
      <MetricCard label="Execution duration" value={`p50 ${formatMs(summary.execution_duration_ms.p50)}`} sub={`p95 ${formatMs(summary.execution_duration_ms.p95)} / ${summary.execution_duration_ms.samples} samples`} />
    </div>
  );
}

function MetricCard({ label, value, sub }: { label: string; value: string; sub: string }): React.ReactElement {
  return (
    <div className="rounded border border-border-base bg-bg-elevated p-3">
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
  rows: Array<{ id: string; name: string; summary: InsightSummary; onOpen: () => void }>;
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
                <th className="px-3 py-2 font-medium">Queue p50/p95</th>
                <th className="px-3 py-2 font-medium">Duration p50/p95</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id} className="border-t border-border-base">
                  <td className="px-3 py-2">
                    <button type="button" onClick={row.onOpen} className="font-medium text-brand hover:underline">
                      {row.name}
                    </button>
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

function ExecutionDrilldown({
  title,
  query,
  onClose,
}: {
  title: string;
  query: ReturnType<typeof useInsightExecutions>;
  onClose: () => void;
}): React.ReactElement {
  return (
    <div className="rounded border border-border-base bg-bg-elevated" data-testid="insight-drilldown">
      <div className="flex items-center justify-between gap-3 border-b border-border-base px-3 py-2">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">TaskExecution details</h2>
          <p className="text-xs text-text-muted">{title}</p>
        </div>
        <button type="button" onClick={onClose} className="rounded px-2 py-1 text-sm text-text-secondary hover:bg-bg-subtle">
          Close
        </button>
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
          {query.data.freshness.state === 'stale' && <StatePanel testId="insight-drilldown-stale" tone="warn" title="Execution details are stale" />}
          {query.data.executions.length === 0 ? (
            <StatePanel testId="insight-drilldown-empty" title="No matching execution attempts" />
          ) : (
            <ExecutionTable rows={query.data.executions} />
          )}
        </>
      )}
    </div>
  );
}

function ExecutionTable({ rows }: { rows: InsightExecutionRow[] }): React.ReactElement {
  return (
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
          {rows.map((row) => <ExecutionRow key={row.execution_id} row={row} />)}
        </tbody>
      </table>
    </div>
  );
}

function ExecutionRow({ row }: { row: InsightExecutionRow }): React.ReactElement {
  return (
    <tr className="border-t border-border-base" data-testid="insight-execution-row">
      <td className="px-3 py-2 align-top">
        <Link to={`../executions/${encodeURIComponent(row.execution_id)}`} relative="path" className="font-mono text-brand hover:underline">
          {row.execution_id}
        </Link>
        <div className="font-mono text-text-muted">{row.command_id ?? EMPTY}</div>
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

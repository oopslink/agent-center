import type React from 'react';
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ApiError } from '@/api/client';
import {
  useInsightExecution,
  useInsightExecutions,
  useInsightOverview,
  type InsightExecutionFilters,
  type InsightExecutionRow,
  type InsightFreshness,
  type InsightOverview as InsightOverviewDTO,
  type InsightPercentiles,
  type InsightSummary,
} from '@/api/insights';

const EMPTY = '—';

export default function InsightOverview(): React.ReactElement {
  const { t } = useTranslation('insights');
  const { slug = '' } = useParams<{ slug: string }>();
  const base = `/organizations/${encodeURIComponent(slug)}/insights`;
  const overview = useInsightOverview();
  const unavailableEnvelope = envelopeFromError(overview.error);

  return (
    <section className="space-y-4" data-testid="page-InsightOverview">
      <InsightHeader title={t('insight.overview.title')} action={<LinkButton to={`${base}/executions?window=24h`}>{t('insight.actions.viewAll')}</LinkButton>} />

      {overview.isLoading && <StatePanel testId="insight-loading" title={t('insight.state.loadingOverview')} />}

      {overview.data && (
        <>
          <WindowBar data={overview.data} />
          <FreshnessNotice data={overview.data} />
          <SummaryCards summary={overview.data.summary} />
          <CoverageNotice summary={overview.data.summary} />
          <section className="grid gap-4 xl:grid-cols-2">
            <DimensionTable
              kind="agent"
              title={t('insight.overview.byAgent')}
              rows={overview.data.agents.map((a) => ({ id: a.agent_ref, name: a.display_name ?? a.agent_ref, summary: a.summary, to: `${base}/executions?window=24h&agent_ref=${encodeURIComponent(a.agent_ref)}` }))}
            />
            <DimensionTable
              kind="project"
              title={t('insight.overview.byProject')}
              rows={overview.data.projects.map((p) => ({ id: p.project_id, name: p.name ?? p.project_id, summary: p.summary, to: `${base}/executions?window=24h&project_id=${encodeURIComponent(p.project_id)}` }))}
            />
          </section>
          <MethodNote diagnostics={overview.data.diagnostics} />
          {isOverviewEmpty(overview.data) && <StatePanel testId="insight-empty" title={t('insight.state.emptyOverview')} body={t('insight.state.emptyOverviewBody')} />}
        </>
      )}

      {overview.isError && !unavailableEnvelope && <InsightError testIdPrefix="insight" error={overview.error} fallbackTitle={t('insight.state.overviewFailed')} />}

      {unavailableEnvelope && (
        <>
          <WindowBar data={unavailableEnvelope} />
          <StatePanel
            testId={unavailableEnvelope.freshness.state === 'rebuilding' ? 'insight-rebuilding' : 'insight-unavailable'}
            tone="danger"
            title={unavailableEnvelope.freshness.state === 'rebuilding' ? t('insight.state.rebuilding') : t('insight.state.unavailable')}
            body={overview.error instanceof Error ? overview.error.message : undefined}
          />
        </>
      )}
    </section>
  );
}

export function InsightExecutionsPage(): React.ReactElement {
  const { t } = useTranslation('insights');
  const { slug = '' } = useParams<{ slug: string }>();
  const base = `/organizations/${encodeURIComponent(slug)}/insights`;
  const [params, setParams] = useSearchParams();
  const filters = filtersFromParams(params);
  const query = useInsightExecutions({ ...filters, limit: 50 }, true);

  const removeFilter = (key: 'agent_ref' | 'project_id') => {
    const next = new URLSearchParams(params);
    next.delete(key);
    next.delete('cursor');
    next.set('window', '24h');
    setParams(next);
  };
  const clearFilters = () => setParams(new URLSearchParams({ window: '24h' }));
  const setCursor = (cursor: string | null) => {
    const next = new URLSearchParams(params);
    next.set('window', '24h');
    if (cursor) next.set('cursor', cursor);
    else next.delete('cursor');
    setParams(next);
  };

  return (
    <section className="space-y-4" data-testid="page-InsightExecutions">
      <InsightHeader title={t('insight.executions.title')} subtitle={t('insight.executions.subtitle')} />
      <FilterSummary filters={filters} onRemove={removeFilter} onClear={clearFilters} />
      {query.isLoading && <StatePanel testId="insight-executions-loading" title={t('insight.state.loadingExecutions')} />}
      {query.isError && <InsightError testIdPrefix="insight-executions" error={query.error} fallbackTitle={t('insight.state.executionsFailed')} />}
      {query.data && (
        <>
          <WindowBar data={query.data} />
          <FreshnessNotice data={query.data} />
          {query.data.executions.length === 0 ? (
            <StatePanel testId="insight-executions-empty" title={hasExecutionFilter(filters) ? t('insight.state.emptyFiltered') : t('insight.state.emptyList')} />
          ) : (
            <TaskExecutionTable rows={query.data.executions} base={base} />
          )}
          <div className="flex items-center justify-between gap-3 text-sm">
            <button type="button" className="rounded border border-border-base px-3 py-1.5 text-text-primary disabled:opacity-50" disabled={!filters.cursor} onClick={() => setCursor(null)}>
              {t('insight.actions.previous')}
            </button>
            <button type="button" className="rounded border border-border-base px-3 py-1.5 text-text-primary disabled:opacity-50" disabled={!query.data.next_cursor} onClick={() => setCursor(query.data?.next_cursor ?? null)}>
              {t('insight.actions.next')}
            </button>
          </div>
        </>
      )}
    </section>
  );
}

export function InsightExecutionDetailPage(): React.ReactElement {
  const { t } = useTranslation('insights');
  const { executionId, slug = '' } = useParams<{ executionId: string; slug: string }>();
  const base = `/organizations/${encodeURIComponent(slug)}/insights`;
  const location = useLocation();
  const query = useInsightExecution(executionId);
  const isNotFound = query.error instanceof ApiError && query.error.status === 404;
  const listBack = typeof location.state === 'object' && location.state && 'from' in location.state ? String((location.state as { from?: string }).from ?? '') : `${base}/executions?window=24h`;

  return (
    <section className="space-y-4" data-testid="page-InsightExecutionDetail">
      <InsightHeader
        title={t('insight.detail.title')}
        subtitle={executionId ?? EMPTY}
        action={<button type="button" onClick={() => void query.refetch()} disabled={!executionId || query.isFetching} className="rounded border border-border-base bg-bg-elevated px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle disabled:opacity-60">{query.isFetching ? t('insight.actions.refreshing') : t('insight.actions.refresh')}</button>}
      />
      <nav className="flex flex-wrap gap-2 text-sm" aria-label={t('insight.breadcrumb')}>
        <Link to={`${base}/overview`} className="text-brand hover:underline">{t('insight.overview.title')}</Link>
        <span className="text-text-muted">/</span>
        <Link to={listBack} className="text-brand hover:underline">{t('insight.executions.title')}</Link>
      </nav>
      {query.isLoading && <StatePanel testId="insight-execution-loading" title={t('insight.state.loadingDetail')} />}
      {isNotFound && <StatePanel testId="insight-execution-not-found" title={t('insight.state.detailMissing')} body={t('insight.state.detailMissingBody')} />}
      {query.isError && !isNotFound && <InsightError testIdPrefix="insight-execution" error={query.error} fallbackTitle={t('insight.state.detailFailed')} />}
      {query.data && (
        <>
          <WindowBar data={query.data} />
          <FreshnessNotice data={query.data} />
          <ExecutionDetail execution={query.data.execution} />
        </>
      )}
    </section>
  );
}

function InsightHeader({ title, subtitle, action }: { title: string; subtitle?: string; action?: React.ReactNode }): React.ReactElement {
  return (
    <header className="flex flex-wrap items-start justify-between gap-3">
      <div>
        <p className="text-xs font-semibold uppercase tracking-wide text-text-muted">Insight</p>
        <h1 className="text-xl font-semibold text-text-primary">{title}</h1>
        {subtitle && <p className="mt-1 text-sm text-text-secondary">{subtitle}</p>}
      </div>
      {action}
    </header>
  );
}

function LinkButton({ to, children }: { to: string; children: React.ReactNode }): React.ReactElement {
  return <Link to={to} className="rounded border border-border-base bg-bg-elevated px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle">{children}</Link>;
}

function WindowBar({ data }: { data: Pick<InsightOverviewDTO, 'window' | 'refreshed_at' | 'freshness'> }): React.ReactElement {
  const { t } = useTranslation('insights');
  return (
    <div className="flex flex-wrap items-center gap-3 rounded border border-border-base bg-bg-elevated px-3 py-2 text-xs text-text-secondary" data-testid="insight-window">
      <strong className="text-text-primary">{t('insight.window.title')}</strong>
      <span title={data.window.start}>{formatWindowTime(data.window.start)}</span>
      <span aria-hidden="true">–</span>
      <span title={data.window.end}>{formatWindowTime(data.window.end)}</span>
      <span>{t('insight.window.localTz', { tz: localUTCOffset() })}</span>
      <span className="text-text-muted">{t('insight.window.refreshed', { time: formatClock(data.refreshed_at) })}</span>
      <FreshnessBadge freshness={data.freshness} />
    </div>
  );
}

function FreshnessBadge({ freshness }: { freshness: InsightFreshness }): React.ReactElement {
  const { t } = useTranslation('insights');
  const tone = freshness.state === 'fresh' ? 'border-success/30 bg-success/10 text-success' : freshness.state === 'stale' ? 'border-warning/30 bg-warning/10 text-warning' : 'border-danger/30 bg-danger/10 text-danger';
  return <span className={`rounded-full border px-2 py-0.5 font-medium ${tone}`} title={`${humanDuration(freshness.age_ms)} / ${humanDuration(freshness.threshold_ms)}`} data-testid="insight-freshness">{t(`insight.freshness.${freshness.state}`)}</span>;
}

function FreshnessNotice({ data }: { data: Pick<InsightOverviewDTO, 'freshness' | 'refreshed_at'> }): React.ReactElement | null {
  const { t } = useTranslation('insights');
  if (data.freshness.state === 'fresh') return null;
  if (data.freshness.state === 'stale') {
    return <StatePanel testId="insight-stale" tone="warn" title={t('insight.state.stale')} body={t('insight.state.staleBody', { time: formatClock(data.refreshed_at) })} />;
  }
  if (data.freshness.state === 'rebuilding') {
    return <StatePanel testId="insight-rebuilding" tone="danger" title={t('insight.state.rebuilding')} body={t('insight.state.staleBody', { time: formatClock(data.refreshed_at) })} />;
  }
  if (data.freshness.state === 'unavailable') {
    return <StatePanel testId="insight-unavailable" tone="danger" title={t('insight.state.unavailable')} body={t('insight.state.staleBody', { time: formatClock(data.refreshed_at) })} />;
  }
  return <StatePanel testId="insight-freshness-unknown" tone="danger" title={t('insight.state.unknownFreshness')} body={t('insight.state.staleBody', { time: formatClock(data.refreshed_at) })} />;
}

function SummaryCards({ summary }: { summary: InsightSummary }): React.ReactElement {
  const { t } = useTranslation('insights');
  const coverage = classifyCoverage(summary.slot_coverage_ratio, summary.slot_utilization, t);
  return (
    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-5" data-testid="insight-summary">
      <MetricCard label={t('insight.metric.completed')} value={String(summary.completed_executions)} sub={t('insight.metric.completedHint')} />
      <MetricCard label={t('insight.metric.failureRate')} value={formatRatio(summary.failure_rate)} sub={summary.failure_rate === null ? t('insight.metric.noCompleted') : t('insight.metric.failureFormula', { failed: summary.failed_executions, completed: summary.completed_executions })} />
      <MetricCard label={t('insight.metric.utilization')} value={coverage.value} sub={coverage.sub} tone={coverage.tone} testId="insight-utilization-card" />
      <PercentileCard label={t('insight.metric.queue')} data={summary.queue_wait_ms} sampleText={t('insight.metric.queueSamples', { count: summary.queue_wait_ms.samples })} />
      <PercentileCard label={t('insight.metric.duration')} data={summary.execution_duration_ms} sampleText={t('insight.metric.durationSamples', { count: summary.execution_duration_ms.samples })} />
    </div>
  );
}

function MetricCard({ label, value, sub, tone = 'normal', testId }: { label: string; value: string; sub: string; tone?: 'normal' | 'unknown' | 'warn'; testId?: string }): React.ReactElement {
  const cls = tone === 'warn' ? 'border-warning/40' : tone === 'unknown' ? 'border-text-muted/30' : 'border-border-base';
  return (
    <div className={`rounded border ${cls} bg-bg-elevated p-3`} data-testid={testId}>
      <div className="text-xs font-medium text-text-muted">{label}</div>
      <div className="mt-1 text-2xl font-semibold tabular-nums text-text-primary">{value}</div>
      <div className="mt-1 text-xs text-text-secondary">{sub}</div>
    </div>
  );
}

function PercentileCard({ label, data, sampleText }: { label: string; data: InsightPercentiles; sampleText: string }): React.ReactElement {
  const { t } = useTranslation('insights');
  const value = data.samples === 0 ? EMPTY : humanDuration(data.p50);
  const tail = data.samples === 0 ? t('insight.metric.noSamples') : `${t('insight.metric.p95')}: ${humanDuration(data.p95)}`;
  return <MetricCard label={label} value={`${t('insight.metric.p50')}: ${value}`} sub={`${tail} · ${sampleText}`} />;
}

function CoverageNotice({ summary }: { summary: InsightSummary }): React.ReactElement | null {
  const { t } = useTranslation('insights');
  const c = classifyCoverage(summary.slot_coverage_ratio, summary.slot_utilization, t);
  if (c.kind === 'representative') return null;
  return <StatePanel testId="insight-coverage-notice" tone={c.kind === 'partial' ? 'warn' : 'muted'} title={t('insight.coverage.noticeTitle')} body={c.sub} />;
}

function DimensionTable({ title, kind, rows }: { title: string; kind: 'agent' | 'project'; rows: Array<{ id: string; name: string; summary: InsightSummary; to: string }> }): React.ReactElement {
  const { t } = useTranslation('insights');
  return (
    <section className="rounded border border-border-base bg-bg-elevated" data-testid={`insight-${kind}-table`}>
      <div className="border-b border-border-base px-3 py-2">
        <h2 className="text-sm font-semibold text-text-primary">{title}</h2>
        <p className="text-xs text-text-muted">{t('insight.overview.tableSubtitle')}</p>
      </div>
      {rows.length === 0 ? <p className="p-3 text-sm text-text-muted">{t('insight.state.emptyDimension')}</p> : (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wide text-text-muted">
              <tr>
                <th className="px-3 py-2 font-medium">{t('insight.table.name')}</th>
                <th className="px-3 py-2 font-medium">{t('insight.table.completed')}</th>
                <th className="px-3 py-2 font-medium">{t('insight.table.failure')}</th>
                <th className="px-3 py-2 font-medium">{t('insight.table.queue')}</th>
                <th className="px-3 py-2 font-medium">{t('insight.table.duration')}</th>
                <th className="px-3 py-2 font-medium">{t('insight.table.action')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id} className="border-t border-border-base">
                  <td className="px-3 py-2">
                    <div className="font-medium text-text-primary">{row.name}</div>
                    <div className="font-mono text-xs text-text-muted">{row.id}</div>
                  </td>
                  <td className="px-3 py-2 tabular-nums">{t('insight.table.completedValue', { count: row.summary.completed_executions })}</td>
                  <td className="px-3 py-2 tabular-nums">{formatFailure(row.summary)}</td>
                  <td className="px-3 py-2 tabular-nums">{formatPercentiles(row.summary.queue_wait_ms)}</td>
                  <td className="px-3 py-2 tabular-nums">{formatPercentiles(row.summary.execution_duration_ms)}</td>
                  <td className="px-3 py-2"><Link to={row.to} className="text-brand hover:underline">{t('insight.actions.viewExecutions')}</Link></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function MethodNote({ diagnostics }: { diagnostics: { invalid_facts: number; late_events: number } }): React.ReactElement {
  const { t } = useTranslation('insights');
  return (
    <div className="space-y-1 rounded border border-border-base bg-bg-elevated p-3 text-xs text-text-secondary" data-testid="insight-method-note">
      <p>{t('insight.method.base')}</p>
      {diagnostics.invalid_facts > 0 && <p>{t('insight.method.invalidFacts', { count: diagnostics.invalid_facts })}</p>}
      {diagnostics.late_events > 0 && <p>{t('insight.method.lateEvents', { count: diagnostics.late_events })}</p>}
    </div>
  );
}

function FilterSummary({ filters, onRemove, onClear }: { filters: InsightExecutionFilters; onRemove: (key: 'agent_ref' | 'project_id') => void; onClear: () => void }): React.ReactElement {
  const { t } = useTranslation('insights');
  return (
    <div className="flex flex-wrap items-center gap-2 text-sm" data-testid="insight-filter-summary">
      <span className="text-text-muted">{t('insight.window.title')}</span>
      {filters.agent_ref && <FilterChip label={t('insight.filters.agent', { value: filters.agent_ref })} onClick={() => onRemove('agent_ref')} />}
      {filters.project_id && <FilterChip label={t('insight.filters.project', { value: filters.project_id })} onClick={() => onRemove('project_id')} />}
      {hasExecutionFilter(filters) && <button type="button" onClick={onClear} className="text-brand hover:underline">{t('insight.filters.clear')}</button>}
    </div>
  );
}

function FilterChip({ label, onClick }: { label: string; onClick: () => void }): React.ReactElement {
  return <button type="button" onClick={onClick} className="rounded-full border border-border-base bg-bg-elevated px-2 py-1 text-xs text-text-primary">{label} ×</button>;
}

function TaskExecutionTable({ rows, base }: { rows: InsightExecutionRow[]; base: string }): React.ReactElement {
  const { t } = useTranslation('insights');
  const location = useLocation();
  return (
    <div className="overflow-x-auto rounded border border-border-base bg-bg-elevated" data-testid="insight-execution-table">
      <table className="w-full min-w-[58rem] text-left text-sm">
        <thead className="text-xs uppercase tracking-wide text-text-muted">
          <tr>
            <th className="px-3 py-2 font-medium">{t('insight.execution.status')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.execution.task')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.execution.agent')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.execution.queued')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.execution.started')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.execution.finished')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.execution.queueWait')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.execution.duration')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.execution.dataHint')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.execution_id} className="border-t border-border-base" data-testid="insight-execution-row">
              <td className="px-3 py-2 align-top"><ExecutionStatusBadge row={row} /></td>
              <td className="px-3 py-2 align-top">
                <Link to={`${base}/executions/${encodeURIComponent(row.execution_id)}`} state={{ from: `${location.pathname}${location.search}` }} className="font-medium text-brand hover:underline">{row.task_title ?? row.task_ref ?? row.task_id ?? row.execution_id}</Link>
                <div className="font-mono text-xs text-text-muted">{row.project_name ?? row.project_id ?? EMPTY}</div>
              </td>
              <td className="px-3 py-2 align-top">{row.agent_name ?? row.agent_ref}</td>
              <td className="px-3 py-2 align-top tabular-nums">{timeOrLabel(row.queued_at, EMPTY)}</td>
              <td className="px-3 py-2 align-top tabular-nums">{timeOrLabel(row.started_at, t('insight.execution.notStarted'))}</td>
              <td className="px-3 py-2 align-top tabular-nums">{timeOrLabel(row.finished_at, t('insight.execution.notFinished'))}</td>
              <td className="px-3 py-2 align-top tabular-nums">{humanDuration(row.queue_wait_ms)}</td>
              <td className="px-3 py-2 align-top tabular-nums">{humanDuration(row.duration_ms)}</td>
              <td className="px-3 py-2 align-top"><QualityBadge quality={row.quality} /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ExecutionDetail({ execution }: { execution: InsightExecutionRow }): React.ReactElement {
  const { t } = useTranslation('insights');
  const status = executionStatus(execution, t);
  const reason = failureMessage(execution, t);
  return (
    <article className="space-y-4 rounded border border-border-base bg-bg-elevated p-4" data-testid="insight-execution-detail">
      <div className="flex flex-wrap items-center gap-2">
        <ExecutionStatusBadge row={execution} />
        {execution.recovered && <span className="rounded-full border border-warning/30 bg-warning/10 px-2 py-0.5 text-xs font-medium text-warning">{t('insight.execution.recovered')}</span>}
      </div>
      <h2 className="text-lg font-semibold text-text-primary">{execution.task_title ?? execution.task_ref ?? execution.execution_id}</h2>
      <p className="text-sm text-text-secondary">{[execution.project_name, execution.agent_name ?? execution.agent_ref].filter(Boolean).join(' · ')}</p>

      <section>
        <h3 className="text-sm font-semibold text-text-primary">{t('insight.detail.timeline')}</h3>
        <div className="mt-2 grid gap-2 text-sm text-text-secondary md:grid-cols-5">
          <TimelineCell label={t('insight.execution.queued')} value={timeOrLabel(execution.queued_at, EMPTY)} />
          <TimelineCell label={t('insight.execution.queueWait')} value={humanDuration(execution.queue_wait_ms)} />
          <TimelineCell label={t('insight.execution.started')} value={timeOrLabel(execution.started_at, t('insight.execution.notStarted'))} />
          <TimelineCell label={t('insight.execution.duration')} value={humanDuration(execution.duration_ms)} />
          <TimelineCell label={t('insight.execution.finished')} value={timeOrLabel(execution.finished_at, t('insight.execution.notFinished'))} />
        </div>
      </section>

      <section>
        <h3 className="text-sm font-semibold text-text-primary">{t('insight.detail.result')}</h3>
        <p className="mt-1 text-sm text-text-secondary">{status.label}</p>
        {reason && <p className="mt-1 text-sm text-text-secondary">{reason}</p>}
      </section>

      <dl className="grid gap-3 text-sm md:grid-cols-2">
        <Info label={t('insight.detail.task')} value={execution.task_title ?? execution.task_ref ?? execution.task_id ?? EMPTY} />
        <Info label={t('insight.detail.project')} value={execution.project_name ?? execution.project_id ?? EMPTY} />
        <Info label={t('insight.detail.agent')} value={execution.agent_name ?? execution.agent_ref} />
        <Info label={t('insight.detail.worker')} value={execution.worker_id ?? EMPTY} />
        <Info label="Execution ID" value={execution.execution_id} mono />
        <Info label="Command ID" value={execution.command_id ?? EMPTY} mono />
      </dl>

      {execution.quality !== 'valid' && <StatePanel testId="insight-execution-quality" tone="warn" title={qualityLabel(execution.quality, t)} body={t('insight.quality.invalidBody')} />}
      <details className="text-xs text-text-muted">
        <summary className="cursor-pointer text-text-secondary">{t('insight.detail.technical')}</summary>
        <pre className="mt-2 overflow-x-auto rounded bg-bg-subtle p-2">{JSON.stringify({ outcome: execution.outcome, failure_reason: execution.failure_reason, command_status: execution.command_status, status_reason: execution.status_reason, quality: execution.quality }, null, 2)}</pre>
      </details>
    </article>
  );
}

function TimelineCell({ label, value }: { label: string; value: string }): React.ReactElement {
  return <div className="rounded border border-border-base bg-bg-subtle p-2"><div className="text-xs text-text-muted">{label}</div><div className="mt-1 font-medium text-text-primary">{value}</div></div>;
}

function Info({ label, value, mono }: { label: string; value: string; mono?: boolean }): React.ReactElement {
  return <div><dt className="text-xs text-text-muted">{label}</dt><dd className={`mt-1 text-text-primary ${mono ? 'font-mono text-xs' : ''}`}>{value}</dd></div>;
}

function ExecutionStatusBadge({ row }: { row: InsightExecutionRow }): React.ReactElement {
  const { t } = useTranslation('insights');
  const status = executionStatus(row, t);
  return <span className={`rounded-full border px-2 py-0.5 text-xs font-medium ${status.className}`}>{status.label}</span>;
}

function QualityBadge({ quality }: { quality: string }): React.ReactElement | null {
  const { t } = useTranslation('insights');
  if (quality === 'valid') return null;
  return <span className="rounded-full border border-warning/30 bg-warning/10 px-2 py-0.5 text-xs font-medium text-warning">{qualityLabel(quality, t)}</span>;
}

function StatePanel({ testId, title, body, tone = 'muted' }: { testId: string; title: string; body?: string; tone?: 'muted' | 'warn' | 'danger' }): React.ReactElement {
  const cls = tone === 'danger' ? 'border-danger/30 bg-danger/5 text-danger' : tone === 'warn' ? 'border-warning/30 bg-warning/5 text-warning' : 'border-border-base bg-bg-elevated text-text-secondary';
  return <div className={`rounded border p-3 text-sm ${cls}`} data-testid={testId}><div className="font-medium">{title}</div>{body && <p className="mt-1 text-xs">{body}</p>}</div>;
}

function InsightError({ testIdPrefix, error, fallbackTitle }: { testIdPrefix: string; error: unknown; fallbackTitle: string }): React.ReactElement {
  const { t } = useTranslation('insights');
  const auth = isAuthError(error);
  return <StatePanel testId={auth ? `${testIdPrefix}-auth-error` : `${testIdPrefix}-error`} tone="danger" title={auth ? t('insight.state.unauthorized') : fallbackTitle} body={error instanceof Error ? error.message : undefined} />;
}

function filtersFromParams(params: URLSearchParams): InsightExecutionFilters {
  return {
    agent_ref: params.get('agent_ref') || undefined,
    project_id: params.get('project_id') || undefined,
    cursor: params.get('cursor') || undefined,
  };
}

function hasExecutionFilter(filters: InsightExecutionFilters): boolean {
  return Boolean(filters.agent_ref || filters.project_id);
}

function classifyCoverage(coverage: number | null, utilization: number | null, t: (key: string, options?: Record<string, unknown>) => string): { kind: 'unknown' | 'insufficient' | 'partial' | 'representative'; value: string; sub: string; tone: 'unknown' | 'warn' | 'normal' } {
  const coverageLabel = formatCoverage(coverage);
  if (coverage === null) return { kind: 'unknown', value: t('insight.coverage.unknown'), sub: t('insight.coverage.noBaseline'), tone: 'unknown' };
  if (coverage === 0) return { kind: 'unknown', value: t('insight.coverage.unknown'), sub: t('insight.coverage.noObservation'), tone: 'unknown' };
  if (coverage < 0.5) return { kind: 'insufficient', value: t('insight.coverage.insufficient'), sub: t('insight.coverage.insufficientSub', { coverage: coverageLabel }), tone: 'warn' };
  if (utilization === null) return { kind: 'unknown', value: t('insight.coverage.unknown'), sub: t('insight.coverage.noAvailable'), tone: 'unknown' };
  if (coverage < 0.9) return { kind: 'partial', value: `${formatRatio(utilization)} ${t('insight.coverage.partialSuffix')}`, sub: t('insight.coverage.partialSub', { coverage: coverageLabel }), tone: 'warn' };
  return { kind: 'representative', value: formatRatio(utilization), sub: t('insight.coverage.representativeSub', { coverage: coverageLabel }), tone: 'normal' };
}

function executionStatus(row: InsightExecutionRow, t: (key: string, options?: Record<string, unknown>) => string): { label: string; className: string } {
  if (row.outcome === 'succeeded') return { label: t('insight.status.completed'), className: 'border-success/30 bg-success/10 text-success' };
  if (row.outcome === 'failed') return { label: t('insight.status.failed'), className: 'border-danger/30 bg-danger/10 text-danger' };
  if (row.outcome === 'crashed') return { label: t('insight.status.interrupted'), className: 'border-danger/30 bg-danger/10 text-danger' };
  if (row.outcome === 'quiet_finalized') return { label: t('insight.status.quietFinalized'), className: 'border-danger/30 bg-danger/10 text-danger' };
  if (row.finished_at) return { label: t('insight.status.outcomeUnknown'), className: 'border-warning/30 bg-warning/10 text-warning' };
  if (row.started_at) return { label: t('insight.status.running'), className: 'border-brand/30 bg-brand/10 text-brand' };
  if (row.command_status === 'rejected' || row.command_status === 'failed' || row.command_status === 'expired') return { label: t('insight.status.didNotStart'), className: 'border-danger/30 bg-danger/10 text-danger' };
  if (row.queued_at) return { label: t('insight.status.waiting'), className: 'border-border-base bg-bg-subtle text-text-secondary' };
  return { label: t('insight.status.unknown'), className: 'border-warning/30 bg-warning/10 text-warning' };
}

function qualityLabel(quality: string, t: (key: string, options?: Record<string, unknown>) => string): string {
  if (quality === 'invalid_time_order') return t('insight.quality.invalidTime');
  return t('insight.quality.check');
}

function failureMessage(row: InsightExecutionRow, t: (key: string, options?: Record<string, unknown>) => string): string | null {
  if (row.failure_message) return row.failure_message;
  if (row.status_message && !row.started_at) return row.status_message;
  const reason = row.failure_reason || row.status_reason;
  if (!reason) return null;
  const key = reasonMap[reason] ?? 'generic';
  return t(`insight.reason.${key}`);
}

const reasonMap: Record<string, string> = {
  nonzero_exit: 'nonzeroExit',
  output_failure: 'outputFailure',
  status_failed: 'outputFailure',
  process_gone: 'processGone',
  clean_exit_no_output: 'cleanExitNoOutput',
  done_no_output: 'cleanExitNoOutput',
  stalled: 'stalled',
  non_delivery: 'nonDelivery',
  evidence_persistence: 'evidencePersistence',
  repo_source_unavailable: 'repoSourceUnavailable',
  no_backfill_guard: 'noBackfillGuard',
};

function formatFailure(summary: InsightSummary): string {
  if (summary.completed_executions === 0 || summary.failure_rate === null) return `${EMPTY} (0)`;
  return `${summary.failed_executions}/${summary.completed_executions} (${formatRatio(summary.failure_rate)})`;
}

function formatPercentiles(data: InsightPercentiles): string {
  if (data.samples === 0) return `${EMPTY} · 0`;
  return `P50 ${humanDuration(data.p50)} / P95 ${humanDuration(data.p95)} · ${data.samples}`;
}

function formatRatio(value: number | null): string {
  if (value === null) return EMPTY;
  return `${Math.round(value * 1000) / 10}%`;
}

function formatCoverage(value: number | null): string {
  if (value === null) return EMPTY;
  if (value > 0 && value < 0.001) return '<0.1%';
  return `${Math.round(value * 1000) / 10}%`;
}

function humanDuration(value: number | null): string {
  if (value === null) return EMPTY;
  if (value < 0) return 'Invalid time data';
  if (value < 1000) return `${Math.round(value)} ms`;
  const seconds = value / 1000;
  if (seconds < 60) return `${Math.round(seconds * 10) / 10}`.replace(/\.0$/, '') + ' s';
  const minutes = Math.floor(seconds / 60);
  const wholeSeconds = Math.round(seconds % 60);
  if (minutes < 60) return wholeSeconds === 0 ? `${minutes} min` : `${minutes} min ${String(wholeSeconds).padStart(2, '0')} s`;
  const hours = Math.floor(minutes / 60);
  const remMinutes = minutes % 60;
  if (hours < 24) return `${hours} h ${String(remMinutes).padStart(2, '0')} min`;
  const days = Math.floor(hours / 24);
  return `${days} d ${hours % 24} h`;
}

function formatWindowTime(value: string): string {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(d);
}

function formatClock(value: string): string {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(d);
}

function timeOrLabel(value: string | null, label: string): string {
  return value ? formatClock(value) : label;
}

function localUTCOffset(): string {
  const minutes = -new Date().getTimezoneOffset();
  const sign = minutes >= 0 ? '+' : '-';
  const abs = Math.abs(minutes);
  return `UTC${sign}${String(Math.floor(abs / 60)).padStart(2, '0')}:${String(abs % 60).padStart(2, '0')}`;
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
  return { window: envelope.window, refreshed_at: envelope.refreshed_at, freshness: envelope.freshness };
}

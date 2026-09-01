import React, { useState } from 'react';
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ApiError } from '@/api/client';
import { ChartPanel, DonutChart, HorizontalBars, SegmentedBar, type ChartDatum } from '@/components/insight/InsightCharts';
import { InsightExecutionStatusBadge, InsightFreshnessBadge, InsightQualityBadge, InsightStatePanel } from '@/components/insight/InsightPresentation';
import {
  useInsightExecution,
  useInsightExecutions,
  useInsightOverview,
  type InsightExecutionFilters,
  type InsightExecutionRow,
  type InsightOverview as InsightOverviewDTO,
  type InsightPercentiles,
  type InsightSummary,
} from '@/api/insights';
import {
  INSIGHT_EMPTY,
  classifyInsightCoverage,
  formatInsightClock,
  formatInsightDuration,
  formatInsightFailure,
  formatInsightPercentiles,
  formatInsightRatio,
  formatInsightTimeOrLabel,
  formatInsightWindowTime,
  insightExecutionStatus,
  insightFailureMessage,
  insightQualityLabel,
  localInsightUTCOffset,
} from '@/utils/insightPresentation';

const EMPTY = INSIGHT_EMPTY;

export default function InsightOverview(): React.ReactElement {
  const { t } = useTranslation('insights');
  const { slug = '' } = useParams<{ slug: string }>();
  const base = `/organizations/${encodeURIComponent(slug)}/insights`;
  const overview = useInsightOverview();
  const unavailableEnvelope = envelopeFromError(overview.error);

  return (
    <section className="space-y-4" data-testid="page-InsightOverview">
      <InsightHeader title={t('insight.overview.title')} />

      {overview.isLoading && <InsightStatePanel testId="insight-loading" title={t('insight.state.loadingOverview')} />}

      {overview.data && (
        <>
          <WindowBar data={overview.data} />
          <FreshnessNotice data={overview.data} />
          <SummaryCards summary={overview.data.summary} />
          <CoverageNotice summary={overview.data.summary} />
          <OverviewCharts data={overview.data} base={base} />
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
          {isOverviewEmpty(overview.data) && <InsightStatePanel testId="insight-empty" title={t('insight.state.emptyOverview')} body={t('insight.state.emptyOverviewBody')} />}
        </>
      )}

      {overview.isError && !unavailableEnvelope && <InsightError testIdPrefix="insight" error={overview.error} fallbackTitle={t('insight.state.overviewFailed')} />}

      {unavailableEnvelope && (
        <>
          <WindowBar data={unavailableEnvelope} />
          <InsightStatePanel
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
  const unavailableEnvelope = envelopeFromError(query.error);

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
      {query.isLoading && <InsightStatePanel testId="insight-executions-loading" title={t('insight.state.loadingExecutions')} />}
      {query.isError && !unavailableEnvelope && <InsightError testIdPrefix="insight-executions" error={query.error} fallbackTitle={t('insight.state.executionsFailed')} />}
      {unavailableEnvelope && (
        <>
          <WindowBar data={unavailableEnvelope} />
          <InsightStatePanel
            testId={unavailableEnvelope.freshness.state === 'rebuilding' ? 'insight-executions-rebuilding' : 'insight-executions-unavailable'}
            tone="danger"
            title={unavailableEnvelope.freshness.state === 'rebuilding' ? t('insight.state.rebuilding') : t('insight.state.unavailable')}
            body={query.error instanceof Error ? query.error.message : undefined}
          />
        </>
      )}
      {query.data && (
        <>
          <WindowBar data={query.data} />
          <FreshnessNotice data={query.data} />
          {query.data.executions.length === 0 ? (
            <InsightStatePanel testId="insight-executions-empty" title={hasExecutionFilter(filters) ? t('insight.state.emptyFiltered') : t('insight.state.emptyList')} />
          ) : (
            <>
              <ExecutionListCharts rows={query.data.executions} />
              <TaskExecutionTable rows={query.data.executions} base={base} />
            </>
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
  const unavailableEnvelope = envelopeFromError(query.error);
  const stateBack = typeof location.state === 'object' && location.state && 'from' in location.state ? String((location.state as { from?: string }).from ?? '') : '';
  const listBack = stateBack || listHrefFromDetailSearch(location.search, base);

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
      {query.isLoading && <InsightStatePanel testId="insight-execution-loading" title={t('insight.state.loadingDetail')} />}
      {isNotFound && <InsightStatePanel testId="insight-execution-not-found" title={t('insight.state.detailMissing')} body={t('insight.state.detailMissingBody')} />}
      {query.isError && !isNotFound && !unavailableEnvelope && <InsightError testIdPrefix="insight-execution" error={query.error} fallbackTitle={t('insight.state.detailFailed')} />}
      {unavailableEnvelope && (
        <>
          <WindowBar data={unavailableEnvelope} />
          <InsightStatePanel
            testId={unavailableEnvelope.freshness.state === 'rebuilding' ? 'insight-execution-rebuilding' : 'insight-execution-unavailable'}
            tone="danger"
            title={unavailableEnvelope.freshness.state === 'rebuilding' ? t('insight.state.rebuilding') : t('insight.state.unavailable')}
            body={query.error instanceof Error ? query.error.message : undefined}
          />
        </>
      )}
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

function WindowBar({ data }: { data: Pick<InsightOverviewDTO, 'window' | 'refreshed_at' | 'freshness'> }): React.ReactElement {
  const { t } = useTranslation('insights');
  return (
    <div className="flex flex-wrap items-center gap-3 rounded border border-border-base bg-bg-elevated px-3 py-2 text-xs text-text-secondary" data-testid="insight-window">
      <strong className="text-text-primary">{t('insight.window.title')}</strong>
      <span title={data.window.start}>{formatInsightWindowTime(data.window.start)}</span>
      <span aria-hidden="true">–</span>
      <span title={data.window.end}>{formatInsightWindowTime(data.window.end)}</span>
      <span>{t('insight.window.localTz', { tz: localInsightUTCOffset() })}</span>
      <span className="text-text-muted">{t('insight.window.refreshed', { time: formatInsightClock(data.refreshed_at) })}</span>
      <InsightFreshnessBadge freshness={data.freshness} />
    </div>
  );
}

function FreshnessNotice({ data }: { data: Pick<InsightOverviewDTO, 'freshness' | 'refreshed_at'> }): React.ReactElement | null {
  const { t } = useTranslation('insights');
  if (data.freshness.state === 'fresh') return null;
  if (data.freshness.state === 'stale') {
    return <InsightStatePanel testId="insight-stale" tone="warn" title={t('insight.state.stale')} body={t('insight.state.staleBody', { time: formatInsightClock(data.refreshed_at) })} />;
  }
  if (data.freshness.state === 'rebuilding') {
    return <InsightStatePanel testId="insight-rebuilding" tone="danger" title={t('insight.state.rebuilding')} body={t('insight.state.staleBody', { time: formatInsightClock(data.refreshed_at) })} />;
  }
  if (data.freshness.state === 'unavailable') {
    return <InsightStatePanel testId="insight-unavailable" tone="danger" title={t('insight.state.unavailable')} body={t('insight.state.staleBody', { time: formatInsightClock(data.refreshed_at) })} />;
  }
  return <InsightStatePanel testId="insight-freshness-unknown" tone="danger" title={t('insight.state.unknownFreshness')} body={t('insight.state.staleBody', { time: formatInsightClock(data.refreshed_at) })} />;
}

function SummaryCards({ summary }: { summary: InsightSummary }): React.ReactElement {
  const { t } = useTranslation('insights');
  const coverage = classifyInsightCoverage(summary.slot_coverage_ratio, summary.slot_utilization, t);
  return (
    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-5" data-testid="insight-summary">
      <MetricCard label={t('insight.metric.completed')} value={String(summary.completed_executions)} sub={t('insight.metric.completedHint')} />
      <MetricCard label={t('insight.metric.failureRate')} value={formatInsightRatio(summary.failure_rate)} sub={summary.failure_rate === null ? t('insight.metric.noCompleted') : t('insight.metric.failureFormula', { failed: summary.failed_executions, completed: summary.completed_executions })} />
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
  const value = data.samples === 0 ? EMPTY : formatInsightDuration(data.p50, t);
  const tail = data.samples === 0 ? t('insight.metric.noSamples') : `${t('insight.metric.p95')}: ${formatInsightDuration(data.p95, t)}`;
  return <MetricCard label={label} value={`${t('insight.metric.p50')}: ${value}`} sub={`${tail} · ${sampleText}`} />;
}

function CoverageNotice({ summary }: { summary: InsightSummary }): React.ReactElement | null {
  const { t } = useTranslation('insights');
  const c = classifyInsightCoverage(summary.slot_coverage_ratio, summary.slot_utilization, t);
  if (c.kind === 'representative') return null;
  return <InsightStatePanel testId="insight-coverage-notice" tone={c.kind === 'partial' ? 'warn' : 'neutral'} title={t('insight.coverage.noticeTitle')} body={c.sub} />;
}

function OverviewCharts({ data, base }: { data: InsightOverviewDTO; base: string }): React.ReactElement {
  const { t } = useTranslation('insights');
  const recovery = recoveryFinalized(data.summary);
  const failed = Math.max(0, data.summary.failed_executions);
  const completed = Math.max(0, data.summary.completed_executions);
  const healthy = Math.max(0, completed - failed - recovery);
  const outcomeData: ChartDatum[] = [
    { key: 'healthy', label: t('insight.chart.outcomeHealthy'), value: healthy, tone: 'success' },
    { key: 'failed', label: t('insight.chart.outcomeFailed'), value: failed, tone: 'danger' },
    { key: 'recovery', label: t('insight.chart.outcomeRecovery'), value: recovery, tone: 'warning' },
  ];
  const agents = data.agents
    .slice()
    .sort((a, b) => b.summary.completed_executions - a.summary.completed_executions || (a.display_name ?? a.agent_ref).localeCompare(b.display_name ?? b.agent_ref))
    .slice(0, 6)
    .map((agent) => ({
      key: agent.agent_ref,
      label: agent.display_name ?? agent.agent_ref,
      value: agent.summary.completed_executions,
      tone: riskTone(agent.summary),
      href: `${base}/executions?window=24h&agent_ref=${encodeURIComponent(agent.agent_ref)}`,
      detail: t('insight.chart.failureDetail', { failed: agent.summary.failed_executions, recovery: recoveryFinalized(agent.summary) }),
    }));
  const projects = data.projects
    .slice()
    .sort((a, b) => riskScore(b.summary) - riskScore(a.summary) || (a.name ?? a.project_id).localeCompare(b.name ?? b.project_id))
    .slice(0, 6)
    .map((project) => ({
      key: project.project_id,
      label: project.name ?? project.project_id,
      value: riskScore(project.summary),
      tone: riskTone(project.summary),
      href: `${base}/executions?window=24h&project_id=${encodeURIComponent(project.project_id)}`,
      detail: t('insight.chart.riskDetail', { completed: project.summary.completed_executions }),
    }));
  const latency = [
    { key: 'queue-p50', label: t('insight.chart.queueP50'), value: data.summary.queue_wait_ms.p50 ?? 0, tone: 'info' as const },
    { key: 'queue-p95', label: t('insight.chart.queueP95'), value: data.summary.queue_wait_ms.p95 ?? 0, tone: 'warning' as const },
    { key: 'duration-p50', label: t('insight.chart.durationP50'), value: data.summary.execution_duration_ms.p50 ?? 0, tone: 'success' as const },
    { key: 'duration-p95', label: t('insight.chart.durationP95'), value: data.summary.execution_duration_ms.p95 ?? 0, tone: 'danger' as const },
  ].map((item) => ({ ...item, detail: formatInsightDuration(item.value, t) }));

  return (
    <section className="grid gap-4 xl:grid-cols-4" data-testid="insight-overview-charts">
      <ChartPanel title={t('insight.chart.outcomeMix')} subtitle={t('insight.chart.outcomeSubtitle')}>
        <DonutChart data={outcomeData} totalLabel={t('insight.chart.executionsTotal')} />
      </ChartPanel>
      <ChartPanel title={t('insight.chart.agentVolume')} subtitle={t('insight.chart.agentVolumeSubtitle')}>
        <HorizontalBars data={agents} emptyLabel={t('insight.chart.empty')} />
      </ChartPanel>
      <ChartPanel title={t('insight.chart.projectRisk')} subtitle={t('insight.chart.projectRiskSubtitle')}>
        <HorizontalBars data={projects} emptyLabel={t('insight.chart.empty')} />
      </ChartPanel>
      <ChartPanel title={t('insight.chart.latencyShape')} subtitle={t('insight.chart.latencySubtitle')}>
        <HorizontalBars data={latency} emptyLabel={t('insight.chart.empty')} />
      </ChartPanel>
    </section>
  );
}

function ExecutionListCharts({ rows }: { rows: InsightExecutionRow[] }): React.ReactElement {
  const { t } = useTranslation('insights');
  const counts = new Map<string, number>();
  for (const row of rows) {
    const key = executionBucket(row);
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  return (
    <ChartPanel title={t('insight.chart.visibleExecutions')} subtitle={t('insight.chart.visibleExecutionsSubtitle')} testId="insight-execution-charts">
      <SegmentedBar
        emptyLabel={t('insight.chart.empty')}
        data={[
          { key: 'succeeded', label: t('insight.status.completed'), value: counts.get('succeeded') ?? 0, tone: 'success' },
          { key: 'failed', label: t('insight.status.failed'), value: counts.get('failed') ?? 0, tone: 'danger' },
          { key: 'quiet_finalized', label: t('insight.status.quietFinalized'), value: counts.get('quiet_finalized') ?? 0, tone: 'warning' },
          { key: 'running', label: t('insight.status.running'), value: counts.get('running') ?? 0, tone: 'info' },
          { key: 'unknown', label: t('insight.status.unknown'), value: counts.get('unknown') ?? 0, tone: 'neutral' },
        ]}
      />
    </ChartPanel>
  );
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
                  <td className="px-3 py-2 tabular-nums">{formatInsightFailure(row.summary, t)}</td>
                  <td className="px-3 py-2 tabular-nums">{formatInsightPercentiles(row.summary.queue_wait_ms, t)}</td>
                  <td className="px-3 py-2 tabular-nums">{formatInsightPercentiles(row.summary.execution_duration_ms, t)}</td>
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

function recoveryFinalized(summary: InsightSummary): number {
  return Math.max(0, summary.recovery_finalized_executions ?? 0);
}

function riskScore(summary: InsightSummary): number {
  return Math.max(0, summary.failed_executions) + recoveryFinalized(summary);
}

function riskTone(summary: InsightSummary): ChartDatum['tone'] {
  const score = riskScore(summary);
  if (score === 0) return 'success';
  return summary.failed_executions > 0 ? 'danger' : 'warning';
}

function executionBucket(row: InsightExecutionRow): string {
  if (row.outcome === 'succeeded') return 'succeeded';
  if (row.outcome === 'failed' || row.outcome === 'crashed') return 'failed';
  if (row.outcome === 'quiet_finalized') return 'quiet_finalized';
  if (!row.finished_at) return 'running';
  return 'unknown';
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
      <table className="w-full min-w-[34rem] text-left text-sm md:min-w-[44rem] lg:min-w-[58rem]">
        <thead className="text-xs uppercase tracking-wide text-text-muted">
          <tr>
            <th className="px-3 py-2 font-medium">{t('insight.execution.status')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.execution.task')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.execution.agent')}</th>
            <th className="hidden px-3 py-2 font-medium lg:table-cell">{t('insight.execution.queued')}</th>
            <th className="hidden px-3 py-2 font-medium lg:table-cell">{t('insight.execution.started')}</th>
            <th className="hidden px-3 py-2 font-medium lg:table-cell">{t('insight.execution.finished')}</th>
            <th className="hidden px-3 py-2 font-medium lg:table-cell">{t('insight.execution.queueWait')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.execution.duration')}</th>
            <th className="hidden px-3 py-2 font-medium md:table-cell">{t('insight.execution.dataHint')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.execution_id} className="border-t border-border-base" data-testid="insight-execution-row">
              <td className="px-3 py-2 align-top"><InsightExecutionStatusBadge row={row} /></td>
              <td className="px-3 py-2 align-top">
                <Link to={`${base}/executions/${encodeURIComponent(row.execution_id)}`} state={{ from: `${location.pathname}${location.search}` }} className="font-medium text-brand hover:underline">{row.task_title ?? row.task_ref ?? row.task_id ?? row.execution_id}</Link>
                <div className="font-mono text-xs text-text-muted">{row.project_name ?? row.project_id ?? EMPTY}</div>
              </td>
              <td className="px-3 py-2 align-top">
                <div>{row.agent_name ?? row.agent_ref}</div>
                {row.recovered && <span className="mt-1 inline-flex rounded-full border border-warning/30 bg-warning/10 px-2 py-0.5 text-xs font-medium text-warning">{t('insight.execution.recovered')}</span>}
              </td>
              <td className="hidden px-3 py-2 align-top tabular-nums lg:table-cell">{formatInsightTimeOrLabel(row.queued_at, EMPTY)}</td>
              <td className="hidden px-3 py-2 align-top tabular-nums lg:table-cell">{formatInsightTimeOrLabel(row.started_at, t('insight.execution.notStarted'))}</td>
              <td className="hidden px-3 py-2 align-top tabular-nums lg:table-cell">{formatInsightTimeOrLabel(row.finished_at, t('insight.execution.notFinished'))}</td>
              <td className="hidden px-3 py-2 align-top tabular-nums lg:table-cell">{formatInsightDuration(row.queue_wait_ms, t)}</td>
              <td className="px-3 py-2 align-top tabular-nums">{formatInsightDuration(row.duration_ms, t)}</td>
              <td className="hidden px-3 py-2 align-top md:table-cell"><InsightQualityBadge quality={row.quality} /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ExecutionDetail({ execution }: { execution: InsightExecutionRow }): React.ReactElement {
  const { t } = useTranslation('insights');
  const [showDiagnostics, setShowDiagnostics] = useState(false);
  const status = insightExecutionStatus(execution, t);
  const reason = insightFailureMessage(execution, t);
  return (
    <article className="space-y-4 rounded border border-border-base bg-bg-elevated p-4" data-testid="insight-execution-detail">
      <div className="flex flex-wrap items-center gap-2">
        <InsightExecutionStatusBadge row={execution} />
        {execution.recovered && <span className="rounded-full border border-warning/30 bg-warning/10 px-2 py-0.5 text-xs font-medium text-warning">{t('insight.execution.recovered')}</span>}
      </div>
      <h2 className="text-lg font-semibold text-text-primary">{execution.task_title ?? execution.task_ref ?? execution.execution_id}</h2>
      <p className="text-sm text-text-secondary">{[execution.project_name, execution.agent_name ?? execution.agent_ref].filter(Boolean).join(' · ')}</p>

      <section>
        <h3 className="text-sm font-semibold text-text-primary">{t('insight.detail.timeline')}</h3>
        <div className="mt-2 grid gap-2 text-sm text-text-secondary md:grid-cols-5">
          <TimelineCell label={t('insight.execution.queued')} value={formatInsightTimeOrLabel(execution.queued_at, EMPTY)} />
          <TimelineCell label={t('insight.execution.queueWait')} value={formatInsightDuration(execution.queue_wait_ms, t)} />
          <TimelineCell label={t('insight.execution.started')} value={formatInsightTimeOrLabel(execution.started_at, t('insight.execution.notStarted'))} />
          <TimelineCell label={t('insight.execution.duration')} value={formatInsightDuration(execution.duration_ms, t)} />
          <TimelineCell label={t('insight.execution.finished')} value={formatInsightTimeOrLabel(execution.finished_at, t('insight.execution.notFinished'))} />
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

      {execution.quality !== 'valid' && <InsightStatePanel testId="insight-execution-quality" tone="warn" title={insightQualityLabel(execution.quality, t) ?? t('insight.quality.check')} body={t('insight.quality.invalidBody')} />}
      <section className="text-xs text-text-muted">
        <button type="button" className="text-text-secondary hover:underline" aria-expanded={showDiagnostics} onClick={() => setShowDiagnostics((shown) => !shown)}>{t('insight.detail.technical')}</button>
        {showDiagnostics && <pre className="mt-2 overflow-x-auto rounded bg-bg-subtle p-2">{JSON.stringify({ outcome: execution.outcome, failure_reason: execution.failure_reason, command_status: execution.command_status, status_reason: execution.status_reason, quality: execution.quality }, null, 2)}</pre>}
      </section>
    </article>
  );
}

function TimelineCell({ label, value }: { label: string; value: string }): React.ReactElement {
  return <div className="rounded border border-border-base bg-bg-subtle p-2"><div className="text-xs text-text-muted">{label}</div><div className="mt-1 font-medium text-text-primary">{value}</div></div>;
}

function Info({ label, value, mono }: { label: string; value: string; mono?: boolean }): React.ReactElement {
  return <div><dt className="text-xs text-text-muted">{label}</dt><dd className={`mt-1 text-text-primary ${mono ? 'font-mono text-xs' : ''}`}>{value}</dd></div>;
}

function InsightError({ testIdPrefix, error, fallbackTitle }: { testIdPrefix: string; error: unknown; fallbackTitle: string }): React.ReactElement {
  const { t } = useTranslation('insights');
  const auth = isAuthError(error);
  return <InsightStatePanel testId={auth ? `${testIdPrefix}-auth-error` : `${testIdPrefix}-error`} tone="danger" title={auth ? t('insight.state.unauthorized') : fallbackTitle} body={error instanceof Error ? error.message : undefined} />;
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

function listHrefFromDetailSearch(search: string, base: string): string {
  const current = new URLSearchParams(search);
  const next = new URLSearchParams({ window: '24h' });
  const agentRef = current.get('agent_ref');
  const projectId = current.get('project_id');
  const cursor = current.get('cursor');
  if (agentRef) next.set('agent_ref', agentRef);
  if (projectId) next.set('project_id', projectId);
  if (cursor) next.set('cursor', cursor);
  return `${base}/executions?${next.toString()}`;
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

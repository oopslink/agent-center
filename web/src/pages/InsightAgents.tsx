import type React from 'react';
import { Link, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ApiError } from '@/api/client';
import { useInsightAgent, useInsightAgents, type InsightV2AgentSummary, type InsightV2CountMetric } from '@/api/insights';
import { ChartPanel, DonutChart, HorizontalBars, SegmentedBar, type ChartDatum } from '@/components/insight/InsightCharts';
import { presentInsightEnum } from '@/utils/insightPresentation';

const EMPTY = '—';

export default function InsightAgentsPage(): React.ReactElement {
  const { t } = useTranslation('insights');
  const { slug = '' } = useParams<{ slug: string }>();
  const base = `/organizations/${encodeURIComponent(slug)}/insights`;
  const query = useInsightAgents();

  return (
    <section className="space-y-4" data-testid="page-InsightAgents">
      <InsightAgentsHeader title={t('insight.agents.title')} subtitle={t('insight.agents.subtitle')} />
      {query.isLoading && <StatePanel testId="insight-agents-loading" title={t('insight.agents.loading')} />}
      {query.isError && <InsightError testIdPrefix="insight-agents" error={query.error} fallbackTitle={t('insight.agents.failed')} />}
      {query.data && (
        query.data.length === 0
          ? <StatePanel testId="insight-agents-empty" title={t('insight.agents.empty')} />
          : (
            <>
              <InsightAgentsCharts agents={query.data} base={base} />
              <InsightAgentsTable agents={query.data} base={base} />
            </>
          )
      )}
    </section>
  );
}

function InsightAgentsCharts({ agents, base }: { agents: InsightV2AgentSummary[]; base: string }): React.ReactElement {
  const { t } = useTranslation('insights');
  const healthData: ChartDatum[] = ['healthy', 'elevated', 'degraded', 'unknown'].map((status) => ({
    key: status,
    label: presentInsightEnum('health', status, t),
    value: agents.filter((agent) => agent.health.status === status).length,
    tone: healthTone(status),
  }));
  const executionData = agents
    .slice()
    .sort((a, b) => metricNumber(b.execution_count) - metricNumber(a.execution_count) || (a.name ?? a.id).localeCompare(b.name ?? b.id))
    .slice(0, 6)
    .map((agent) => ({
      key: agent.id,
      label: agent.name ?? agent.id,
      value: metricNumber(agent.execution_count),
      tone: healthTone(agent.health.status),
      href: `${base}/executions?window=24h&agent_ref=${encodeURIComponent(agent.id)}`,
      detail: t('insight.chart.agentWorkDetail', { blocked: metricValue(agent.blocked_tasks), plans: metricValue(agent.active_plans) }),
    }));
  const workData = agents
    .slice()
    .sort((a, b) => agentPressure(b) - agentPressure(a) || (a.name ?? a.id).localeCompare(b.name ?? b.id))
    .slice(0, 6)
    .map((agent) => ({
      key: agent.id,
      label: agent.name ?? agent.id,
      value: agentPressure(agent),
      tone: agent.health.status === 'healthy' ? 'info' as const : healthTone(agent.health.status),
      href: `${base}/agents/${encodeURIComponent(agent.id)}`,
      detail: t('insight.chart.agentPressureDetail', { issues: metricValue(agent.open_issues), blocked: metricValue(agent.blocked_tasks) }),
    }));

  return (
    <section className="grid gap-4 lg:grid-cols-3" data-testid="insight-agents-charts">
      <ChartPanel title={t('insight.chart.healthMix')} subtitle={t('insight.chart.healthMixSubtitle')}>
        <DonutChart data={healthData} totalLabel={t('insight.chart.agentsTotal')} />
      </ChartPanel>
      <ChartPanel title={t('insight.chart.agentExecutions')} subtitle={t('insight.chart.agentExecutionsSubtitle')}>
        <HorizontalBars data={executionData} emptyLabel={t('insight.chart.empty')} />
      </ChartPanel>
      <ChartPanel title={t('insight.chart.agentPressure')} subtitle={t('insight.chart.agentPressureSubtitle')}>
        <HorizontalBars data={workData} emptyLabel={t('insight.chart.empty')} />
      </ChartPanel>
    </section>
  );
}

export function InsightAgentDetailPage(): React.ReactElement {
  const { t } = useTranslation('insights');
  const { slug = '', agentRef = '' } = useParams<{ slug: string; agentRef: string }>();
  const decodedAgentRef = decodeURIComponent(agentRef);
  const base = `/organizations/${encodeURIComponent(slug)}/insights`;
  const query = useInsightAgent(decodedAgentRef);
  const isNotFound = query.error instanceof ApiError && query.error.status === 404;

  return (
    <section className="space-y-4" data-testid="page-InsightAgentDetail">
      <InsightAgentsHeader
        title={query.data?.name ?? decodedAgentRef}
        subtitle={decodedAgentRef}
        action={<LinkButton to={`${base}/executions?window=24h&agent_ref=${encodeURIComponent(decodedAgentRef)}`}>{t('insight.actions.viewExecutions')}</LinkButton>}
      />
      <nav className="flex flex-wrap gap-2 text-sm" aria-label={t('insight.breadcrumb')}>
        <Link to={`${base}/overview`} className="text-brand hover:underline">{t('insight.overview.title')}</Link>
        <span className="text-text-muted">/</span>
        <Link to={`${base}/agents`} className="text-brand hover:underline">{t('insight.agents.title')}</Link>
      </nav>
      {query.isLoading && <StatePanel testId="insight-agent-loading" title={t('insight.agents.loadingDetail')} />}
      {isNotFound && <StatePanel testId="insight-agent-not-found" tone="danger" title={t('insight.agents.notFound')} />}
      {query.isError && !isNotFound && <InsightError testIdPrefix="insight-agent" error={query.error} fallbackTitle={t('insight.agents.failedDetail')} />}
      {query.data && <InsightAgentSummaryCard agent={query.data} />}
    </section>
  );
}

function InsightAgentsHeader({ title, subtitle, action }: { title: string; subtitle?: string; action?: React.ReactNode }): React.ReactElement {
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

function InsightAgentsTable({ agents, base }: { agents: InsightV2AgentSummary[]; base: string }): React.ReactElement {
  const { t } = useTranslation('insights');
  return (
    <div className="overflow-x-auto rounded border border-border-base bg-bg-elevated" data-testid="insight-agents-table">
      <table className="w-full min-w-[52rem] text-left text-sm">
        <thead className="text-xs uppercase tracking-wide text-text-muted">
          <tr>
            <th className="px-3 py-2 font-medium">{t('insight.agents.col.agent')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.agents.col.health')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.agents.col.executions')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.agents.col.unknown')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.agents.col.coverage')}</th>
            <th className="px-3 py-2 font-medium">{t('insight.table.action')}</th>
          </tr>
        </thead>
        <tbody>
          {agents.map((agent) => (
            <tr key={agent.id} className="border-t border-border-base">
              <td className="px-3 py-2">
                <Link to={`${base}/agents/${encodeURIComponent(agent.id)}`} className="font-medium text-brand hover:underline">{agent.name ?? agent.id}</Link>
                <div className="font-mono text-xs text-text-muted">{agent.id}</div>
              </td>
              <td className="px-3 py-2"><HealthBadge status={agent.health.status} /></td>
              <td className="px-3 py-2 tabular-nums">{metricValue(agent.execution_count)}</td>
              <td className="px-3 py-2 tabular-nums">{agent.execution_count.meta.unknown_count}</td>
              <td className="px-3 py-2 tabular-nums">{formatCoverage(agent.execution_count.meta.coverage)}</td>
              <td className="px-3 py-2">
                <Link to={`${base}/executions?window=24h&agent_ref=${encodeURIComponent(agent.id)}`} className="text-brand hover:underline">{t('insight.actions.viewExecutions')}</Link>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function InsightAgentSummaryCard({ agent }: { agent: InsightV2AgentSummary }): React.ReactElement {
  const { t } = useTranslation('insights');
  return (
    <>
      <article className="space-y-4 rounded border border-border-base bg-bg-elevated p-4" data-testid="insight-agent-detail">
        <div className="flex flex-wrap items-center gap-2">
          <HealthBadge status={agent.health.status} />
          {agent.reason_codes.map((code, index) => <span key={`${index}-${code}`} className="rounded-full border border-border-base bg-bg-subtle px-2 py-0.5 text-xs text-text-secondary">{presentInsightEnum('reasonCode', code, t)}</span>)}
        </div>
        <dl className="grid gap-3 text-sm md:grid-cols-2 xl:grid-cols-4">
          <MetricInfo label={t('insight.agents.metric.executions')} metric={agent.execution_count} />
          <MetricInfo label={t('insight.agents.metric.openIssues')} metric={agent.open_issues} />
          <MetricInfo label={t('insight.agents.metric.blockedTasks')} metric={agent.blocked_tasks} />
          <MetricInfo label={t('insight.agents.metric.activePlans')} metric={agent.active_plans} />
        </dl>
        <section>
          <h2 className="text-sm font-semibold text-text-primary">{t('insight.agents.confidence')}</h2>
          <dl className="mt-2 grid gap-3 text-sm md:grid-cols-3">
            <Info label={t('insight.agents.col.coverage')} value={formatCoverage(agent.execution_count.meta.coverage)} />
            <Info label={t('insight.agents.col.unknown')} value={String(agent.execution_count.meta.unknown_count)} />
            <Info label={t('insight.agents.sampleCount')} value={String(agent.execution_count.meta.sample_count)} />
          </dl>
        </section>
      </article>
      <ChartPanel title={t('insight.chart.agentDetailShape')} subtitle={t('insight.chart.agentDetailShapeSubtitle')}>
        <SegmentedBar
          emptyLabel={t('insight.chart.empty')}
          data={[
            { key: 'executions', label: t('insight.agents.metric.executions'), value: metricNumber(agent.execution_count), tone: 'info' },
            { key: 'openIssues', label: t('insight.agents.metric.openIssues'), value: metricNumber(agent.open_issues), tone: 'warning' },
            { key: 'blockedTasks', label: t('insight.agents.metric.blockedTasks'), value: metricNumber(agent.blocked_tasks), tone: 'danger' },
            { key: 'activePlans', label: t('insight.agents.metric.activePlans'), value: metricNumber(agent.active_plans), tone: 'success' },
          ]}
        />
      </ChartPanel>
    </>
  );
}

function MetricInfo({ label, metric }: { label: string; metric: InsightV2CountMetric }): React.ReactElement {
  return <Info label={label} value={metricValue(metric)} muted={!metric.meta.known} />;
}

function Info({ label, value, muted }: { label: string; value: string; muted?: boolean }): React.ReactElement {
  return <div><dt className="text-xs text-text-muted">{label}</dt><dd className={`mt-1 font-medium text-text-primary ${muted ? 'text-text-muted' : ''}`}>{value}</dd></div>;
}

function HealthBadge({ status }: { status: string }): React.ReactElement {
  const { t } = useTranslation('insights');
  const tone = status === 'healthy'
    ? 'border-success/30 bg-success/10 text-success'
    : status === 'elevated'
      ? 'border-warning/30 bg-warning/10 text-warning'
      : status === 'degraded'
        ? 'border-danger/30 bg-danger/10 text-danger'
        : 'border-border-base bg-bg-subtle text-text-secondary';
  return <span className={`rounded-full border px-2 py-0.5 text-xs font-medium ${tone}`}>{presentInsightEnum('health', status, t)}</span>;
}

function StatePanel({ testId, title, body, tone = 'muted' }: { testId: string; title: string; body?: string; tone?: 'muted' | 'danger' }): React.ReactElement {
  const cls = tone === 'danger' ? 'border-danger/30 bg-danger/5 text-danger' : 'border-border-base bg-bg-elevated text-text-secondary';
  return <div className={`rounded border p-3 text-sm ${cls}`} data-testid={testId}><div className="font-medium">{title}</div>{body && <p className="mt-1 text-xs">{body}</p>}</div>;
}

function LinkButton({ to, children }: { to: string; children: React.ReactNode }): React.ReactElement {
  return <Link to={to} className="rounded border border-border-base bg-bg-elevated px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle">{children}</Link>;
}

function InsightError({ testIdPrefix, error, fallbackTitle }: { testIdPrefix: string; error: unknown; fallbackTitle: string }): React.ReactElement {
  const { t } = useTranslation('insights');
  const auth = error instanceof ApiError && (error.status === 401 || error.status === 403);
  return <StatePanel testId={auth ? `${testIdPrefix}-auth-error` : `${testIdPrefix}-error`} tone="danger" title={auth ? t('insight.state.unauthorized') : fallbackTitle} body={error instanceof Error ? error.message : undefined} />;
}

function metricValue(metric: InsightV2CountMetric): string {
  if (!metric.meta.known || metric.value === null) return EMPTY;
  return String(metric.value);
}

function metricNumber(metric: InsightV2CountMetric): number {
  if (!metric.meta.known || metric.value === null) return 0;
  return Math.max(0, metric.value);
}

function agentPressure(agent: InsightV2AgentSummary): number {
  return metricNumber(agent.open_issues) + metricNumber(agent.blocked_tasks) + metricNumber(agent.active_plans);
}

function healthTone(status: string): ChartDatum['tone'] {
  if (status === 'healthy') return 'success';
  if (status === 'elevated') return 'warning';
  if (status === 'degraded') return 'danger';
  return 'neutral';
}

function formatCoverage(value: number | null): string {
  if (value === null) return EMPTY;
  if (value > 0 && value < 0.001) return '<0.1%';
  return `${Math.round(value * 1000) / 10}%`;
}

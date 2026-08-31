import type React from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ApiError } from '@/api/client';
import {
  INSIGHT_V2_BREAK_KINDS,
  useInsightV2PlanLineage,
  useInsightV2Project,
  useInsightV2ProjectDelivery,
  useInsightV2ProjectEvolution,
  useInsightV2Projects,
  type InsightFreshness,
  type InsightV2BreakKind,
  type InsightV2CountMetric,
  type InsightV2FunnelBreak,
  type InsightV2Generation,
  type InsightV2Health,
  type InsightV2ProjectEvolution,
  type InsightV2ProjectSummary,
} from '@/api/insights';

const EMPTY = '—';

export default function InsightProjectsPage(): React.ReactElement {
  const { t } = useTranslation('insights');
  const { slug = '' } = useParams<{ slug: string }>();
  const base = `/organizations/${encodeURIComponent(slug)}/insights/projects`;
  const projects = useInsightV2Projects();

  return (
    <section className="space-y-4" data-testid="page-InsightProjects">
      <InsightProjectHeader title={t('insight.projects.title')} subtitle={t('insight.projects.subtitle')} />
      {projects.isLoading && <StatePanel testId="insight-projects-loading" title={t('insight.projects.loading')} />}
      {projects.isError && <InsightV2Error testIdPrefix="insight-projects" error={projects.error} fallbackTitle={t('insight.projects.failed')} />}
      {projects.data && projects.data.length === 0 && (
        <StatePanel testId="insight-projects-empty" title={t('insight.projects.empty')} body={t('insight.projects.emptyBody')} />
      )}
      {projects.data && projects.data.length > 0 && (
        <div className="overflow-x-auto rounded border border-border-base bg-bg-elevated" data-testid="insight-projects-table">
          <table className="w-full min-w-[58rem] text-left text-sm">
            <thead className="text-xs uppercase tracking-wide text-text-muted">
              <tr>
                <th className="px-3 py-2 font-medium">{t('insight.projects.project')}</th>
                <th className="px-3 py-2 font-medium">{t('insight.projects.health')}</th>
                <th className="px-3 py-2 font-medium">{t('insight.projects.executions')}</th>
                <th className="px-3 py-2 font-medium">{t('insight.projects.openIssues')}</th>
                <th className="px-3 py-2 font-medium">{t('insight.projects.blockedTasks')}</th>
                <th className="px-3 py-2 font-medium">{t('insight.projects.activePlans')}</th>
                <th className="px-3 py-2 font-medium">{t('insight.projects.reasonCodes')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border-base">
              {projects.data.map((project) => (
                <ProjectRow key={project.id} project={project} to={`${base}/${encodeURIComponent(project.id)}`} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

export function InsightProjectDetailPage(): React.ReactElement {
  const { t } = useTranslation('insights');
  const { slug = '', projectId = '' } = useParams<{ slug: string; projectId: string }>();
  const [params] = useSearchParams();
  const planId = params.get('plan_id') || undefined;
  const base = `/organizations/${encodeURIComponent(slug)}/insights/projects`;
  const project = useInsightV2Project(projectId);
  const delivery = useInsightV2ProjectDelivery(projectId);
  const evolution = useInsightV2ProjectEvolution(projectId);

  return (
    <section className="space-y-4" data-testid="page-InsightProjectDetail" data-project-id={projectId}>
      <InsightProjectHeader
        title={project.data?.name ?? projectId}
        subtitle={t('insight.projectDetail.subtitle')}
        action={<LinkButton to={base}>{t('insight.projects.back')}</LinkButton>}
      />

      {project.isLoading && <StatePanel testId="insight-project-loading" title={t('insight.projectDetail.loading')} />}
      {project.isError && <InsightV2Error testIdPrefix="insight-project" error={project.error} fallbackTitle={t('insight.projectDetail.failed')} />}
      {project.data && <ProjectSummaryPanel project={project.data} />}

      {delivery.isLoading && <StatePanel testId="insight-delivery-loading" title={t('insight.delivery.loading')} />}
      {delivery.isError && <InsightV2Error testIdPrefix="insight-delivery" error={delivery.error} fallbackTitle={t('insight.delivery.failed')} />}
      {delivery.data && (
        <>
          <WindowBar window={delivery.data.time_window} asOf={delivery.data.as_of} freshness={delivery.data.meta.freshness} />
          <HealthPanel health={delivery.data.health} meta={delivery.data.meta} />
          <DeliveryFunnel breaks={delivery.data.funnel.breaks} metrics={[
            ['issues', delivery.data.funnel.issues],
            ['tasks', delivery.data.funnel.tasks],
            ['plans', delivery.data.funnel.plans],
            ['done', delivery.data.funnel.done],
          ]} />
        </>
      )}

      {evolution.isLoading && <StatePanel testId="insight-evolution-loading" title={t('insight.evolution.loading')} />}
      {evolution.isError && <InsightV2Error testIdPrefix="insight-evolution" error={evolution.error} fallbackTitle={t('insight.evolution.failed')} />}
      {evolution.data && <EvolutionPanel data={evolution.data.evolution} />}

      {planId ? (
        <LineageLink projectId={projectId} planId={planId} slug={slug} />
      ) : (
        <StatePanel testId="insight-lineage-empty" title={t('insight.lineage.noPlan')} body={t('insight.lineage.noPlanBody')} />
      )}
    </section>
  );
}

export function InsightPlanLineagePage(): React.ReactElement {
  const { t } = useTranslation('insights');
  const { slug = '', projectId = '', planId = '' } = useParams<{ slug: string; projectId: string; planId: string }>();
  const lineage = useInsightV2PlanLineage(projectId, planId);

  return (
    <section className="space-y-4" data-testid="page-InsightPlanLineage" data-project-id={projectId} data-plan-id={planId}>
      <InsightProjectHeader
        title={t('insight.lineage.title', { planId })}
        subtitle={t('insight.lineage.subtitle')}
        action={<LinkButton to={`/organizations/${encodeURIComponent(slug)}/insights/projects/${encodeURIComponent(projectId)}?plan_id=${encodeURIComponent(planId)}`}>{t('insight.lineage.back')}</LinkButton>}
      />
      {lineage.isLoading && <StatePanel testId="insight-lineage-loading" title={t('insight.lineage.loading')} />}
      {lineage.isError && <InsightV2Error testIdPrefix="insight-lineage" error={lineage.error} fallbackTitle={t('insight.lineage.failed')} />}
      {lineage.data && (
        <>
          <WindowBar window={lineage.data.time_window} asOf={lineage.data.as_of} freshness={lineage.data.meta.freshness} />
          <HealthPanel health={lineage.data.health} meta={lineage.data.meta} />
          {lineage.data.generations.length === 0 ? (
            <StatePanel testId="insight-lineage-empty-response" title={t('insight.lineage.empty')} body={t('insight.lineage.emptyBody')} />
          ) : (
            <div className="space-y-3" data-testid="insight-lineage-generations">
              {lineage.data.generations.map((generation) => (
                <GenerationCard key={`${generation.generation}-${generation.created_at}`} generation={generation} />
              ))}
            </div>
          )}
        </>
      )}
    </section>
  );
}

function ProjectRow({ project, to }: { project: InsightV2ProjectSummary; to: string }): React.ReactElement {
  return (
    <tr data-testid="insight-project-row" data-project-id={project.id}>
      <td className="px-3 py-2">
        <Link to={to} className="font-medium text-brand hover:underline">
          {project.name ?? project.id}
        </Link>
        <div className="font-mono text-xs text-text-muted">{project.id}</div>
      </td>
      <td className="px-3 py-2"><HealthBadge health={project.health} /></td>
      <td className="px-3 py-2 tabular-nums"><MetricValue metric={project.execution_count} /></td>
      <td className="px-3 py-2 tabular-nums"><MetricValue metric={project.open_issues} /></td>
      <td className="px-3 py-2 tabular-nums"><MetricValue metric={project.blocked_tasks} /></td>
      <td className="px-3 py-2 tabular-nums"><MetricValue metric={project.active_plans} /></td>
      <td className="px-3 py-2"><ReasonCodes codes={project.reason_codes} /></td>
    </tr>
  );
}

function ProjectSummaryPanel({ project }: { project: InsightV2ProjectSummary }): React.ReactElement {
  const { t } = useTranslation('insights');
  return (
    <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="insight-project-summary">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <h2 className="text-sm font-semibold text-text-primary">{t('insight.projectDetail.runtimeHealth')}</h2>
        <HealthBadge health={project.health} />
      </div>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <MetricTile label={t('insight.projects.executions')} metric={project.execution_count} />
        <MetricTile label={t('insight.projects.openIssues')} metric={project.open_issues} />
        <MetricTile label={t('insight.projects.blockedTasks')} metric={project.blocked_tasks} />
        <MetricTile label={t('insight.projects.activePlans')} metric={project.active_plans} />
      </div>
      <div className="mt-3">
        <ReasonCodes codes={project.reason_codes} />
      </div>
    </section>
  );
}

function DeliveryFunnel({ metrics, breaks }: { metrics: Array<[string, InsightV2CountMetric]>; breaks: InsightV2FunnelBreak[] }): React.ReactElement {
  const { t } = useTranslation('insights');
  const byKind = new Map(breaks.map((item) => [item.kind, item]));
  return (
    <section className="space-y-3 rounded border border-border-base bg-bg-elevated p-4" data-testid="insight-delivery-funnel">
      <div>
        <h2 className="text-sm font-semibold text-text-primary">{t('insight.delivery.title')}</h2>
        <p className="text-xs text-text-muted">{t('insight.delivery.subtitle')}</p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {metrics.map(([key, metric]) => (
          <MetricTile key={key} label={t(`insight.delivery.stage.${key}`)} metric={metric} />
        ))}
      </div>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[52rem] text-left text-sm">
          <thead className="text-xs uppercase tracking-wide text-text-muted">
            <tr>
              <th className="px-2 py-2 font-medium">{t('insight.delivery.breakKind')}</th>
              <th className="px-2 py-2 font-medium">{t('insight.delivery.count')}</th>
              <th className="px-2 py-2 font-medium">{t('insight.delivery.drilldown')}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border-base">
            {INSIGHT_V2_BREAK_KINDS.map((kind) => {
              const item = byKind.get(kind) ?? emptyBreak(kind);
              return <BreakRow key={kind} item={item} />;
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function BreakRow({ item }: { item: InsightV2FunnelBreak }): React.ReactElement {
  const { t } = useTranslation('insights');
  const filter = stableJSON(item.drilldown);
  return (
    <tr data-testid="insight-funnel-break" data-break-kind={item.kind}>
      <td className="px-2 py-2">
        <div className="font-medium text-text-primary">{t(`insight.delivery.break.${item.kind}`)}</div>
        <div className="font-mono text-xs text-text-muted">{item.kind}</div>
      </td>
      <td className="px-2 py-2 tabular-nums"><MetricValue metric={item.count} /></td>
      <td className="px-2 py-2">
        <code className="block max-w-[34rem] overflow-x-auto rounded bg-bg-subtle px-2 py-1 text-xs text-text-secondary" data-testid="insight-break-drilldown">
          {filter}
        </code>
      </td>
    </tr>
  );
}

function EvolutionPanel({ data }: { data: InsightV2ProjectEvolution['evolution'] }): React.ReactElement {
  const { t } = useTranslation('insights');
  const knownKeys = new Set(['plans', 'evolved_plans', 'evolution_rate', 'generation_count']);
  const extra = Object.entries(data).filter(([key]) => !knownKeys.has(key));
  return (
    <section className="space-y-3 rounded border border-border-base bg-bg-elevated p-4" data-testid="insight-evolution-panel">
      <div>
        <h2 className="text-sm font-semibold text-text-primary">{t('insight.evolution.title')}</h2>
        <p className="text-xs text-text-muted">{t('insight.evolution.subtitle')}</p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <ValueTile label={t('insight.evolution.plans')} value={String(data.plans)} />
        <ValueTile label={t('insight.evolution.evolvedPlans')} value={String(data.evolved_plans)} />
        <ValueTile label={t('insight.evolution.evolutionRate')} value={formatRatio(data.evolution_rate)} />
        <ValueTile label={t('insight.evolution.generationCount')} value={String(data.generation_count)} />
      </div>
      {extra.length > 0 && (
        <dl className="grid gap-2 text-xs sm:grid-cols-2" data-testid="insight-evolution-extra">
          {extra.map(([key, value]) => (
            <div key={key} className="rounded border border-border-base bg-bg-subtle p-2">
              <dt className="font-mono text-text-muted">{key}</dt>
              <dd className="mt-1 text-text-primary">{formatUnknownValue(value)}</dd>
            </div>
          ))}
        </dl>
      )}
    </section>
  );
}

function LineageLink({ slug, projectId, planId }: { slug: string; projectId: string; planId: string }): React.ReactElement {
  const { t } = useTranslation('insights');
  return (
    <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="insight-lineage-link-panel">
      <h2 className="text-sm font-semibold text-text-primary">{t('insight.lineage.panelTitle')}</h2>
      <p className="mt-1 text-xs text-text-secondary">{t('insight.lineage.panelBody', { planId })}</p>
      <LinkButton to={`/organizations/${encodeURIComponent(slug)}/insights/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}/lineage`}>
        {t('insight.lineage.open')}
      </LinkButton>
    </section>
  );
}

function GenerationCard({ generation }: { generation: InsightV2Generation }): React.ReactElement {
  const { t } = useTranslation('insights');
  const anomaly = generation.reason === 'unknown' || generation.recovery_outcome === 'unknown' || generation.acceptance_verdict === 'reject' || generation.delivery_sha === '';
  return (
    <article className={`rounded border bg-bg-elevated p-4 ${anomaly ? 'border-warning/40' : 'border-border-base'}`} data-testid="insight-lineage-generation" data-generation={`G${generation.generation}`}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">G{generation.generation}</h2>
          <p className="text-xs text-text-muted">{formatDateTime(generation.created_at)}</p>
        </div>
        {anomaly && <span className="rounded-full border border-warning/30 bg-warning/10 px-2 py-0.5 text-xs font-medium text-warning">{t('insight.lineage.anomaly')}</span>}
      </div>
      <dl className="mt-3 grid gap-3 text-sm md:grid-cols-2">
        <Info label={t('insight.lineage.triggeredBy')} value={generation.triggered_by || EMPTY} mono />
        <Info label={t('insight.lineage.reason')} value={reasonLabel(generation.reason, t)} />
        <Info label={t('insight.lineage.recoveryDuration')} value={humanDuration(generation.recovery_duration_ms)} />
        <Info label={t('insight.lineage.recoveryOutcome')} value={generation.recovery_outcome || EMPTY} />
        <Info label={t('insight.lineage.deliveryBranch')} value={generation.delivery_branch || EMPTY} mono />
        <Info label={t('insight.lineage.deliverySha')} value={generation.delivery_sha || EMPTY} mono />
        <Info label={t('insight.lineage.verdict')} value={verdictLabel(generation.acceptance_verdict, t)} />
      </dl>
      <JsonBlock title={t('insight.lineage.evidence')} value={generation.evidence} />
      <JsonBlock title={t('insight.lineage.nodeChanges')} value={generation.node_changes} />
    </article>
  );
}

function InsightProjectHeader({ title, subtitle, action }: { title: string; subtitle?: string; action?: React.ReactNode }): React.ReactElement {
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
  return <Link to={to} className="mt-3 inline-flex rounded border border-border-base bg-bg-elevated px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle">{children}</Link>;
}

function WindowBar({ window, asOf, freshness }: { window: { start: string; end: string }; asOf: string; freshness: InsightFreshness }): React.ReactElement {
  const { t } = useTranslation('insights');
  return (
    <div className="flex flex-wrap items-center gap-3 rounded border border-border-base bg-bg-elevated px-3 py-2 text-xs text-text-secondary" data-testid="insight-v2-window">
      <strong className="text-text-primary">{t('insight.window.title')}</strong>
      <span title={window.start}>{formatDateTime(window.start)}</span>
      <span aria-hidden="true">–</span>
      <span title={window.end}>{formatDateTime(window.end)}</span>
      <span>{t('insight.v2.asOf', { time: formatDateTime(asOf) })}</span>
      <span><FreshnessBadge freshness={freshness} /></span>
    </div>
  );
}

function HealthPanel({ health, meta }: { health: InsightV2Health; meta: { known: boolean; coverage: number | null; sample_count: number; unknown_count: number } }): React.ReactElement {
  const { t } = useTranslation('insights');
  return (
    <section className="rounded border border-border-base bg-bg-elevated p-3 text-sm" data-testid="insight-health-panel">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium text-text-primary">{t('insight.v2.healthDecision')}</span>
        <HealthBadge health={health} />
      </div>
      <p className="mt-2 text-xs text-text-secondary">
        {t('insight.v2.meta', {
          known: meta.known ? t('insight.v2.known') : t('insight.v2.unknown'),
          sample: meta.sample_count,
          coverage: formatRatio(meta.coverage),
          unknown: meta.unknown_count,
        })}
      </p>
      <ReasonCodes codes={health.reason_codes} />
    </section>
  );
}

function HealthBadge({ health }: { health: InsightV2Health }): React.ReactElement {
  const { t } = useTranslation('insights');
  const cls = health.status === 'healthy'
    ? 'border-success/30 bg-success/10 text-success'
    : health.status === 'elevated'
      ? 'border-warning/30 bg-warning/10 text-warning'
      : health.status === 'degraded'
        ? 'border-danger/30 bg-danger/10 text-danger'
        : 'border-text-muted/30 bg-bg-subtle text-text-secondary';
  return <span className={`rounded-full border px-2 py-0.5 text-xs font-medium ${cls}`} data-testid="insight-health-badge">{t(`insight.health.${health.status}`)}</span>;
}

function FreshnessBadge({ freshness }: { freshness: InsightFreshness }): React.ReactElement {
  const { t } = useTranslation('insights');
  const cls = freshness.state === 'fresh' ? 'text-success' : freshness.state === 'stale' ? 'text-warning' : 'text-danger';
  return <span className={`font-medium ${cls}`} title={`${freshness.age_ms}/${freshness.threshold_ms}`}>{t(`insight.freshness.${freshness.state}`)}</span>;
}

function MetricTile({ label, metric }: { label: string; metric: InsightV2CountMetric }): React.ReactElement {
  return <ValueTile label={label} value={<MetricValue metric={metric} />} sub={metric.meta.known ? undefined : 'known=false'} />;
}

function ValueTile({ label, value, sub }: { label: string; value: React.ReactNode; sub?: string }): React.ReactElement {
  return (
    <div className="rounded border border-border-base bg-bg-subtle p-3">
      <div className="text-xs font-medium text-text-muted">{label}</div>
      <div className="mt-1 text-2xl font-semibold tabular-nums text-text-primary">{value}</div>
      {sub && <div className="mt-1 text-xs text-text-muted">{sub}</div>}
    </div>
  );
}

function MetricValue({ metric }: { metric: InsightV2CountMetric }): React.ReactElement {
  return <span title={`known=${metric.meta.known}; sample_count=${metric.meta.sample_count}; unknown_count=${metric.meta.unknown_count}`}>{metric.meta.known && metric.value !== null ? metric.value : EMPTY}</span>;
}

function ReasonCodes({ codes }: { codes: string[] }): React.ReactElement {
  const { t } = useTranslation('insights');
  if (codes.length === 0) return <span className="text-xs text-text-muted">{t('insight.v2.noReasons')}</span>;
  return (
    <div className="mt-2 flex flex-wrap gap-1" data-testid="insight-reason-codes">
      {codes.map((code) => (
        <code key={code} className="rounded bg-bg-subtle px-1.5 py-0.5 text-xs text-text-secondary">{code}</code>
      ))}
    </div>
  );
}

function StatePanel({ testId, title, body, tone = 'muted' }: { testId: string; title: string; body?: string; tone?: 'muted' | 'danger' }): React.ReactElement {
  const cls = tone === 'danger' ? 'border-danger/30 bg-danger/5 text-danger' : 'border-border-base bg-bg-elevated text-text-secondary';
  return <div className={`rounded border p-3 text-sm ${cls}`} data-testid={testId}><div className="font-medium">{title}</div>{body && <p className="mt-1 text-xs">{body}</p>}</div>;
}

function InsightV2Error({ testIdPrefix, error, fallbackTitle }: { testIdPrefix: string; error: unknown; fallbackTitle: string }): React.ReactElement {
  const { t } = useTranslation('insights');
  const title = error instanceof ApiError && (error.status === 401 || error.status === 403) ? t('insight.state.unauthorized') : fallbackTitle;
  return <StatePanel testId={`${testIdPrefix}-error`} tone="danger" title={title} body={error instanceof Error ? error.message : undefined} />;
}

function Info({ label, value, mono }: { label: string; value: string; mono?: boolean }): React.ReactElement {
  return <div><dt className="text-xs text-text-muted">{label}</dt><dd className={`mt-1 text-text-primary ${mono ? 'font-mono text-xs' : ''}`}>{value}</dd></div>;
}

function JsonBlock({ title, value }: { title: string; value: Record<string, unknown>[] }): React.ReactElement {
  return (
    <details className="mt-3 text-xs text-text-muted">
      <summary className="cursor-pointer text-text-secondary">{title}</summary>
      <pre className="mt-2 overflow-x-auto rounded bg-bg-subtle p-2" data-testid="insight-lineage-json">{stableJSON(value)}</pre>
    </details>
  );
}

function emptyBreak(kind: InsightV2BreakKind): InsightV2FunnelBreak {
  return {
    kind,
    count: { value: 0, meta: emptyMeta() },
    drilldown: { break_kind: kind },
  };
}

function emptyMeta() {
  return {
    metric_version: 'insight.metrics.v2' as const,
    sample_count: 0,
    coverage: 1,
    freshness: { state: 'unknown' as const, age_ms: 0, threshold_ms: 0 },
    unknown_count: 0,
    known: true,
  };
}

function formatRatio(value: number | null): string {
  if (value === null) return EMPTY;
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

function formatDateTime(value: string): string {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value || EMPTY;
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' }).format(d);
}

function reasonLabel(reason: string, t: (key: string, options?: Record<string, unknown>) => string): string {
  const key = ['blocked', 'review_reject', 'requirement_change', 'execution_failure', 'manual_adjustment', 'unknown'].includes(reason) ? reason : 'unknown';
  return t(`insight.lineage.reasonValue.${key}`);
}

function verdictLabel(verdict: string, t: (key: string, options?: Record<string, unknown>) => string): string {
  const key = verdict === 'pass' || verdict === 'reject' || verdict === 'pending' ? verdict : 'pending';
  return t(`insight.lineage.verdictValue.${key}`);
}

function formatUnknownValue(value: unknown): string {
  if (value === null || value === undefined) return EMPTY;
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return String(value);
  return stableJSON(value);
}

function stableJSON(value: unknown): string {
  return JSON.stringify(sortJSON(value), null, 2);
}

function sortJSON(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortJSON);
  if (!value || typeof value !== 'object') return value;
  return Object.fromEntries(Object.entries(value as Record<string, unknown>).sort(([a], [b]) => a.localeCompare(b)).map(([k, v]) => [k, sortJSON(v)]));
}

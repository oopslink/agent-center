import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import * as echarts from 'echarts/core';
import { GraphChart, type GraphSeriesOption } from 'echarts/charts';
import { LegendComponent, TooltipComponent, type TooltipComponentOption, type LegendComponentOption } from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import type { ECharts, ComposeOption } from 'echarts/core';
import { ApiError } from '@/api/client';
import {
  useInfiniteCollaborationEffects,
  useCollaborationEvidenceBundle,
  type CollaborationEdge,
  type CollaborationEffect,
  type CollaborationEffectScope,
  type CollaborationFilters,
  type CollaborationGraphResponse,
  type CollaborationNode,
  type CollaborationPolarity,
  type CollaborationRelation,
} from '@/api/insights';
import { identityRefOf, normalizeIdentityRef, refKind, useMembers } from '@/api/members';
import { useProjectMembers, useProjects } from '@/api/projects';
import { useTasksList } from '@/api/tasks';
import { usePlans } from '@/api/plans';
import { EntitySelect, type EntityOption } from '@/components/EntitySelect';

echarts.use([GraphChart, TooltipComponent, LegendComponent, CanvasRenderer]);

const RELATIONS: CollaborationRelation[] = ['assign', 'reassign', 'complete', 'block', 'unblock', 'dependency_release', 'review_accept', 'review_reject'];
const POLARITIES: CollaborationPolarity[] = ['positive', 'negative', 'neutral', 'mixed'];
const COLLABORATION_VIEWS = ['network', 'impact', 'lineage'] as const;
const TIMELINE_RENDER_LIMIT = 200;
const EDGE_LIST_RENDER_LIMIT = 240;
const NODE_COLORS: Record<CollaborationNode['kind'], string> = {
  agent: '#2563eb',
  task: '#0f766e',
  plan: '#7c3aed',
  stage: '#ca8a04',
  project: '#64748b',
  cluster: '#db2777',
};
const EDGE_COLORS: Record<CollaborationPolarity, string> = {
  positive: '#16803c',
  negative: '#c0362c',
  neutral: '#64748b',
  mixed: '#9a5b00',
};
type CollaborationViewKind = typeof COLLABORATION_VIEWS[number];
type CollaborationChartOption = ComposeOption<GraphSeriesOption | TooltipComponentOption | LegendComponentOption>;

type CollaborationGraphView = {
  nodes: CollaborationNode[];
  edges: CollaborationEdge[];
  clusters: CollaborationNode[];
  lod: 'full' | 'cluster';
  truncated: boolean;
};
type PositionedNode = CollaborationNode & { x: number; y: number };
type DimensionGraphView = CollaborationGraphView & {
  view: CollaborationViewKind;
  unsupported: boolean;
  reason?: string;
  visibleNodeCount: number;
  visibleEdgeCount: number;
};

export default function InsightCollaboration(): React.ReactElement {
  const { t } = useTranslation('insights');
  const [params, setParams] = useSearchParams();
  const [selected, setSelected] = useState<CollaborationEffectScope[] | null>(null);
  const [switchNotice, setSwitchNotice] = useState<string | null>(null);
  const activeView = viewFromParams(params);
  const filters = filtersFromParams(params);
  const query = useInfiniteCollaborationEffects(filters);
  const effects = useMemo(() => dedupeBy(query.data?.pages.flatMap((page) => page.effects) ?? [], (item) => item.effect_id), [query.data?.pages]);
  const selectedIds = selected?.map((item) => item.effect_id) ?? [];
  const effect = effects.find((item) => selectedIds.includes(item.effect_id)) ?? null;
  const view = useMemo(() => accumulateGraph(query.data?.pages ?? []), [query.data?.pages]);
  const activeGraph = useMemo(() => buildDimensionGraph(activeView, view, effects, t), [activeView, view, effects, t]);
  const summary = useMemo(() => summarizeEffects(effects), [effects]);
  const showFullGraph = () => {
    const next = new URLSearchParams(params);
    next.set('lod', 'full');
    next.delete('max_nodes');
    next.delete('cursor');
    setParams(next);
  };

  const update = (key: string, value: string, clear: string[] = []) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value); else next.delete(key);
    clear.forEach((item) => next.delete(item));
    next.delete('cursor');
    setParams(next);
    setSelected(null);
    setSwitchNotice(null);
  };
  const changeView = (nextView: CollaborationViewKind) => {
    const next = new URLSearchParams(params);
    next.set('view', nextView);
    next.delete('cursor');
    setParams(next);
    const nextGraph = buildDimensionGraph(nextView, view, effects, t);
    if (selected && !selectionCompatible(selected, nextGraph)) {
      setSelected(null);
      setSwitchNotice(t('insight.collaboration.selectionIncompatible', { view: t(`insight.collaboration.views.${nextView}`) }));
    } else {
      setSwitchNotice(null);
    }
  };

  return (
    <section className="flex h-full min-h-0 min-w-0 flex-col gap-3 overflow-hidden" data-testid="page-InsightCollaboration">
      <header className="shrink-0">
        <h1 className="text-xl font-semibold text-text-primary">{t('insight.collaboration.title')}</h1>
        <p className="mt-0.5 text-sm text-text-muted">{t('insight.collaboration.subtitle')}</p>
      </header>
      <CollaborationFiltersBar params={params} update={update} clearAll={() => { const next = new URLSearchParams(); next.set('view', activeView); setParams(next); setSelected(null); setSwitchNotice(null); }} t={t} />
      <CollaborationViewTabs active={activeView} onChange={changeView} t={t} />
      {switchNotice ? <State id="collaboration-selection-incompatible" title={t('insight.collaboration.selectionCleared')} body={switchNotice} /> : null}
      {query.isLoading ? <State id="collaboration-loading" title={t('insight.collaboration.loading')} /> : null}
      {query.isError ? <CollaborationError error={query.error} t={t} /> : null}
      {query.data ? <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-3 overflow-hidden" data-testid="collaboration-workspace">
        <div className="shrink-0">
          <Summary summary={summary} t={t} />
        </div>
        {activeGraph.unsupported ? <State id="collaboration-unsupported" title={t('insight.collaboration.unsupported')} body={activeGraph.reason ?? t('insight.collaboration.emptyBody')} /> : null}
        {!activeGraph.unsupported && activeGraph.edges.length === 0 ? <State id="collaboration-empty" title={t('insight.collaboration.empty')} body={t('insight.collaboration.emptyBody')} /> : null}
        {!activeGraph.unsupported && activeGraph.edges.length > 0 ? <div className="relative flex min-h-0 min-w-0 flex-1 overflow-hidden">
          <CollaborationGraph view={activeGraph} selected={selected} onSelect={setSelected} onClearSelection={() => setSelected(null)} t={t} />
          <div className="absolute right-6 top-6 z-20 flex items-center gap-2">
            {query.hasNextPage ? <button type="button" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()} className="rounded border border-border bg-bg-elevated px-3 py-1.5 text-xs shadow-lg hover:bg-bg-subtle" data-testid="collaboration-load-more">{query.isFetchingNextPage ? t('insight.collaboration.loadingMore') : t('insight.collaboration.loadMore')}</button> : null}
            <CollaborationLODNotice view={activeGraph} canLoadMore={query.hasNextPage} loadingMore={query.isFetchingNextPage} onLoadMore={() => void query.fetchNextPage()} onShowFull={showFullGraph} t={t} />
            <Timeline effects={effects} onSelect={setSelected} t={t} />
          </div>
        </div> : null}
      </div> : null}
      {selected ? <EvidenceDrawer effect={effect} effectIds={selected} onClose={() => setSelected(null)} t={t} /> : null}
    </section>
  );
}

function filtersFromParams(params: URLSearchParams): CollaborationFilters {
  const relation = params.get('relation_type');
  const polarity = params.get('polarity');
  const hasDetailFilter = Boolean(params.get('project_id') || params.get('plan_id') || params.get('task_id') || params.get('agent_ref') || params.get('relation_type') || params.get('polarity'));
  const lod = params.get('lod') === 'full' ? 'full' : params.get('lod') === 'cluster' ? 'cluster' : hasDetailFilter ? undefined : 'cluster';
  const maxNodes = Number(params.get('max_nodes') ?? '');
  return {
    project_id: params.get('project_id') ?? '', plan_id: params.get('plan_id') ?? undefined, task_id: params.get('task_id') ?? undefined,
    agent_ref: params.get('agent_ref') ?? undefined,
    relation_type: RELATIONS.includes(relation as CollaborationRelation) ? relation as CollaborationRelation : undefined,
    polarity: POLARITIES.includes(polarity as CollaborationPolarity) ? polarity as CollaborationPolarity : undefined,
    since: params.get('since') ?? undefined, until: params.get('until') ?? undefined, cursor: params.get('cursor') ?? undefined,
    lod, max_nodes: Number.isFinite(maxNodes) && maxNodes > 0 ? maxNodes : lod === 'cluster' ? 90 : undefined, limit: 100,
  };
}

function viewFromParams(params: URLSearchParams): CollaborationViewKind {
  const view = params.get('view');
  return COLLABORATION_VIEWS.includes(view as CollaborationViewKind) ? view as CollaborationViewKind : 'network';
}

function CollaborationViewTabs({ active, onChange, t }: { active: CollaborationViewKind; onChange: (view: CollaborationViewKind) => void; t: Translator }) {
  return (
    <div role="tablist" aria-label={t('insight.collaboration.views.label')} className="flex shrink-0 flex-wrap gap-2 rounded-lg border border-border bg-bg-surface p-2" data-testid="collaboration-view-tabs">
      {COLLABORATION_VIEWS.map((view) => (
        <button
          key={view}
          type="button"
          role="tab"
          aria-selected={active === view}
          data-testid={`collaboration-view-${view}`}
          onClick={() => onChange(view)}
          className={`rounded px-3 py-2 text-sm ${active === view ? 'bg-brand text-white' : 'text-text-muted hover:bg-bg-subtle'}`}
        >
          {t(`insight.collaboration.views.${view}`)}
        </button>
      ))}
    </div>
  );
}

type Translator = ReturnType<typeof useTranslation>['t'];
function CollaborationFiltersBar({ params, update, clearAll, t }: { params: URLSearchParams; update: (key: string, value: string, clear?: string[]) => void; clearAll: () => void; t: Translator }) {
  const projectId = params.get('project_id') ?? '';
  const planId = params.get('plan_id') ?? '';
  const projects = useProjects();
  const plans = usePlans(projectId || undefined);
  const tasks = useTasksList(projectId || undefined, { status: ['all'], sort: 'updated', dir: 'desc', page_size: 500 });
  const members = useMembers();
  const projectMembers = useProjectMembers(projectId || undefined);
  const projectOptions = useMemo<EntityOption[]>(() => (projects.data ?? [])
    .map((project) => ({ value: project.id, label: project.name || project.id, hint: project.id, badge: project.status }))
    .sort((a, b) => a.label.localeCompare(b.label) || a.value.localeCompare(b.value)), [projects.data]);
  const planOptions = useMemo<EntityOption[]>(() => (plans.data ?? [])
    .filter((plan) => !plan.is_builtin)
    .map((plan) => ({ value: plan.id, label: plan.name || plan.org_ref || plan.id, hint: plan.org_ref ? `${plan.org_ref} · ${plan.id}` : plan.id, badge: plan.status }))
    .sort((a, b) => a.label.localeCompare(b.label) || a.value.localeCompare(b.value)), [plans.data]);
  const taskOptions = useMemo<EntityOption[]>(() => (tasks.data?.items ?? [])
    .filter((task) => !planId || task.plan_id === planId)
    .map((task) => ({ value: task.id, label: task.title || task.org_ref || task.id, hint: task.org_ref ? `${task.org_ref} · ${task.id}` : task.id, badge: task.status }))
    .sort((a, b) => a.label.localeCompare(b.label) || a.value.localeCompare(b.value)), [tasks.data?.items, planId]);
  const agentOptions = useMemo<EntityOption[]>(() => {
    const directory = new Map((members.data ?? []).map((member) => [identityRefOf({ kind: member.kind, identity_id: member.identity_id }), member]));
    return (projectMembers.data ?? [])
      .map((member) => identityRefOf({ kind: refKind(member.identity_id), identity_id: member.identity_id }))
      .filter((ref) => ref.startsWith('agent:'))
      .map((ref) => {
        const member = directory.get(ref);
        return { value: ref, label: member?.display_name ?? normalizeIdentityRef(ref), hint: ref, badge: 'agent' };
      })
      .sort((a, b) => a.label.localeCompare(b.label) || a.value.localeCompare(b.value));
  }, [members.data, projectMembers.data]);
  const fields = [
    ['since', t('insight.collaboration.filters.since'), 'datetime-local'],
    ['until', t('insight.collaboration.filters.until'), 'datetime-local'],
  ];
  const activeCount = ['project_id', 'plan_id', 'task_id', 'agent_ref', 'since', 'until', 'relation_type', 'polarity'].filter((key) => params.get(key)).length;
  return <form aria-label={t('insight.collaboration.filters.label')} className="shrink-0 rounded-lg border border-border bg-bg-surface p-2" data-testid="collaboration-filter-toolbar" onSubmit={(e) => e.preventDefault()}>
    <div className="flex min-w-0 flex-wrap items-end gap-2">
      <div className="min-w-[15rem] flex-1">
        <EntityFilter
          name="project_id"
          label={t('insight.collaboration.filters.project')}
          value={projectId}
          options={projectOptions}
          disabled={projects.isLoading}
          placeholder={projects.isLoading ? t('insight.collaboration.filters.loadingProjects') : t('insight.collaboration.filters.chooseProject')}
          searchPlaceholder={t('insight.collaboration.filters.searchProjects')}
          emptyLabel={t('insight.collaboration.filters.noProjects')}
          update={(key, value) => update(key, value, value !== projectId ? ['plan_id', 'task_id', 'agent_ref'] : [])}
        />
      </div>
      <div className="min-w-[13rem] flex-1">
        <EntityFilter
          name="task_id"
          label={t('insight.collaboration.filters.task')}
          value={params.get('task_id') ?? ''}
          options={taskOptions}
          disabled={!projectId || tasks.isLoading}
          placeholder={!projectId ? t('insight.collaboration.filters.chooseProjectFirst') : tasks.isLoading ? t('insight.collaboration.filters.loadingTasks') : t('insight.collaboration.filters.chooseTask')}
          searchPlaceholder={t('insight.collaboration.filters.searchTasks')}
          emptyLabel={t('insight.collaboration.filters.noTasks')}
          update={update}
        />
      </div>
      <details className="group relative shrink-0">
        <summary className="flex h-9 cursor-pointer list-none items-center gap-2 rounded border border-border px-3 text-xs text-text-primary hover:bg-bg-subtle">
          <span>{t('insight.collaboration.filters.more')}</span>
          {activeCount ? <span aria-label={t('insight.collaboration.filters.active')}>{`(${activeCount})`}</span> : null}
          {activeCount ? <span className="rounded bg-brand px-1.5 py-0.5 text-[10px] font-semibold text-white">{t('insight.collaboration.filters.active')}</span> : null}
        </summary>
        <div className="absolute right-0 z-20 mt-2 grid w-[min(48rem,calc(100vw-2rem))] max-w-[calc(100vw-2rem)] gap-3 rounded-lg border border-border bg-bg-elevated p-3 shadow-xl sm:grid-cols-2 lg:grid-cols-3">
          <EntityFilter
            name="plan_id"
            label={t('insight.collaboration.filters.plan')}
            value={planId}
            options={planOptions}
            disabled={!projectId || plans.isLoading}
            placeholder={!projectId ? t('insight.collaboration.filters.chooseProjectFirst') : t('insight.collaboration.filters.anyPlan')}
            searchPlaceholder={t('insight.collaboration.filters.searchPlans')}
            emptyLabel={t('insight.collaboration.filters.noPlans')}
            update={(key, value) => update(key, value, ['task_id'])}
          />
          <EntityFilter
            name="agent_ref"
            label={t('insight.collaboration.filters.agent')}
            value={params.get('agent_ref') ?? ''}
            options={agentOptions}
            disabled={!projectId || projectMembers.isLoading || members.isLoading}
            placeholder={!projectId ? t('insight.collaboration.filters.chooseProjectFirst') : t('insight.collaboration.filters.anyAgent')}
            searchPlaceholder={t('insight.collaboration.filters.searchAgents')}
            emptyLabel={t('insight.collaboration.filters.noAgents')}
            update={update}
          />
          {fields.map(([name, label, type]) => <label key={name} className="text-xs text-text-muted">{label}<input aria-label={label} type={type} value={dateTimeInputValue(params.get(name))} onChange={(e) => update(name, dateTimeInputToRFC3339(e.target.value))} className="mt-1 w-full rounded border border-border bg-bg-primary px-2 py-1.5 text-sm text-text-primary" /></label>)}
          <SelectFilter name="relation_type" label={t('insight.collaboration.filters.relation')} values={RELATIONS} value={params.get('relation_type') ?? ''} update={update} t={t} />
          <SelectFilter name="polarity" label={t('insight.collaboration.filters.polarity')} values={POLARITIES} value={params.get('polarity') ?? ''} update={update} t={t} />
        </div>
      </details>
      <button type="button" onClick={clearAll} className="h-9 shrink-0 rounded border border-border px-3 text-xs hover:bg-bg-subtle">{t('insight.collaboration.filters.clearAll')}</button>
    </div>
  </form>;
}

function EntityFilter({ name, label, value, options, disabled, placeholder, searchPlaceholder, emptyLabel, update }: { name: string; label: string; value: string; options: EntityOption[]; disabled?: boolean; placeholder: string; searchPlaceholder: string; emptyLabel: string; update: (key: string, value: string) => void }) {
  return <label className="text-xs text-text-muted">{label}<div className="mt-1 flex gap-2"><div className="min-w-0 flex-1"><EntitySelect testId={`collaboration-${name}`} ariaLabel={label} value={value} options={options} onChange={(next) => update(name, next)} disabled={disabled} placeholder={placeholder} searchPlaceholder={searchPlaceholder} emptyLabel={emptyLabel} /></div>{value ? <button type="button" onClick={() => update(name, '')} className="shrink-0 rounded border border-border px-2 text-sm text-text-muted hover:bg-bg-subtle" aria-label={`Clear ${label}`}>×</button> : null}</div></label>;
}

function SelectFilter({ name, label, values, value, update, t }: { name: string; label: string; values: string[]; value: string; update: (k: string, v: string) => void; t: Translator }) {
  return <label className="text-xs text-text-muted">{label}<select aria-label={label} value={value} onChange={(e) => update(name, e.target.value)} className="mt-1 w-full rounded border border-border bg-bg-primary px-2 py-1.5 text-sm text-text-primary"><option value="">{t('insight.collaboration.filters.all')}</option>{values.map((item) => <option key={item} value={item}>{labelFor(t, item)}</option>)}</select></label>;
}

function dateTimeInputValue(value: string | null): string {
  if (!value) return '';
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${parsed.getFullYear()}-${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())}T${pad(parsed.getHours())}:${pad(parsed.getMinutes())}`;
}

function dateTimeInputToRFC3339(value: string): string {
  if (!value) return '';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toISOString();
}

function Summary({ summary, t }: { summary: { positive_count: number; negative_count: number; neutral_count: number; mixed_count: number; affected_task_count: number }; t: Translator }) {
  return <section aria-label={t('insight.collaboration.summary')} className="grid min-w-0 grid-cols-5 gap-2 overflow-hidden">{(['positive', 'negative', 'neutral', 'mixed'] as const).map((key) => <div key={key} className="min-w-0 rounded border border-border bg-bg-surface px-3 py-2"><span className="block truncate text-xs text-text-muted">{labelFor(t, key)}</span><strong className="block text-base">{summary[`${key}_count`]}</strong></div>)}<div className="min-w-0 rounded border border-border bg-bg-surface px-3 py-2"><span className="block truncate text-xs text-text-muted">{t('insight.collaboration.affectedTasks')}</span><strong className="block text-base">{summary.affected_task_count}</strong></div></section>;
}

function dedupeBy<T>(items: T[], key: (item: T) => string): T[] {
  return [...new Map(items.map((item) => [key(item), item])).values()];
}

function summarizeEffects(effects: Array<{ polarity: CollaborationPolarity; target_task_id: string }>) {
  const count = (polarity: CollaborationPolarity) => effects.filter((effect) => effect.polarity === polarity).length;
  return { positive_count: count('positive'), negative_count: count('negative'), neutral_count: count('neutral'), mixed_count: count('mixed'), affected_task_count: new Set(effects.map((effect) => effect.target_task_id)).size };
}

function accumulateGraph(pages: CollaborationGraphResponse[]): CollaborationGraphView {
  const evidenceIdsByEdge = new Map<string, Set<string>>();
  const effectIdsByEdge = new Map<string, Set<string>>();
  const effectScopesByEdge = new Map<string, Map<string, CollaborationEffectScope>>();
  for (const page of pages) {
    for (const effect of page.effects) {
      const target = effect.target_agent_ref || (effect.target_task_id ? `task:${effect.target_task_id}` : effect.target);
      const key = semanticEdgeKey(effect.source_agent_ref || effect.source, target, effect.relation_type, effect.polarity);
      const ids = evidenceIdsByEdge.get(key) ?? new Set<string>();
      effect.evidence_event_ids.forEach((id) => ids.add(id));
      evidenceIdsByEdge.set(key, ids);
      const effectIds = effectIdsByEdge.get(key) ?? new Set<string>();
      effectIds.add(effect.effect_id);
      effectIdsByEdge.set(key, effectIds);
      const scopes = effectScopesByEdge.get(key) ?? new Map<string, CollaborationEffectScope>();
      scopes.set(effect.effect_id, { effect_id: effect.effect_id, project_id: effect.project_id });
      effectScopesByEdge.set(key, scopes);
    }
  }
  const mergedEdges = new Map<string, CollaborationEdge>();
  for (const edge of pages.flatMap((page) => page.graph.edges)) {
    const key = semanticEdgeKey(edge.source, edge.target, edge.relation_type, edge.polarity);
    const effectIds = sortedEdgeEffectIds(edge, effectIdsByEdge.get(key));
    const effectScopes = sortedEdgeEffectScopes(edge, effectScopesByEdge.get(key));
    const current = mergedEdges.get(key);
    if (!current) {
      mergedEdges.set(key, { ...edge, effect_ids: effectIds, effect_scopes: effectScopes, evidence_count: evidenceIdsByEdge.get(key)?.size ?? edge.evidence_count });
      continue;
    }
    const first = minDate(current.first_occurred_at, edge.first_occurred_at);
    const last = maxDate(current.last_occurred_at, edge.last_occurred_at);
    mergedEdges.set(key, {
      ...current,
      magnitude: Math.max(current.magnitude, edge.magnitude) as CollaborationEdge['magnitude'],
      interaction_count: current.interaction_count + edge.interaction_count,
      evidence_count: evidenceIdsByEdge.get(key)?.size ?? current.evidence_count + edge.evidence_count,
      first_occurred_at: first,
      last_occurred_at: last,
      effect_id: current.effect_id || edge.effect_id,
      effect_ids: sortedEdgeEffectIds(current, new Set(effectIds)),
      effect_scopes: sortedEdgeEffectScopes(current, new Map((effectScopes ?? []).map((scope) => [scope.effect_id, scope]))),
    });
  }
  return {
    // A cursor page can have a different graph_version because the backend
    // includes that page's effects in the snapshot hash. Stable entity/edge
    // ids are therefore the cross-page identity; duplicate static graph data
    // is collapsed while newly paged effects remain visible.
    nodes: dedupeBy(pages.flatMap((page) => page.graph.nodes), (node) => node.id),
    edges: [...mergedEdges.values()],
    clusters: dedupeBy(pages.flatMap((page) => page.graph.clusters), (node) => node.id),
    lod: pages.some((page) => page.graph.lod === 'cluster') ? 'cluster' : 'full',
    truncated: pages.some((page) => page.truncated || page.graph.truncated),
  };
}

function buildDimensionGraph(view: CollaborationViewKind, graph: CollaborationGraphView, effects: CollaborationEffect[], t: Translator): DimensionGraphView {
  const readable = readableGraph(graph, t);
  const base = readable.lod === 'cluster' || readable.clusters.length > 0 || readable.nodes.some((node) => node.kind === 'cluster')
    ? dimensionResult(readable.nodes, readable.edges, view)
    : view === 'network'
    ? agentNetworkGraph(readable, effects)
    : view === 'impact'
      ? taskImpactGraph(readable, effects)
      : planLineageGraph(readable);
  const cropped = cropLargeGraph(base, 100, 240);
  return {
    ...cropped,
    view,
    clusters: readable.clusters,
    lod: readable.lod,
    truncated: readable.truncated || cropped.visibleNodeCount < base.nodes.length || cropped.visibleEdgeCount < base.edges.length,
  };
}

function agentNetworkGraph(graph: CollaborationGraphView, effects: CollaborationEffect[]): DimensionGraphView {
  const nodesByID = new Map(graph.nodes.filter((node) => node.kind === 'agent' || node.kind === 'cluster').map((node) => [node.id, node]));
  const targetAgents = new Map(effects.filter((effect) => effect.target_agent_ref).map((effect) => [effect.effect_id, effect.target_agent_ref]));
  const edges = graph.edges
    .map((edge) => {
      const target = nodesByID.has(edge.target) ? edge.target : edge.effect_id ? targetAgents.get(edge.effect_id) : undefined;
      if (!nodesByID.has(edge.source) || !target || !nodesByID.has(target) || edge.source === target) return null;
      return target === edge.target ? edge : { ...edge, target };
    })
    .filter((edge): edge is CollaborationEdge => Boolean(edge));
  const used = new Set(edges.flatMap((edge) => [edge.source, edge.target]));
  return dimensionResult([...nodesByID.values()].filter((node) => used.has(node.id)), edges, 'network');
}

function taskImpactGraph(graph: CollaborationGraphView, effects: CollaborationEffect[]): DimensionGraphView {
  const nodesByID = new Map(graph.nodes.filter((node) => node.kind === 'agent' || node.kind === 'task' || node.kind === 'plan').map((node) => [node.id, node]));
  const taskTargets = new Map(effects.filter((effect) => effect.target_task_id).map((effect) => [effect.effect_id, `task:${effect.target_task_id}`]));
  const edges = graph.edges
    .map((edge) => {
      const source = nodesByID.get(edge.source);
      const targetID = nodesByID.has(edge.target) ? edge.target : edge.effect_id ? taskTargets.get(edge.effect_id) : undefined;
      const target = targetID ? nodesByID.get(targetID) : undefined;
      if (!source || !target || source.kind !== 'agent' || !['task', 'plan'].includes(target.kind)) return null;
      return targetID === edge.target ? edge : { ...edge, target: targetID };
    })
    .filter((edge): edge is CollaborationEdge => Boolean(edge));
  const used = new Set(edges.flatMap((edge) => [edge.source, edge.target]));
  return dimensionResult([...nodesByID.values()].filter((node) => used.has(node.id)), edges, 'impact');
}

function planLineageGraph(graph: CollaborationGraphView): DimensionGraphView {
  const nodesByID = new Map(graph.nodes.filter((node) => ['plan', 'stage', 'task'].includes(node.kind)).map((node) => [node.id, node]));
  const edges = graph.edges.filter((edge) => {
    const source = nodesByID.get(edge.source);
    const target = nodesByID.get(edge.target);
    return Boolean(source && target && source.kind !== 'agent' && target.kind !== 'agent');
  });
  const used = new Set(edges.flatMap((edge) => [edge.source, edge.target]));
  return dimensionResult([...nodesByID.values()].filter((node) => used.has(node.id)), edges, 'lineage');
}

function dimensionResult(nodes: CollaborationNode[], edges: CollaborationEdge[], view: CollaborationViewKind): DimensionGraphView {
  const unsupported = edges.length === 0 && nodes.length === 0;
  return {
    nodes,
    edges,
    clusters: [],
    lod: 'full',
    truncated: false,
    view,
    unsupported,
    reason: unsupported ? `No ${view} relationships are present in the current API response.` : undefined,
    visibleNodeCount: nodes.length,
    visibleEdgeCount: edges.length,
  };
}

function cropLargeGraph(view: DimensionGraphView, maxNodes: number, maxEdges: number): DimensionGraphView {
  if (view.nodes.length <= maxNodes && view.edges.length <= maxEdges) return view;
  const degree = new Map<string, number>();
  for (const edge of view.edges) {
    degree.set(edge.source, (degree.get(edge.source) ?? 0) + 1);
    degree.set(edge.target, (degree.get(edge.target) ?? 0) + 1);
  }
  const ranked = [...view.nodes].sort((a, b) => (degree.get(b.id) ?? 0) - (degree.get(a.id) ?? 0) || a.id.localeCompare(b.id)).slice(0, maxNodes);
  const visible = new Set(ranked.map((node) => node.id));
  const edges = view.edges.filter((edge) => visible.has(edge.source) && visible.has(edge.target)).slice(0, maxEdges);
  return {
    ...view,
    nodes: ranked,
    edges,
    visibleNodeCount: ranked.length,
    visibleEdgeCount: edges.length,
  };
}

function selectionCompatible(selected: CollaborationEffectScope[], view: DimensionGraphView): boolean {
  const ids = new Set(selected.map((scope) => scope.effect_id));
  return view.edges.some((edge) => edgeHasAnyEffect(edge, ids));
}

function semanticEdgeKey(source: string, target: string, relation: string, polarity: string): string {
  return [source, target, relation, polarity].join('\0');
}

function sortedEdgeEffectIds(edge: CollaborationEdge, aggregated?: Set<string>): string[] | undefined {
  const ids = new Set<string>(aggregated ?? []);
  edge.effect_ids?.forEach((id) => ids.add(id));
  if (edge.effect_id) ids.add(edge.effect_id);
  return ids.size > 0 ? [...ids].sort() : undefined;
}

function sortedEdgeEffectScopes(edge: CollaborationEdge, aggregated?: Map<string, CollaborationEffectScope>): CollaborationEffectScope[] | undefined {
  const scopes = new Map<string, CollaborationEffectScope>(aggregated ?? []);
  edge.effect_scopes?.forEach((scope) => {
    if (scope.effect_id) scopes.set(scope.effect_id, scope);
  });
  if (edge.effect_id && !scopes.has(edge.effect_id)) scopes.set(edge.effect_id, { effect_id: edge.effect_id, project_id: '' });
  const sorted = [...scopes.values()].sort((a, b) => a.effect_id.localeCompare(b.effect_id) || a.project_id.localeCompare(b.project_id));
  return sorted.length > 0 ? sorted : undefined;
}

function minDate(a?: string, b?: string): string | undefined {
  if (!a) return b;
  if (!b) return a;
  return a <= b ? a : b;
}

function maxDate(a?: string, b?: string): string | undefined {
  if (!a) return b;
  if (!b) return a;
  return a >= b ? a : b;
}

function CollaborationLODNotice({ view, canLoadMore, loadingMore, onLoadMore, onShowFull, t }: { view: CollaborationGraphView; canLoadMore: boolean; loadingMore: boolean; onLoadMore: () => void; onShowFull: () => void; t: Translator }) {
  const clustered = view.lod === 'cluster' || view.clusters.length > 0;
  if (!clustered && !view.truncated) return null;
  return (
    <details className="relative" data-testid="collaboration-lod-notice">
      <summary className="cursor-pointer list-none rounded border border-border bg-bg-elevated px-3 py-1.5 text-xs shadow-lg hover:bg-bg-subtle">{clustered ? t('insight.collaboration.lod.clusteredTitle') : t('insight.collaboration.lod.truncatedTitle')}</summary>
      <div className="absolute right-0 mt-2 w-80 rounded border border-border bg-bg-elevated p-3 text-xs shadow-xl">
        <p className="text-text-muted">{t('insight.collaboration.lod.body', { nodes: view.nodes.length, edges: view.edges.length })}</p>
        <div className="mt-2 flex flex-wrap gap-2">
        {canLoadMore ? <button type="button" disabled={loadingMore} onClick={onLoadMore} className="rounded border border-border px-3 py-1.5 text-xs hover:bg-bg-subtle" data-testid="collaboration-lod-load-more">{loadingMore ? t('insight.collaboration.loadingMore') : t('insight.collaboration.lod.continueLoading')}</button> : null}
        {clustered ? <button type="button" onClick={onShowFull} className="rounded border border-border px-3 py-1.5 text-xs hover:bg-bg-subtle" data-testid="collaboration-show-full-graph">{t('insight.collaboration.lod.showFull')}</button> : null}
        </div>
      </div>
    </details>
  );
}

function CollaborationGraph({ view, selected, onSelect, onClearSelection, t }: { view: DimensionGraphView; selected: CollaborationEffectScope[] | null; onSelect: (scopes: CollaborationEffectScope[]) => void; onClearSelection: () => void; t: Translator }) {
  const { nodes, edges } = view;
  const storageKey = `insight:collaboration:pins:${view.view}`;
  const baseNodeMap = useMemo(() => layoutNodes(view), [view]);
  const [dragPositions, setDragPositions] = useState<Record<string, { x: number; y: number }>>({});
  const [collapsedNodeIds, setCollapsedNodeIds] = useState<Set<string>>(() => new Set());
  const [hoveredId, setHoveredId] = useState<string | null>(null);
  const [locateId, setLocateId] = useState('');
  const nodeMap = useMemo(() => new Map([...baseNodeMap.values()].map((node) => [node.id, { ...node, ...(dragPositions[node.id] ?? {}) }])), [baseNodeMap, dragPositions]);
  const visibleEdges = useMemo(() => edges.filter((edge) => !collapsedNodeIds.has(edge.source) && !collapsedNodeIds.has(edge.target)), [edges, collapsedNodeIds]);
  const visibleNodeIDs = useMemo(() => new Set([...visibleEdges.flatMap((edge) => [edge.source, edge.target]), ...nodes.filter((node) => !collapsedNodeIds.has(node.id) && node.kind === 'plan').map((node) => node.id)]), [nodes, visibleEdges, collapsedNodeIds]);
  const visibleNodes = useMemo(() => [...nodeMap.values()].filter((node) => !collapsedNodeIds.has(node.id) && (visibleNodeIDs.has(node.id) || edges.length === 0)), [nodeMap, collapsedNodeIds, visibleNodeIDs, edges.length]);
  const [focusedNodeId, setFocusedNodeId] = useState<string | null>(null);
  const chartHostRef = useRef<HTMLDivElement | null>(null);
  const chartRef = useRef<ECharts | null>(null);
  const selectedKey = selected?.map((scope) => `${scope.effect_id}\0${scope.project_id}`).join('\0') ?? '';
  const selectedEffectIds = useMemo(() => new Set(selected?.map((scope) => scope.effect_id) ?? []), [selected]);
  const context = useMemo(() => graphContext(visibleEdges, selectedEffectIds, focusedNodeId ?? hoveredId), [visibleEdges, focusedNodeId, hoveredId, selectedEffectIds]);
  const hasNoiseReduction = selectedEffectIds.size > 0 || Boolean(focusedNodeId || hoveredId);
  const showLabels = visibleNodes.length <= 36 && visibleEdges.length < 80;
  const communityCount = useMemo(() => view.view === 'network' ? connectedComponentCount(nodes, visibleEdges) : 0, [nodes, view.view, visibleEdges]);
  const chartOption = useMemo(() => collaborationChartOption({
    view,
    nodes: visibleNodes,
    edges: visibleEdges,
    selectedEffectIds,
    focusedNodeId,
    hoveredId,
    t,
    showLabels,
    hasNoiseReduction,
    context,
  }), [view, visibleNodes, visibleEdges, selectedEffectIds, focusedNodeId, hoveredId, t, showLabels, hasNoiseReduction, context]);
  const fit = useCallback(() => chartRef.current?.dispatchAction({ type: 'restore' }), []);
  const focusSelected = useCallback(() => {
    const id = focusedNodeId || [...context.nodes][0];
    if (!id) return;
    setFocusedNodeId(id);
    chartRef.current?.dispatchAction({ type: 'focusNodeAdjacency', seriesIndex: 0, dataIndex: visibleNodes.findIndex((node) => node.id === id) });
    chartRef.current?.dispatchAction({ type: 'showTip', seriesIndex: 0, dataIndex: visibleNodes.findIndex((node) => node.id === id) });
  }, [context.nodes, focusedNodeId, visibleNodes]);
  const reset = useCallback(() => {
    setFocusedNodeId(null);
    onClearSelection();
    setCollapsedNodeIds(new Set());
    chartRef.current?.dispatchAction({ type: 'restore' });
  }, [onClearSelection]);
  const zoom = useCallback((zoomValue: number) => {
    chartRef.current?.setOption({ series: [{ id: 'collaboration', zoom: zoomValue }] }, false);
  }, []);
  useEffect(() => {
    try {
      const parsed = JSON.parse(sessionStorage.getItem(storageKey) ?? '{}') as Record<string, { x: number; y: number }>;
      setDragPositions(parsed && typeof parsed === 'object' ? parsed : {});
    } catch {
      setDragPositions({});
    }
  }, [storageKey]);
  useEffect(() => {
    sessionStorage.setItem(storageKey, JSON.stringify(dragPositions));
  }, [dragPositions, storageKey]);
  useEffect(() => {
    const host = chartHostRef.current;
    if (!host) return undefined;
    const chart = echarts.init(host, undefined, { renderer: 'canvas' });
    chartRef.current = chart;
    (window as unknown as { __collaborationECharts?: ECharts }).__collaborationECharts = chart;
    const resize = () => chart.resize();
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(resize) : null;
    observer?.observe(host);
    window.addEventListener('resize', resize);
    return () => {
      observer?.disconnect();
      window.removeEventListener('resize', resize);
      chart.dispose();
      chartRef.current = null;
      delete (window as unknown as { __collaborationECharts?: ECharts }).__collaborationECharts;
    };
  }, []);
  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;
    chart.setOption(chartOption, true);
    chart.resize();
    const timeout = window.setTimeout(() => {
      if (chart.isDisposed()) return;
      const option = chart.getOption() as { series?: Array<{ data?: Array<EChartNodeDatum & { x?: number; y?: number }> }> } | null;
      const liveNodes = option?.series?.[0]?.data ?? [];
      (window as unknown as { __collaborationGraphDebug?: unknown }).__collaborationGraphDebug = {
        view: view.view,
        node_count: visibleNodes.length,
        edge_count: visibleEdges.length,
        nodes: liveNodes.map((node) => ({ id: node.id, name: node.name, x: node.x, y: node.y, symbol_size: node.symbolSize })),
      };
    }, 120);
    return () => window.clearTimeout(timeout);
  }, [chartOption, view.view, visibleEdges.length, visibleNodes.length]);
  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return undefined;
    let pendingNodeDrag: { id: string; x: number; y: number } | null = null;
    const persistNodePosition = (id: string, datum: EChartNodeDatum | undefined, offsetX?: number, offsetY?: number) => {
      const option = chart.getOption() as { series?: Array<{ data?: EChartNodeDatum[] }> };
      const liveDatum = option.series?.[0]?.data?.find((item) => item.id === id || item.name === id);
      const converted = typeof offsetX === 'number' && typeof offsetY === 'number'
        ? chart.convertFromPixel({ seriesIndex: 0 }, [offsetX, offsetY]) as number[] | null
        : null;
      const x = converted?.[0] ?? liveDatum?.x ?? datum?.x;
      const y = converted?.[1] ?? liveDatum?.y ?? datum?.y;
      if (typeof x !== 'number' || typeof y !== 'number') return;
      setFocusedNodeId(id);
      setDragPositions((current) => ({ ...current, [id]: { x, y } }));
    };
    const click = (params: { dataType?: string; data?: unknown; name?: string }) => {
      if (params.dataType === 'edge') {
        const edge = params.data as EChartEdgeDatum | undefined;
        const scopes = edge?.edge ? scopesForEdge(edge.edge) : [];
        if (scopes.length > 0) onSelect(scopes);
        return;
      }
      if (params.dataType === 'node') {
        setFocusedNodeId(String(params.name));
      }
    };
    const mouseover = (params: { dataType?: string; data?: unknown; name?: string }) => {
      if (params.dataType === 'edge') setHoveredId((params.data as EChartEdgeDatum | undefined)?.edge?.source ?? null);
      if (params.dataType === 'node') setHoveredId(String(params.name));
    };
    const mouseout = () => setHoveredId(null);
    const dblclick = (params: { dataType?: string; name?: string }) => {
      if (params.dataType === 'node') setCollapsedNodeIds((current) => toggleSet(current, String(params.name)));
    };
    const mousedown = (params: { dataType?: string; name?: string; event?: { offsetX?: number; offsetY?: number } }) => {
      if (params.dataType !== 'node' || typeof params.event?.offsetX !== 'number' || typeof params.event?.offsetY !== 'number') return;
      pendingNodeDrag = { id: String(params.name), x: params.event.offsetX, y: params.event.offsetY };
    };
    const mouseup = (params: { dataType?: string; name?: string; data?: unknown; event?: { offsetX?: number; offsetY?: number } }) => {
      if (params.dataType !== 'node' || !pendingNodeDrag || pendingNodeDrag.id !== String(params.name)) {
        pendingNodeDrag = null;
        return;
      }
      const dx = (params.event?.offsetX ?? pendingNodeDrag.x) - pendingNodeDrag.x;
      const dy = (params.event?.offsetY ?? pendingNodeDrag.y) - pendingNodeDrag.y;
      if (Math.hypot(dx, dy) >= 4) persistNodePosition(String(params.name), params.data as EChartNodeDatum | undefined, params.event?.offsetX, params.event?.offsetY);
      pendingNodeDrag = null;
    };
    const dragend = (params: { dataType?: string; name?: string; data?: unknown; event?: { offsetX?: number; offsetY?: number } }) => {
      if (params.dataType !== 'node') return;
      persistNodePosition(String(params.name), params.data as EChartNodeDatum | undefined, params.event?.offsetX, params.event?.offsetY);
      pendingNodeDrag = null;
    };
    chart.on('click', click);
    chart.on('mouseover', mouseover);
    chart.on('mouseout', mouseout);
    chart.on('dblclick', dblclick);
    chart.on('mousedown', mousedown);
    chart.on('mouseup', mouseup);
    chart.on('dragend', dragend);
    return () => {
      chart.off('click', click);
      chart.off('mouseover', mouseover);
      chart.off('mouseout', mouseout);
      chart.off('dblclick', dblclick);
      chart.off('mousedown', mousedown);
      chart.off('mouseup', mouseup);
      chart.off('dragend', dragend);
    };
  }, [onSelect]);
  const locateOptions = visibleNodes.map((node) => ({ value: node.id, label: `${node.label} (${node.kind})` }));
  const focusLocated = () => {
    if (!locateId) return;
    setFocusedNodeId(locateId);
    chartRef.current?.dispatchAction({ type: 'focusNodeAdjacency', seriesIndex: 0, dataIndex: visibleNodes.findIndex((node) => node.id === locateId) });
  };
  const renderEdgeButtons = () => visibleEdges.filter((edge) => edge.effect_id || edge.interaction_count > 0).slice(0, EDGE_LIST_RENDER_LIMIT).map((edge) => {
    const scopes = scopesForEdge(edge);
    const key = scopes.map((scope) => `${scope.effect_id}\0${scope.project_id}`).join('\0');
    return <button key={edge.id} type="button" disabled={scopes.length === 0} aria-pressed={selectedKey === key} onClick={() => scopes.length > 0 && onSelect(scopes)} onMouseEnter={() => setHoveredId(edge.source)} onMouseLeave={() => setHoveredId(null)} className="rounded border border-border px-3 py-2 text-left text-sm hover:bg-bg-subtle focus:ring-2 focus:ring-brand disabled:cursor-default"><strong>{labelFor(t, edge.relation_type)}</strong> · {labelFor(t, edge.polarity)} · {t('insight.collaboration.magnitude', { value: edge.magnitude })} · {t('insight.collaboration.aggregatedEffects', { count: edge.interaction_count })} · evidence {edge.evidence_count}{edge.last_occurred_at ? ` · ${new Date(edge.last_occurred_at).toLocaleString()}` : ''}</button>;
  });
  return <section className="relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-border bg-bg-surface p-2" aria-label={t('insight.collaboration.graph')} data-testid="collaboration-graph">
    <div className="absolute left-4 top-4 z-10 flex max-w-[calc(100%-2rem)] flex-wrap items-center gap-2" data-testid="collaboration-graph-toolbar">
      <div className="flex min-w-0 flex-wrap gap-2 rounded border border-border bg-bg-elevated/95 px-2 py-1.5 text-xs text-text-muted shadow-lg">
        <span>{t(`insight.collaboration.views.${view.view}`)}</span>
        {view.view === 'network' ? <span data-testid="collaboration-network-communities">{t('insight.collaboration.legend.communities', { count: communityCount })}</span> : null}
        {view.truncated ? <span>{t('insight.collaboration.lod.cropped', { nodes: view.visibleNodeCount, edges: view.visibleEdgeCount })}</span> : null}
      </div>
      <details className="relative">
        <summary className="h-8 cursor-pointer list-none rounded border border-border bg-bg-elevated/95 px-2 py-1.5 text-xs text-text-primary shadow-lg hover:bg-bg-subtle">{t('insight.collaboration.legend.relationship')}</summary>
        <div className="absolute left-0 mt-2 w-56 rounded border border-border bg-bg-elevated p-3 text-xs text-text-muted shadow-xl">
          <p>━━ {t('insight.collaboration.legend.relationship')}</p>
          <p className="mt-1">┄┄ {t('insight.collaboration.legend.effect')}</p>
          <p className="mt-1">+/− {t('insight.collaboration.legend.mixed')}</p>
        </div>
      </details>
      <div className="flex min-w-0 flex-wrap items-center gap-1 rounded border border-border bg-bg-elevated/95 p-1 shadow-lg" aria-label={t('insight.collaboration.viewport.controls')}>
        <select aria-label="Locate" className="h-8 max-w-[13rem] rounded border border-border bg-bg-primary px-2 text-xs text-text-primary" value={locateId} onChange={(event) => setLocateId(event.target.value)} data-testid="collaboration-locate">
          <option value="">Locate</option>
          {locateOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={focusLocated} disabled={!locateId}>Go</button>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={() => zoom(1.22)} aria-label={t('insight.collaboration.viewport.zoomIn')}>+</button>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={() => zoom(0.82)} aria-label={t('insight.collaboration.viewport.zoomOut')}>-</button>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={focusSelected} disabled={context.nodes.size === 0}>{t('insight.collaboration.viewport.focus')}</button>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={fit}>{t('insight.collaboration.viewport.fit')}</button>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={reset}>{t('insight.collaboration.viewport.reset')}</button>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={() => setDragPositions({})}>{t('insight.collaboration.viewport.unpin')}</button>
      </div>
    </div>
    <div
      ref={chartHostRef}
      className="min-h-[340px] min-w-0 flex-1 touch-none rounded border border-border bg-bg-primary"
      role="img"
      aria-label={t('insight.collaboration.graph')}
      data-testid="collaboration-echarts"
    />
    <div className="absolute bottom-4 left-4 z-10 flex max-w-[calc(100%-2rem)] flex-wrap gap-2">
      <button type="button" className="rounded border border-border px-3 py-1.5 text-xs hover:bg-bg-subtle" onClick={() => focusedNodeId && setCollapsedNodeIds((current) => toggleSet(current, focusedNodeId))} disabled={!focusedNodeId} data-testid="collaboration-collapse-focus">{t('insight.collaboration.viewport.collapse')}</button>
      <button type="button" className="rounded border border-border px-3 py-1.5 text-xs hover:bg-bg-subtle" onClick={() => setCollapsedNodeIds(new Set())} disabled={collapsedNodeIds.size === 0} data-testid="collaboration-expand-all">{t('insight.collaboration.viewport.expand')}</button>
    </div>
    <div className="sr-only" aria-label="Keyboard-accessible graph nodes">
      {visibleNodes.map((node) => <button
        key={node.id}
        type="button"
        aria-pressed={focusedNodeId === node.id}
        onFocus={() => setHoveredId(node.id)}
        onBlur={() => setHoveredId(null)}
        onClick={() => setFocusedNodeId(node.id)}
        onDoubleClick={() => setCollapsedNodeIds((current) => toggleSet(current, node.id))}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') setFocusedNodeId(node.id);
          if (event.key === 'Backspace' || event.key === 'Delete') setCollapsedNodeIds((current) => toggleSet(current, node.id));
        }}
      >{node.label}</button>)}
    </div>
    <details className="absolute bottom-4 right-4 z-10" data-testid="collaboration-edge-drawer">
      <summary className="cursor-pointer list-none rounded border border-border bg-bg-elevated/95 px-3 py-1.5 text-xs text-text-primary shadow-lg hover:bg-bg-subtle">{t('insight.collaboration.edgeList')}</summary>
      <div className="mt-2 grid max-h-[min(22rem,55vh)] w-[min(40rem,calc(100vw-2rem))] gap-2 overflow-auto rounded border border-border bg-bg-elevated p-3 shadow-xl md:grid-cols-2">{renderEdgeButtons()}</div>
    </details>
    <div className="sr-only" aria-label={t('insight.collaboration.edgeList')}>{renderEdgeButtons()}</div>
    <div className="sr-only" data-testid="collaboration-rendered-labels">{visibleNodes.map((node) => node.label).join(' ')} {visibleEdges.map((edge) => `${labelFor(t, edge.relation_type)} ${labelFor(t, edge.polarity)}`).join(' ')} {view.lod === 'cluster' ? t('insight.collaboration.lod.overviewBadge') : ''}</div>
  </section>;
}

type EChartNodeDatum = {
  id: string;
  name: string;
  value: string;
  category: string;
  x?: number;
  y?: number;
  fixed?: boolean;
  draggable?: boolean;
  symbolSize?: number | number[];
  node: PositionedNode;
};

type EChartEdgeDatum = {
  source: string;
  target: string;
  value: number;
  edge: CollaborationEdge;
};

function collaborationChartOption({ view, nodes, edges, selectedEffectIds, focusedNodeId, hoveredId, t, showLabels, hasNoiseReduction, context }: {
  view: DimensionGraphView;
  nodes: PositionedNode[];
  edges: CollaborationEdge[];
  selectedEffectIds: Set<string>;
  focusedNodeId: string | null;
  hoveredId: string | null;
  t: Translator;
  showLabels: boolean;
  hasNoiseReduction: boolean;
  context: { nodes: Set<string>; edges: Set<string> };
}): CollaborationChartOption {
  const large = nodes.length >= 500 || edges.length >= 900;
  const veryLarge = nodes.length >= 2000 || edges.length >= 2500;
  const compactNodes = nodes.length > 48;
  const categories = ['agent', 'task', 'plan', 'stage', 'project', 'cluster'].map((name) => ({ name, itemStyle: { color: NODE_COLORS[name as CollaborationNode['kind']] } }));
  const data: EChartNodeDatum[] = nodes.map((node) => {
    const active = !hasNoiseReduction || context.nodes.has(node.id);
    const pinned = Boolean(node.x !== basePositionFor(node, view)?.x || node.y !== basePositionFor(node, view)?.y);
    return {
      id: node.id,
      name: node.id,
      value: node.label,
      category: node.kind,
      x: node.x,
      y: node.y,
      fixed: view.view !== 'network' || pinned,
      draggable: true,
      node,
      symbol: node.kind === 'agent' ? 'circle' : 'roundRect',
      symbolSize: compactNodes
        ? node.kind === 'cluster' || node.kind === 'plan' ? [72, 28] : node.kind === 'agent' ? 28 : [24, 18]
        : node.kind === 'cluster' || node.kind === 'plan' ? [138, 42] : node.kind === 'agent' ? 44 : [104, 36],
      itemStyle: {
        color: NODE_COLORS[node.kind],
        opacity: active ? 0.95 : 0.16,
        borderColor: focusedNodeId === node.id || hoveredId === node.id ? '#111827' : '#ffffff',
        borderWidth: focusedNodeId === node.id ? 3 : 1,
      },
      label: {
        show: showLabels || focusedNodeId === node.id,
        formatter: truncateLabel(node.label, node.kind === 'cluster' || node.kind === 'plan' ? 28 : 18),
        color: '#111827',
        fontSize: 11,
      },
      emphasis: { focus: 'adjacency', label: { show: true, formatter: node.label } },
    };
  });
  const links: EChartEdgeDatum[] = edges.map((edge) => {
    const active = !hasNoiseReduction || context.edges.has(edge.id);
    const selected = selectedEffectIds.size > 0 && edgeHasAnyEffect(edge, selectedEffectIds);
    const structural = !edge.effect_id && edge.evidence_count === 0;
    return {
      source: edge.source,
      target: edge.target,
      value: Math.max(1, edge.interaction_count),
      edge,
      lineStyle: {
        color: EDGE_COLORS[edge.polarity],
        width: selected ? edge.magnitude + 3 : structural ? 1.2 : edge.magnitude + 1,
        opacity: active ? 0.82 : 0.12,
        type: structural || edge.polarity === 'neutral' ? 'dashed' : 'solid',
        curveness: view.view === 'network' ? 0.18 : 0.08,
      },
      label: {
        show: showLabels && active && !veryLarge,
        formatter: `${labelFor(t, edge.relation_type)}${structural ? '' : ` · ${labelFor(t, edge.polarity)}`}`,
        color: '#475569',
        fontSize: 10,
      },
      emphasis: { lineStyle: { opacity: 1, width: edge.magnitude + 4 } },
    };
  });
  return {
    backgroundColor: 'transparent',
    animation: !large,
    animationThreshold: 450,
    legend: { show: false },
    tooltip: {
      confine: true,
      formatter: (params) => {
        const item = params as { dataType?: string; data?: EChartNodeDatum | EChartEdgeDatum };
        if (item.dataType === 'edge') {
          const edge = (item.data as EChartEdgeDatum).edge;
          return `${labelFor(t, edge.relation_type)}<br/>${labelFor(t, edge.polarity)} · ${t('insight.collaboration.magnitude', { value: edge.magnitude })}<br/>${t('insight.collaboration.aggregatedEffects', { count: edge.interaction_count })} · evidence ${edge.evidence_count}`;
        }
        const node = (item.data as EChartNodeDatum).node;
        return `${node.label}<br/>${labelFor(t, node.kind)}`;
      },
    },
    series: [{
      id: 'collaboration',
      type: 'graph',
      layout: view.view === 'network' ? 'force' : 'none',
      coordinateSystem: undefined,
      data,
      links,
      categories,
      roam: true,
      draggable: true,
      edgeSymbol: ['none', 'arrow'],
      edgeSymbolSize: large ? 4 : 7,
      edgeLabel: { show: showLabels && !large },
      left: 12,
      right: 12,
      top: 42,
      bottom: 18,
      labelLayout: { hideOverlap: true },
      scaleLimit: { min: 0.18, max: 6 },
      zoom: 1,
      force: {
        repulsion: veryLarge ? 120 : large ? 180 : 340,
        gravity: view.view === 'network' ? 0.08 : 0.02,
        edgeLength: veryLarge ? [45, 120] : [80, 210],
        layoutAnimation: !large,
      },
      progressive: large ? 700 : 0,
      progressiveThreshold: 500,
      autoCurveness: view.view === 'network',
      emphasis: { focus: 'adjacency' },
    }],
  };
}

function basePositionFor(node: PositionedNode, view: DimensionGraphView): { x: number; y: number } | undefined {
  return layoutNodes(view).get(node.id);
}

function readableGraph(view: CollaborationGraphView, t: Translator): CollaborationGraphView & { clusteredOverview: boolean } {
  const exceedsReadableSize = view.lod !== 'full' && (view.nodes.length > 90 || view.edges.length > 180);
  const shouldCluster = view.lod === 'cluster' || view.truncated || exceedsReadableSize;
  if (!shouldCluster) return { ...view, clusteredOverview: false };
  const groups = new Map<string, CollaborationNode & { count: number }>();
  const nodeToGroup = new Map<string, string>();
  const labelForGroup = (node: CollaborationNode) => {
    if (node.kind === 'project') return node.label;
    if (node.project_id) return `${node.project_id} ${labelFor(t, node.kind)}`;
    return labelFor(t, node.kind);
  };
  for (const node of view.nodes) {
    const groupID = node.kind === 'project' ? node.id : node.project_id ? `cluster:${node.project_id}:${node.kind}` : `cluster:global:${node.kind}`;
    nodeToGroup.set(node.id, groupID);
    const current = groups.get(groupID);
    if (current) {
      current.count += 1;
      current.label = `${labelForGroup(node)} (${current.count})`;
    } else {
      groups.set(groupID, { id: groupID, kind: node.kind === 'project' ? 'project' : 'cluster', label: `${labelForGroup(node)} (1)`, project_id: node.project_id, count: 1 });
    }
  }
  const groupedEdges = new Map<string, CollaborationEdge>();
  for (const edge of view.edges) {
    const source = nodeToGroup.get(edge.source) ?? edge.source;
    const target = nodeToGroup.get(edge.target) ?? edge.target;
    if (source === target) continue;
    const key = semanticEdgeKey(source, target, edge.relation_type, edge.polarity);
    const current = groupedEdges.get(key);
    if (!current) {
      groupedEdges.set(key, { ...edge, id: `cluster:${edge.id}`, source, target, clustered: true });
      continue;
    }
    current.interaction_count += Math.max(1, edge.interaction_count);
    current.evidence_count += edge.evidence_count;
    current.magnitude = Math.max(current.magnitude, edge.magnitude) as CollaborationEdge['magnitude'];
    current.effect_ids = sortedUnique([...(current.effect_ids ?? []), ...(edge.effect_ids ?? []), edge.effect_id ?? '']);
    current.effect_scopes = sortedScopes([...(current.effect_scopes ?? []), ...(edge.effect_scopes ?? [])]);
  }
  return { ...view, nodes: [...groups.values()].map(({ count, ...node }) => node), edges: [...groupedEdges.values()], clusteredOverview: true };
}

function sortedUnique(values: string[]): string[] | undefined {
  const out = [...new Set(values.filter(Boolean))].sort();
  return out.length ? out : undefined;
}

function sortedScopes(scopes: CollaborationEffectScope[]): CollaborationEffectScope[] | undefined {
  const out = [...new Map(scopes.filter((scope) => scope.effect_id).map((scope) => [`${scope.effect_id}\0${scope.project_id}`, scope])).values()]
    .sort((a, b) => a.effect_id.localeCompare(b.effect_id) || a.project_id.localeCompare(b.project_id));
  return out.length ? out : undefined;
}

function layoutNodes(view: DimensionGraphView): Map<string, PositionedNode> {
  const positions = view.view === 'network' ? layoutNetwork(view.nodes, view.edges) : layoutLanes(view.nodes, view.view);
  return new Map(positions.map((node) => [node.id, node]));
}

function layoutNetwork(nodes: CollaborationNode[], edges: CollaborationEdge[]): PositionedNode[] {
  const degree = new Map<string, number>();
  edges.forEach((edge) => {
    degree.set(edge.source, (degree.get(edge.source) ?? 0) + 1);
    degree.set(edge.target, (degree.get(edge.target) ?? 0) + 1);
  });
  const ordered = [...nodes].sort((a, b) => (degree.get(b.id) ?? 0) - (degree.get(a.id) ?? 0) || a.label.localeCompare(b.label));
  const center = { x: 500, y: 340 };
  const rings = Math.max(1, Math.ceil(Math.sqrt(Math.max(1, ordered.length)) / 2));
  return ordered.map((node, index) => {
    if (ordered.length === 1) return { ...node, ...center };
    const ringIndex = Math.min(rings, Math.floor(Math.sqrt(index + 1) / 2) + 1);
    const radius = 80 + ringIndex * (260 / rings);
    const angle = (index / ordered.length) * Math.PI * 2 - Math.PI / 2;
    return { ...node, x: center.x + Math.cos(angle) * radius, y: center.y + Math.sin(angle) * radius };
  });
}

function layoutLanes(nodes: CollaborationNode[], view: CollaborationViewKind): PositionedNode[] {
  const ordered = [...nodes]
    .sort((a, b) => laneOrder(a.kind, view) - laneOrder(b.kind, view) || (a.plan_id ?? '').localeCompare(b.plan_id ?? '') || (a.stage_id ?? '').localeCompare(b.stage_id ?? '') || a.label.localeCompare(b.label))
  if (view === 'impact') return layoutImpact(ordered);
  if (view === 'lineage') return layoutLineage(ordered);
  return packByKind(ordered, { x: 120, y: 80, width: 760, height: 520 });
}

function layoutImpact(nodes: CollaborationNode[]): PositionedNode[] {
  const agents = nodes.filter((node) => node.kind === 'agent');
  const plans = nodes.filter((node) => node.kind === 'plan');
  const tasks = nodes.filter((node) => node.kind === 'task');
  const others = nodes.filter((node) => !['agent', 'plan', 'task'].includes(node.kind));
  return [
    ...spreadColumn(agents, 135, 120, 560),
    ...spreadColumn(plans, 390, 130, 540),
    ...packByKind(tasks, { x: 590, y: 90, width: 340, height: 500 }),
    ...packByKind(others, { x: 330, y: 90, width: 260, height: 500 }),
  ];
}

function layoutLineage(nodes: CollaborationNode[]): PositionedNode[] {
  const plans = nodes.filter((node) => node.kind === 'plan');
  const stages = nodes.filter((node) => node.kind === 'stage');
  const tasks = nodes.filter((node) => node.kind === 'task');
  const others = nodes.filter((node) => !['plan', 'stage', 'task'].includes(node.kind));
  return [
    ...spreadColumn(plans, 130, 130, 540),
    ...spreadColumn(stages, 390, 100, 580),
    ...packByKind(tasks, { x: 600, y: 90, width: 330, height: 500 }),
    ...packByKind(others, { x: 390, y: 120, width: 260, height: 460 }),
  ];
}

function spreadColumn(nodes: CollaborationNode[], x: number, top: number, height: number): PositionedNode[] {
  if (nodes.length === 0) return [];
  return nodes.map((node, index) => ({ ...node, x, y: top + ((index + 1) * height) / (nodes.length + 1) }));
}

function packByKind(nodes: CollaborationNode[], box: { x: number; y: number; width: number; height: number }): PositionedNode[] {
  if (nodes.length === 0) return [];
  const columns = Math.max(1, Math.ceil(Math.sqrt(nodes.length * (box.width / Math.max(1, box.height)))));
  const rows = Math.max(1, Math.ceil(nodes.length / columns));
  const cellW = box.width / columns;
  const cellH = box.height / rows;
  return nodes.map((node, index) => {
    const col = index % columns;
    const row = Math.floor(index / columns);
    const jitter = ((index * 37) % 19 - 9) / 9;
    return {
      ...node,
      x: box.x + cellW * (col + 0.5) + jitter * Math.min(12, cellW * 0.18),
      y: box.y + cellH * (row + 0.5) - jitter * Math.min(10, cellH * 0.16),
    };
  });
}

function laneOrder(kind: CollaborationNode['kind'], view: CollaborationViewKind): number {
  const order = view === 'lineage' ? ['plan', 'stage', 'task'] : ['agent', 'plan', 'task', 'stage', 'project', 'cluster'];
  const index = order.indexOf(kind);
  return index >= 0 ? index : order.length;
}

function graphContext(edges: CollaborationEdge[], selectedEffectIds: Set<string>, focusedNodeId: string | null): { nodes: Set<string>; edges: Set<string> } {
  const context = { nodes: new Set<string>(), edges: new Set<string>() };
  if (focusedNodeId) context.nodes.add(focusedNodeId);
  for (const edge of edges) {
    const selectedEdge = selectedEffectIds.size > 0 && edgeHasAnyEffect(edge, selectedEffectIds);
    const focusedEdge = Boolean(focusedNodeId && (edge.source === focusedNodeId || edge.target === focusedNodeId));
    if (!selectedEdge && !focusedEdge) continue;
    context.edges.add(edge.id);
    context.nodes.add(edge.source);
    context.nodes.add(edge.target);
  }
  return context;
}

function connectedComponentCount(nodes: CollaborationNode[], edges: CollaborationEdge[]): number {
  const nodeIDs = new Set(nodes.map((node) => node.id));
  const adjacency = new Map<string, string[]>();
  for (const id of nodeIDs) adjacency.set(id, []);
  for (const edge of edges) {
    if (!nodeIDs.has(edge.source) || !nodeIDs.has(edge.target)) continue;
    adjacency.get(edge.source)?.push(edge.target);
    adjacency.get(edge.target)?.push(edge.source);
  }
  const visited = new Set<string>();
  let count = 0;
  for (const id of nodeIDs) {
    if (visited.has(id)) continue;
    count += 1;
    const stack = [id];
    visited.add(id);
    while (stack.length > 0) {
      const current = stack.pop();
      for (const next of adjacency.get(current ?? '') ?? []) {
        if (visited.has(next)) continue;
        visited.add(next);
        stack.push(next);
      }
    }
  }
  return count;
}

function edgeHasAnyEffect(edge: CollaborationEdge, effectIds: Set<string>): boolean {
  if (edge.effect_id && effectIds.has(edge.effect_id)) return true;
  return (edge.effect_ids ?? edge.effect_scopes?.map((scope) => scope.effect_id) ?? []).some((id) => effectIds.has(id));
}

function scopesForEdge(edge: CollaborationEdge): CollaborationEffectScope[] {
  return edge.effect_scopes?.length ? edge.effect_scopes : edge.effect_ids?.length ? edge.effect_ids.map((id) => ({ effect_id: id, project_id: '' })) : edge.effect_id ? [{ effect_id: edge.effect_id, project_id: '' }] : [];
}

function toggleSet(source: Set<string>, value: string): Set<string> {
  const next = new Set(source);
  if (next.has(value)) next.delete(value); else next.add(value);
  return next;
}

function truncateLabel(label: string, max = 18): string {
  return label.length > max ? `${label.slice(0, max - 1)}...` : label;
}

function Timeline({ effects, onSelect, t }: { effects: { effect_id: string; project_id: string; occurred_at: string; relation_type: string; polarity: string; source_agent_ref: string; target_task_id: string }[]; onSelect: (scopes: CollaborationEffectScope[]) => void; t: Translator }) {
  const ordered = useMemo(() => [...effects].sort((a, b) => b.occurred_at.localeCompare(a.occurred_at)).slice(0, TIMELINE_RENDER_LIMIT), [effects]);
  return <details className="relative" data-testid="collaboration-timeline"><summary className="cursor-pointer list-none rounded border border-border bg-bg-elevated px-3 py-1.5 text-xs shadow-lg hover:bg-bg-subtle">{t('insight.collaboration.timeline')}</summary><section className="absolute right-0 mt-2 max-h-[min(30rem,65vh)] w-[min(32rem,calc(100vw-2rem))] overflow-y-auto rounded-lg border border-border bg-bg-elevated p-4 shadow-xl"><div className="flex flex-wrap items-center justify-between gap-2"><h2 className="font-semibold">{t('insight.collaboration.timeline')}</h2>{effects.length > ordered.length ? <span className="text-xs text-text-muted" data-testid="collaboration-timeline-limit">{t('insight.collaboration.timelineLimited', { count: ordered.length, total: effects.length })}</span> : null}</div><ol className="mt-3 border-l border-border pl-4">{ordered.map((item) => <li key={item.effect_id} className="mb-3"><button className="text-left text-sm hover:underline focus:ring-2 focus:ring-brand" onClick={() => onSelect([{ effect_id: item.effect_id, project_id: item.project_id }])}><time className="block text-xs text-text-muted">{new Date(item.occurred_at).toLocaleString()}</time>{labelFor(t, item.relation_type)} · {labelFor(t, item.polarity)} - {item.source_agent_ref} {'->'} {item.target_task_id}</button></li>)}</ol></section></details>;
}

function EvidenceDrawer({ effect, effectIds, onClose, t }: { effect: Pick<CollaborationEffect, 'project_id' | 'explanation_key' | 'before_state' | 'after_state'> | null; effectIds: CollaborationEffectScope[]; onClose: () => void; t: Translator }) {
  const query = useCollaborationEvidenceBundle(effectIds, effect?.project_id);
  return <aside role="dialog" aria-modal="true" aria-labelledby="evidence-title" className="fixed inset-y-0 right-0 z-50 w-full max-w-lg overflow-y-auto border-l border-border bg-bg-primary p-5 shadow-xl" data-testid="collaboration-evidence-drawer"><div className="flex items-center justify-between"><h2 id="evidence-title" className="text-lg font-semibold">{t('insight.collaboration.evidence.title')}</h2><button type="button" onClick={onClose} aria-label={t('insight.collaboration.evidence.close')} className="rounded border border-border px-3 py-1">×</button></div>{effect ? <div className="mt-4 text-sm"><p>{t(effect.explanation_key, { defaultValue: effect.explanation_key })}</p><pre className="mt-2 overflow-auto rounded bg-bg-subtle p-2">{JSON.stringify({ before: effect.before_state, after: effect.after_state }, null, 2)}</pre></div> : null}{query.isLoading ? <p className="mt-4">{t('insight.collaboration.evidence.loading')}</p> : null}{query.isError ? <p role="alert" className="mt-4 text-danger">{t('insight.collaboration.evidence.failed')}</p> : null}<ol className="mt-4 space-y-3">{query.data?.evidence.map((event) => <li key={event.event_id} className="rounded border border-border p-3 text-sm"><strong>{event.event_type}</strong><time className="block text-xs text-text-muted">{new Date(event.occurred_at).toLocaleString()}</time><p>{event.actor_ref}</p><pre className="mt-2 overflow-auto text-xs">{JSON.stringify(event.payload, null, 2)}</pre></li>)}</ol></aside>;
}

function CollaborationError({ error, t }: { error: unknown; t: Translator }) { const forbidden = error instanceof ApiError && (error.status === 401 || error.status === 403); return <State id={forbidden ? 'collaboration-forbidden' : 'collaboration-error'} title={forbidden ? t('insight.collaboration.forbidden') : t('insight.collaboration.failed')} body={error instanceof Error ? error.message : undefined} danger />; }
function State({ id, title, body, danger = false }: { id: string; title: string; body?: string; danger?: boolean }) { return <div role={danger ? 'alert' : 'status'} data-testid={id} className={`rounded border p-4 ${danger ? 'border-danger/40 bg-danger/10' : 'border-border bg-bg-surface'}`}><strong>{title}</strong>{body ? <p className="mt-1 text-sm text-text-muted">{body}</p> : null}</div>; }
function labelFor(t: Translator, value: string): string { return t(`insight.collaboration.values.${value}`, { defaultValue: value.replaceAll('_', ' ') }); }

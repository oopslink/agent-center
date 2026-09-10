import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
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

const RELATIONS: CollaborationRelation[] = ['assign', 'reassign', 'complete', 'block', 'unblock', 'dependency_release', 'review_accept', 'review_reject'];
const POLARITIES: CollaborationPolarity[] = ['positive', 'negative', 'neutral', 'mixed'];
const COLLABORATION_VIEWS = ['network', 'impact', 'lineage'] as const;
const TIMELINE_RENDER_LIMIT = 200;
type CollaborationViewKind = typeof COLLABORATION_VIEWS[number];

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
        <div className="grid shrink-0 gap-3 lg:grid-cols-[minmax(0,1fr)_12rem] xl:grid-cols-[minmax(0,1fr)_18rem]">
          <Summary summary={summary} t={t} />
          <CollaborationLODNotice view={activeGraph} canLoadMore={query.hasNextPage} loadingMore={query.isFetchingNextPage} onLoadMore={() => void query.fetchNextPage()} onShowFull={showFullGraph} t={t} />
        </div>
        {activeGraph.unsupported ? <State id="collaboration-unsupported" title={t('insight.collaboration.unsupported')} body={activeGraph.reason ?? t('insight.collaboration.emptyBody')} /> : null}
        {!activeGraph.unsupported && activeGraph.edges.length === 0 ? <State id="collaboration-empty" title={t('insight.collaboration.empty')} body={t('insight.collaboration.emptyBody')} /> : null}
        {!activeGraph.unsupported && activeGraph.edges.length > 0 ? <div className="grid min-h-0 min-w-0 flex-1 gap-3 lg:grid-cols-[minmax(0,1fr)_12rem] xl:grid-cols-[minmax(0,1fr)_18rem]">
          <CollaborationGraph view={activeGraph} selected={selected} onSelect={setSelected} onClearSelection={() => setSelected(null)} t={t} />
          <div className="flex min-h-0 min-w-0 flex-col gap-3 overflow-hidden">
            {query.hasNextPage ? <button type="button" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()} className="shrink-0 rounded border border-border px-3 py-2 text-sm hover:bg-bg-subtle" data-testid="collaboration-load-more">{query.isFetchingNextPage ? t('insight.collaboration.loadingMore') : t('insight.collaboration.loadMore')}</button> : null}
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
    <div role="tablist" aria-label={t('insight.collaboration.views.label')} className="flex shrink-0 flex-wrap gap-1 rounded border border-border bg-bg-surface p-1" data-testid="collaboration-view-tabs">
      {COLLABORATION_VIEWS.map((view) => (
        <button
          key={view}
          type="button"
          role="tab"
          aria-selected={active === view}
          data-testid={`collaboration-view-${view}`}
          onClick={() => onChange(view)}
          className={`rounded px-3 py-1.5 text-sm ${active === view ? 'bg-brand text-white' : 'text-text-muted hover:bg-bg-subtle'}`}
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
  const secondaryActive = Boolean(planId || params.get('since') || params.get('until') || params.get('relation_type') || params.get('polarity'));
  const [advancedOpen, setAdvancedOpen] = useState(secondaryActive);
  useEffect(() => {
    if (secondaryActive) setAdvancedOpen(true);
  }, [secondaryActive]);
  return <form aria-label={t('insight.collaboration.filters.label')} className="shrink-0 rounded border border-border bg-bg-surface p-2" onSubmit={(e) => e.preventDefault()} data-testid="collaboration-filter-toolbar">
    <div className="flex min-w-0 flex-wrap items-end gap-2">
      <h2 className="mb-1 mr-1 shrink-0 text-sm font-semibold text-text-primary">{t('insight.collaboration.filters.label')}</h2>
      <div className="min-w-[12rem] flex-[1_1_13rem]">
        <EntityFilter name="project_id" label={t('insight.collaboration.filters.project')} value={projectId} options={projectOptions} disabled={projects.isLoading} placeholder={projects.isLoading ? t('insight.collaboration.filters.loadingProjects') : t('insight.collaboration.filters.chooseProject')} searchPlaceholder={t('insight.collaboration.filters.searchProjects')} emptyLabel={t('insight.collaboration.filters.noProjects')} update={(key, value) => update(key, value, value !== projectId ? ['plan_id', 'task_id', 'agent_ref'] : [])} />
      </div>
      <div className="min-w-[11rem] flex-[1_1_12rem]">
        <EntityFilter name="task_id" label={t('insight.collaboration.filters.task')} value={params.get('task_id') ?? ''} options={taskOptions} disabled={!projectId || tasks.isLoading} placeholder={!projectId ? t('insight.collaboration.filters.chooseProjectFirst') : tasks.isLoading ? t('insight.collaboration.filters.loadingTasks') : t('insight.collaboration.filters.chooseTask')} searchPlaceholder={t('insight.collaboration.filters.searchTasks')} emptyLabel={t('insight.collaboration.filters.noTasks')} update={update} />
      </div>
      <div className="min-w-[11rem] flex-[1_1_12rem]">
        <EntityFilter name="agent_ref" label={t('insight.collaboration.filters.agent')} value={params.get('agent_ref') ?? ''} options={agentOptions} disabled={!projectId || projectMembers.isLoading || members.isLoading} placeholder={!projectId ? t('insight.collaboration.filters.chooseProjectFirst') : t('insight.collaboration.filters.anyAgent')} searchPlaceholder={t('insight.collaboration.filters.searchAgents')} emptyLabel={t('insight.collaboration.filters.noAgents')} update={update} />
      </div>
      <details className="group relative shrink-0" open={advancedOpen} onToggle={(event) => setAdvancedOpen(event.currentTarget.open)}>
        <summary className="flex h-[2.125rem] cursor-pointer list-none items-center gap-2 rounded border border-border px-3 text-xs text-text-muted hover:bg-bg-subtle">
          <span>{t('insight.collaboration.filters.more')}</span>
          {secondaryActive ? <span className="rounded bg-brand px-1.5 py-0.5 text-[10px] font-semibold text-white">{t('insight.collaboration.filters.active')}</span> : null}
        </summary>
        <div className="absolute right-0 z-20 mt-2 grid w-[min(42rem,calc(100vw-2rem))] gap-3 rounded border border-border bg-bg-elevated p-3 shadow-2 md:grid-cols-3">
          <EntityFilter name="plan_id" label={t('insight.collaboration.filters.plan')} value={planId} options={planOptions} disabled={!projectId || plans.isLoading} placeholder={!projectId ? t('insight.collaboration.filters.chooseProjectFirst') : t('insight.collaboration.filters.anyPlan')} searchPlaceholder={t('insight.collaboration.filters.searchPlans')} emptyLabel={t('insight.collaboration.filters.noPlans')} update={(key, value) => update(key, value, ['task_id'])} />
          {fields.map(([name, label, type]) => <label key={name} className="text-xs text-text-muted">{label}<input aria-label={label} type={type} value={dateTimeInputValue(params.get(name))} onChange={(e) => update(name, dateTimeInputToRFC3339(e.target.value))} className="mt-1 h-[2.125rem] w-full rounded border border-border bg-bg-primary px-2 text-sm text-text-primary" /></label>)}
          <SelectFilter name="relation_type" label={t('insight.collaboration.filters.relation')} values={RELATIONS} value={params.get('relation_type') ?? ''} update={update} t={t} />
          <SelectFilter name="polarity" label={t('insight.collaboration.filters.polarity')} values={POLARITIES} value={params.get('polarity') ?? ''} update={update} t={t} />
        </div>
      </details>
      <button type="button" onClick={clearAll} className="h-[2.125rem] shrink-0 rounded border border-border px-2 text-xs hover:bg-bg-subtle">{t('insight.collaboration.filters.clearAll')}</button>
    </div>
  </form>;
}

function EntityFilter({ name, label, value, options, disabled, placeholder, searchPlaceholder, emptyLabel, update }: { name: string; label: string; value: string; options: EntityOption[]; disabled?: boolean; placeholder: string; searchPlaceholder: string; emptyLabel: string; update: (key: string, value: string) => void }) {
  return <label className="block text-xs text-text-muted">{label}<div className="mt-1 flex gap-1"><div className="min-w-0 flex-1"><EntitySelect testId={`collaboration-${name}`} ariaLabel={label} value={value} options={options} onChange={(next) => update(name, next)} disabled={disabled} placeholder={placeholder} searchPlaceholder={searchPlaceholder} emptyLabel={emptyLabel} /></div>{value ? <button type="button" onClick={() => update(name, '')} className="h-[2.125rem] shrink-0 rounded border border-border px-2 text-sm text-text-muted hover:bg-bg-subtle" aria-label={`Clear ${label}`}>×</button> : null}</div></label>;
}

function SelectFilter({ name, label, values, value, update, t }: { name: string; label: string; values: string[]; value: string; update: (k: string, v: string) => void; t: Translator }) {
  return <label className="text-xs text-text-muted">{label}<select aria-label={label} value={value} onChange={(e) => update(name, e.target.value)} className="mt-1 h-[2.125rem] w-full rounded border border-border bg-bg-primary px-2 text-sm text-text-primary"><option value="">{t('insight.collaboration.filters.all')}</option>{values.map((item) => <option key={item} value={item}>{labelFor(t, item)}</option>)}</select></label>;
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
  const cropped = cropLargeGraph(base, 520, 1250);
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
    <div className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded border border-border bg-bg-surface px-3 py-2 text-sm" data-testid="collaboration-lod-notice">
      <div className="min-w-0">
        <strong className="text-text-primary">{clustered ? t('insight.collaboration.lod.clusteredTitle') : t('insight.collaboration.lod.truncatedTitle')}</strong>
        <p className="truncate text-text-muted">{t('insight.collaboration.lod.body', { nodes: view.nodes.length, edges: view.edges.length })}</p>
      </div>
      <div className="flex flex-wrap gap-2">
        {canLoadMore ? <button type="button" disabled={loadingMore} onClick={onLoadMore} className="rounded border border-border px-3 py-1.5 text-xs hover:bg-bg-subtle" data-testid="collaboration-lod-load-more">{loadingMore ? t('insight.collaboration.loadingMore') : t('insight.collaboration.lod.continueLoading')}</button> : null}
        {clustered ? <button type="button" onClick={onShowFull} className="rounded border border-border px-3 py-1.5 text-xs hover:bg-bg-subtle" data-testid="collaboration-show-full-graph">{t('insight.collaboration.lod.showFull')}</button> : null}
      </div>
    </div>
  );
}

function CollaborationGraph({ view, selected, onSelect, onClearSelection, t }: { view: DimensionGraphView; selected: CollaborationEffectScope[] | null; onSelect: (scopes: CollaborationEffectScope[]) => void; onClearSelection: () => void; t: Translator }) {
  const { nodes, edges } = view;
  const storageKey = `insight:collaboration:pins:${view.view}`;
  const baseNodeMap = useMemo(() => layoutNodes(view), [view]);
  const [dragPositions, setDragPositions] = useState<Record<string, { x: number; y: number }>>({});
  const [collapsedNodeIds, setCollapsedNodeIds] = useState<Set<string>>(() => new Set());
  const [hoveredId, setHoveredId] = useState<string | null>(null);
  const nodeMap = useMemo(() => new Map([...baseNodeMap.values()].map((node) => [node.id, { ...node, ...(dragPositions[node.id] ?? {}) }])), [baseNodeMap, dragPositions]);
  const visibleEdges = useMemo(() => edges.filter((edge) => !collapsedNodeIds.has(edge.source) && !collapsedNodeIds.has(edge.target)), [edges, collapsedNodeIds]);
  const visibleNodeIDs = useMemo(() => new Set([...visibleEdges.flatMap((edge) => [edge.source, edge.target]), ...nodes.filter((node) => !collapsedNodeIds.has(node.id) && node.kind === 'plan').map((node) => node.id)]), [nodes, visibleEdges, collapsedNodeIds]);
  const visibleNodes = useMemo(() => [...nodeMap.values()].filter((node) => !collapsedNodeIds.has(node.id) && (visibleNodeIDs.has(node.id) || edges.length === 0)), [nodeMap, collapsedNodeIds, visibleNodeIDs, edges.length]);
  const graphBounds = useMemo(() => graphViewBox(visibleNodes), [visibleNodes]);
  const [viewport, setViewport] = useState(graphBounds);
  const [focusedNodeId, setFocusedNodeId] = useState<string | null>(null);
  const svgRef = useRef<SVGSVGElement | null>(null);
  const pointerInsideSvgRef = useRef(false);
  const lastPointerRef = useRef<{ clientX: number; clientY: number } | null>(null);
  const panRef = useRef<{ x: number; y: number; viewport: GraphViewBox } | null>(null);
  const dragRef = useRef<{ id: string; pointer: { x: number; y: number }; origin: { x: number; y: number }; moved: boolean } | null>(null);
  const selectedKey = selected?.map((scope) => `${scope.effect_id}\0${scope.project_id}`).join('\0') ?? '';
  const selectedEffectIds = useMemo(() => new Set(selected?.map((scope) => scope.effect_id) ?? []), [selected]);
  const context = useMemo(() => graphContext(visibleEdges, selectedEffectIds, focusedNodeId ?? hoveredId), [visibleEdges, focusedNodeId, hoveredId, selectedEffectIds]);
  const hasNoiseReduction = selectedEffectIds.size > 0 || Boolean(focusedNodeId || hoveredId);
  const showLabels = viewport.width < 900 && visibleEdges.length < 260;
  const communityCount = useMemo(() => view.view === 'network' ? connectedComponentCount(nodes, visibleEdges) : 0, [nodes, view.view, visibleEdges]);
  const fit = useCallback(() => setViewport(graphBounds), [graphBounds]);
  const focusSelected = useCallback(() => {
    const ids = [...context.nodes];
    if (ids.length === 0) return;
    const selectedNodes = ids.map((id) => nodeMap.get(id)).filter((node): node is PositionedNode => Boolean(node));
    setViewport(graphViewBox(selectedNodes));
  }, [context.nodes, nodeMap]);
  const reset = useCallback(() => {
    setFocusedNodeId(null);
    onClearSelection();
    setCollapsedNodeIds(new Set());
    setViewport(graphViewBox([...baseNodeMap.values()]));
  }, [baseNodeMap, onClearSelection]);
  const zoom = useCallback((factor: number, center = { x: viewport.x + viewport.width / 2, y: viewport.y + viewport.height / 2 }) => {
    setViewport((current) => zoomViewBox(current, factor, center));
  }, [viewport]);
  const toSvgPoint = useCallback((event: { clientX: number; clientY: number }) => {
    const svg = svgRef.current;
    const matrix = typeof svg?.getScreenCTM === 'function' ? svg.getScreenCTM() : null;
    if (!svg) return { x: viewport.x + viewport.width / 2, y: viewport.y + viewport.height / 2 };
    if (!matrix) {
      const rect = svg.getBoundingClientRect();
      if (rect.width > 0 && rect.height > 0) return {
        x: viewport.x + ((event.clientX - rect.left) / rect.width) * viewport.width,
        y: viewport.y + ((event.clientY - rect.top) / rect.height) * viewport.height,
      };
      return { x: viewport.x + viewport.width / 2, y: viewport.y + viewport.height / 2 };
    }
    const point = svg.createSVGPoint();
    point.x = event.clientX;
    point.y = event.clientY;
    return point.matrixTransform(matrix.inverse());
  }, [viewport]);
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
  useEffect(() => setViewport(graphBounds), [graphBounds]);
  useEffect(() => {
    const svg = svgRef.current;
    if (!svg) return undefined;
    const handler = (event: WheelEvent) => {
      event.preventDefault();
      const center = toSvgPoint(lastPointerRef.current ?? event);
      flushSync(() => setViewport((current) => zoomViewBox(current, event.deltaY > 0 ? 1.12 : 0.88, center)));
    };
    svg.addEventListener('wheel', handler, { passive: false });
    window.addEventListener('wheel', handler, { passive: false });
    return () => {
      svg.removeEventListener('wheel', handler);
      window.removeEventListener('wheel', handler);
    };
  }, [toSvgPoint]);
  return <section className="flex min-h-0 min-w-0 flex-col overflow-hidden rounded border border-border bg-bg-surface p-3" aria-label={t('insight.collaboration.graph')} data-testid="collaboration-graph">
    <div className="mb-2 flex shrink-0 flex-wrap items-center justify-between gap-2">
      <div className="flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-xs text-text-muted">
        <span>{t(`insight.collaboration.views.${view.view}`)}</span>
        <span>━━ {t('insight.collaboration.legend.relationship')}</span>
        <span>┄┄ {t('insight.collaboration.legend.effect')}</span>
        <span>+/− {t('insight.collaboration.legend.mixed')}</span>
        {view.view === 'network' ? <span data-testid="collaboration-network-communities">{t('insight.collaboration.legend.communities', { count: communityCount })}</span> : null}
        {view.truncated ? <span>{t('insight.collaboration.lod.cropped', { nodes: view.visibleNodeCount, edges: view.visibleEdgeCount })}</span> : null}
      </div>
      <div className="flex shrink-0 items-center gap-1" aria-label={t('insight.collaboration.viewport.controls')}>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={() => zoom(0.82)} aria-label={t('insight.collaboration.viewport.zoomIn')}>+</button>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={() => zoom(1.18)} aria-label={t('insight.collaboration.viewport.zoomOut')}>-</button>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={focusSelected} disabled={context.nodes.size === 0}>{t('insight.collaboration.viewport.focus')}</button>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={fit}>{t('insight.collaboration.viewport.fit')}</button>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={reset}>{t('insight.collaboration.viewport.reset')}</button>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={() => setDragPositions({})}>{t('insight.collaboration.viewport.unpin')}</button>
      </div>
    </div>
    <svg
      ref={svgRef}
      viewBox={`${viewport.x} ${viewport.y} ${viewport.width} ${viewport.height}`}
      className="min-h-[12rem] min-w-0 flex-1 touch-none cursor-grab rounded border border-border bg-bg-primary active:cursor-grabbing"
      role="img"
      aria-label={t('insight.collaboration.graph')}
      data-testid="collaboration-graph-svg"
      onMouseEnter={(event) => { pointerInsideSvgRef.current = true; lastPointerRef.current = { clientX: event.clientX, clientY: event.clientY }; }}
      onMouseMove={(event) => { pointerInsideSvgRef.current = true; lastPointerRef.current = { clientX: event.clientX, clientY: event.clientY }; }}
      onMouseLeave={() => { pointerInsideSvgRef.current = false; }}
      onPointerEnter={(event) => { pointerInsideSvgRef.current = true; lastPointerRef.current = { clientX: event.clientX, clientY: event.clientY }; }}
      onPointerDown={(event) => { lastPointerRef.current = { clientX: event.clientX, clientY: event.clientY }; panRef.current = { x: event.clientX, y: event.clientY, viewport }; event.currentTarget.setPointerCapture?.(event.pointerId); }}
      onPointerMove={(event) => {
        lastPointerRef.current = { clientX: event.clientX, clientY: event.clientY };
        const drag = dragRef.current;
        if (drag) {
          const point = toSvgPoint(event);
          const dx = point.x - drag.pointer.x;
          const dy = point.y - drag.pointer.y;
          drag.moved = drag.moved || Math.hypot(dx, dy) > 4;
          setDragPositions((current) => ({ ...current, [drag.id]: { x: drag.origin.x + dx, y: drag.origin.y + dy } }));
          return;
        }
        const pan = panRef.current;
        if (!pan) return;
        const rect = event.currentTarget.getBoundingClientRect();
        const dx = ((event.clientX - pan.x) / rect.width) * pan.viewport.width;
        const dy = ((event.clientY - pan.y) / rect.height) * pan.viewport.height;
        setViewport({ ...pan.viewport, x: pan.viewport.x - dx, y: pan.viewport.y - dy });
      }}
      onPointerUp={(event) => {
        const drag = dragRef.current;
        if (drag && !drag.moved) {
          const node = nodeMap.get(drag.id);
          if (node) {
            setFocusedNodeId(node.id);
            setViewport(focusViewBox(node));
          }
        }
        dragRef.current = null;
        panRef.current = null;
        if (event.currentTarget.hasPointerCapture?.(event.pointerId)) event.currentTarget.releasePointerCapture?.(event.pointerId);
      }}
      onPointerLeave={() => { pointerInsideSvgRef.current = false; panRef.current = null; dragRef.current = null; }}
    >
      <defs><linearGradient id="collaboration-mixed"><stop offset="0%" stopColor="#16803c"/><stop offset="50%" stopColor="#16803c"/><stop offset="50%" stopColor="#c0362c"/><stop offset="100%" stopColor="#c0362c"/></linearGradient></defs>
      {view.lod === 'cluster' ? <text x={viewport.x + 12} y={viewport.y + 24} className="fill-text-muted text-[12px]">{t('insight.collaboration.lod.overviewBadge')}</text> : null}
      {visibleEdges.map((edge) => {
        const a = nodeMap.get(edge.source);
        const b = nodeMap.get(edge.target);
        if (!a || !b) return null;
        const structural = !edge.effect_id && edge.evidence_count === 0;
        const active = !hasNoiseReduction || context.edges.has(edge.id);
        const selectedEdge = selectedEffectIds.size > 0 && edgeHasAnyEffect(edge, selectedEffectIds);
        const scopes = scopesForEdge(edge);
        return <g key={edge.id} opacity={active ? 1 : 0.16} onMouseEnter={() => setHoveredId(edge.source)} onMouseLeave={() => setHoveredId(null)}>
          <line x1={a.x} y1={a.y} x2={b.x} y2={b.y} stroke="transparent" strokeWidth="16" className="cursor-pointer" onClick={() => scopes.length > 0 && onSelect(scopes)} />
          <line x1={a.x} y1={a.y} x2={b.x} y2={b.y} className={`pointer-events-none collaboration-edge collaboration-edge--${edge.polarity}`} strokeWidth={selectedEdge ? edge.magnitude + 3 : structural ? 1.5 : edge.magnitude + 1} strokeDasharray={structural || edge.polarity === 'neutral' ? '3 5' : edge.relation_type === 'assign' ? undefined : '10 4'} />
          {showLabels && active ? <text x={(a.x+b.x)/2} y={(a.y+b.y)/2-6} textAnchor="middle" className="pointer-events-none fill-text-muted text-[11px]">{labelFor(t, edge.relation_type)}{structural ? '' : ` · ${labelFor(t, edge.polarity)}`}</text> : null}
        </g>;
      })}
      {visibleNodes.map((node) => { const active = !hasNoiseReduction || context.nodes.has(node.id); const focused = focusedNodeId === node.id; const wide = node.kind === 'cluster' || node.kind === 'plan'; return <g key={node.id} role="button" tabIndex={0} aria-label={node.label} className="cursor-pointer outline-none" opacity={active ? 1 : 0.18} onMouseEnter={() => setHoveredId(node.id)} onMouseLeave={() => setHoveredId(null)} onPointerDown={(event) => { event.stopPropagation(); lastPointerRef.current = { clientX: event.clientX, clientY: event.clientY }; const point = toSvgPoint(event); dragRef.current = { id: node.id, pointer: point, origin: { x: node.x, y: node.y }, moved: false }; svgRef.current?.setPointerCapture?.(event.pointerId); }} onDoubleClick={() => setCollapsedNodeIds((current) => toggleSet(current, node.id))} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); setFocusedNodeId(node.id); setViewport(focusViewBox(node)); } if (event.key === 'Backspace' || event.key === 'Delete') setCollapsedNodeIds((current) => toggleSet(current, node.id)); }}><title>{node.label}</title>{node.kind === 'agent' ? <circle cx={node.x} cy={node.y} r="27" fill="var(--color-bg-elevated)" stroke="var(--color-brand)" strokeWidth={focused ? 4 : dragPositions[node.id] ? 3 : 2} /> : <rect x={node.x-(wide ? 88 : 54)} y={node.y-24} width={wide ? 176 : 108} height="48" rx="5" fill="var(--color-bg-elevated)" stroke="var(--color-text-muted)" strokeWidth={focused ? 4 : dragPositions[node.id] ? 3 : 2} />}<text x={node.x} y={node.y+3} textAnchor="middle" fill="var(--color-text-primary)" className="pointer-events-none text-[11px]">{showLabels ? truncateLabel(node.label, wide ? 28 : 18) : truncateLabel(node.label, 10)}</text><text x={node.x} y={node.y+18} textAnchor="middle" fill="var(--color-text-muted)" className="pointer-events-none text-[8px]">{node.kind}</text></g>; })}
    </svg>
    <div className="mt-2 flex shrink-0 flex-wrap gap-2">
      <button type="button" className="rounded border border-border px-3 py-1.5 text-xs hover:bg-bg-subtle" onClick={() => focusedNodeId && setCollapsedNodeIds((current) => toggleSet(current, focusedNodeId))} disabled={!focusedNodeId} data-testid="collaboration-collapse-focus">{t('insight.collaboration.viewport.collapse')}</button>
      <button type="button" className="rounded border border-border px-3 py-1.5 text-xs hover:bg-bg-subtle" onClick={() => setCollapsedNodeIds(new Set())} disabled={collapsedNodeIds.size === 0} data-testid="collaboration-expand-all">{t('insight.collaboration.viewport.expand')}</button>
    </div>
    <div className="mt-2 grid max-h-[32%] shrink-0 gap-2 overflow-auto md:grid-cols-2" aria-label={t('insight.collaboration.edgeList')}>{visibleEdges.filter((edge) => edge.effect_id || edge.interaction_count > 0).map((edge) => { const scopes = scopesForEdge(edge); const key = scopes.map((scope) => `${scope.effect_id}\0${scope.project_id}`).join('\0'); return <button key={edge.id} type="button" disabled={scopes.length === 0} aria-pressed={selectedKey === key} onClick={() => scopes.length > 0 && onSelect(scopes)} onMouseEnter={() => setHoveredId(edge.source)} onMouseLeave={() => setHoveredId(null)} className="min-w-0 rounded border border-border px-3 py-2 text-left text-sm hover:bg-bg-subtle focus:ring-2 focus:ring-brand disabled:cursor-default"><strong>{labelFor(t, edge.relation_type)}</strong> · {labelFor(t, edge.polarity)} · {t('insight.collaboration.magnitude', { value: edge.magnitude })} · {t('insight.collaboration.aggregatedEffects', { count: edge.interaction_count })} · evidence {edge.evidence_count}{edge.last_occurred_at ? ` · ${new Date(edge.last_occurred_at).toLocaleString()}` : ''}</button>; })}</div>
  </section>;
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

type GraphViewBox = { x: number; y: number; width: number; height: number };

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
  const center = { x: 380, y: 260 };
  const radius = Math.max(150, Math.min(560, ordered.length * 17));
  return ordered.map((node, index) => {
    if (ordered.length === 1) return { ...node, ...center };
    const angle = (index / ordered.length) * Math.PI * 2 - Math.PI / 2;
    const ring = index < 16 ? radius * 0.55 : radius;
    return { ...node, x: center.x + Math.cos(angle) * ring, y: center.y + Math.sin(angle) * ring };
  });
}

function layoutLanes(nodes: CollaborationNode[], view: CollaborationViewKind): PositionedNode[] {
  const lanes = view === 'lineage'
    ? { plan: 90, stage: 325, task: 600, agent: 0, project: 0, cluster: 325 }
    : { agent: 65, project: 200, plan: 350, stage: 500, task: 660, cluster: 360 };
  const laneIndex = new Map<string, number>();
  return [...nodes]
    .sort((a, b) => laneOrder(a.kind, view) - laneOrder(b.kind, view) || (a.plan_id ?? '').localeCompare(b.plan_id ?? '') || (a.stage_id ?? '').localeCompare(b.stage_id ?? '') || a.label.localeCompare(b.label))
    .map((node) => {
      const i = laneIndex.get(node.kind) ?? 0;
      laneIndex.set(node.kind, i + 1);
      return { ...node, x: lanes[node.kind], y: 70 + i * 82 };
    });
}

function laneOrder(kind: CollaborationNode['kind'], view: CollaborationViewKind): number {
  const order = view === 'lineage' ? ['plan', 'stage', 'task'] : ['agent', 'plan', 'task', 'stage', 'project', 'cluster'];
  const index = order.indexOf(kind);
  return index >= 0 ? index : order.length;
}

function graphViewBox(nodes: Array<{ x: number; y: number }>): GraphViewBox {
  if (nodes.length === 0) return { x: 0, y: 0, width: 720, height: 260 };
  const xs = nodes.map((node) => node.x);
  const ys = nodes.map((node) => node.y);
  const minX = Math.min(...xs) - 80;
  const maxX = Math.max(...xs) + 80;
  const minY = Math.min(...ys) - 70;
  const maxY = Math.max(...ys) + 70;
  return { x: minX, y: minY, width: Math.max(260, maxX - minX), height: Math.max(260, maxY - minY) };
}

function zoomViewBox(box: GraphViewBox, factor: number, center: { x: number; y: number }): GraphViewBox {
  const width = Math.min(1400, Math.max(160, box.width * factor));
  const height = Math.min(1400, Math.max(160, box.height * factor));
  const x = center.x - ((center.x - box.x) / box.width) * width;
  const y = center.y - ((center.y - box.y) / box.height) * height;
  return { x, y, width, height };
}

function focusViewBox(node: { x: number; y: number }): GraphViewBox {
  return { x: node.x - 120, y: node.y - 90, width: 240, height: 180 };
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
  return <section className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded border border-border bg-bg-surface p-3" data-testid="collaboration-timeline"><div className="flex shrink-0 flex-wrap items-center justify-between gap-2"><h2 className="font-semibold">{t('insight.collaboration.timeline')}</h2>{effects.length > ordered.length ? <span className="text-xs text-text-muted" data-testid="collaboration-timeline-limit">{t('insight.collaboration.timelineLimited', { count: ordered.length, total: effects.length })}</span> : null}</div><ol className="mt-3 min-h-0 overflow-auto border-l border-border pl-4">{ordered.map((item) => <li key={item.effect_id} className="mb-3"><button className="text-left text-sm hover:underline focus:ring-2 focus:ring-brand" onClick={() => onSelect([{ effect_id: item.effect_id, project_id: item.project_id }])}><time className="block text-xs text-text-muted">{new Date(item.occurred_at).toLocaleString()}</time>{labelFor(t, item.relation_type)} · {labelFor(t, item.polarity)} — {item.source_agent_ref} → {item.target_task_id}</button></li>)}</ol></section>;
}

function EvidenceDrawer({ effect, effectIds, onClose, t }: { effect: Pick<CollaborationEffect, 'project_id' | 'explanation_key' | 'before_state' | 'after_state'> | null; effectIds: CollaborationEffectScope[]; onClose: () => void; t: Translator }) {
  const query = useCollaborationEvidenceBundle(effectIds, effect?.project_id);
  return <aside role="dialog" aria-modal="true" aria-labelledby="evidence-title" className="fixed inset-y-0 right-0 z-50 w-full max-w-lg overflow-y-auto border-l border-border bg-bg-primary p-5 shadow-xl" data-testid="collaboration-evidence-drawer"><div className="flex items-center justify-between"><h2 id="evidence-title" className="text-lg font-semibold">{t('insight.collaboration.evidence.title')}</h2><button type="button" onClick={onClose} aria-label={t('insight.collaboration.evidence.close')} className="rounded border border-border px-3 py-1">×</button></div>{effect ? <div className="mt-4 text-sm"><p>{t(effect.explanation_key, { defaultValue: effect.explanation_key })}</p><pre className="mt-2 overflow-auto rounded bg-bg-subtle p-2">{JSON.stringify({ before: effect.before_state, after: effect.after_state }, null, 2)}</pre></div> : null}{query.isLoading ? <p className="mt-4">{t('insight.collaboration.evidence.loading')}</p> : null}{query.isError ? <p role="alert" className="mt-4 text-danger">{t('insight.collaboration.evidence.failed')}</p> : null}<ol className="mt-4 space-y-3">{query.data?.evidence.map((event) => <li key={event.event_id} className="rounded border border-border p-3 text-sm"><strong>{event.event_type}</strong><time className="block text-xs text-text-muted">{new Date(event.occurred_at).toLocaleString()}</time><p>{event.actor_ref}</p><pre className="mt-2 overflow-auto text-xs">{JSON.stringify(event.payload, null, 2)}</pre></li>)}</ol></aside>;
}

function CollaborationError({ error, t }: { error: unknown; t: Translator }) { const forbidden = error instanceof ApiError && (error.status === 401 || error.status === 403); return <State id={forbidden ? 'collaboration-forbidden' : 'collaboration-error'} title={forbidden ? t('insight.collaboration.forbidden') : t('insight.collaboration.failed')} body={error instanceof Error ? error.message : undefined} danger />; }
function State({ id, title, body, danger = false }: { id: string; title: string; body?: string; danger?: boolean }) { return <div role={danger ? 'alert' : 'status'} data-testid={id} className={`rounded border p-4 ${danger ? 'border-danger/40 bg-danger/10' : 'border-border bg-bg-surface'}`}><strong>{title}</strong>{body ? <p className="mt-1 text-sm text-text-muted">{body}</p> : null}</div>; }
function labelFor(t: Translator, value: string): string { return t(`insight.collaboration.values.${value}`, { defaultValue: value.replaceAll('_', ' ') }); }

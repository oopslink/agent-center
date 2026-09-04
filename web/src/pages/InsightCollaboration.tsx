import React, { useCallback, useMemo, useRef, useState } from 'react';
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

export default function InsightCollaboration(): React.ReactElement {
  const { t } = useTranslation('insights');
  const [params, setParams] = useSearchParams();
  const [selected, setSelected] = useState<CollaborationEffectScope[] | null>(null);
  const filters = filtersFromParams(params);
  const query = useInfiniteCollaborationEffects(filters);
  const effects = useMemo(() => dedupeBy(query.data?.pages.flatMap((page) => page.effects) ?? [], (item) => item.effect_id), [query.data?.pages]);
  const selectedIds = selected?.map((item) => item.effect_id) ?? [];
  const effect = effects.find((item) => selectedIds.includes(item.effect_id)) ?? null;
  const view = useMemo(() => accumulateGraph(query.data?.pages ?? []), [query.data?.pages]);
  const summary = useMemo(() => summarizeEffects(effects), [effects]);

  const update = (key: string, value: string, clear: string[] = []) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value); else next.delete(key);
    clear.forEach((item) => next.delete(item));
    next.delete('cursor');
    setParams(next);
    setSelected(null);
  };

  return (
    <section className="space-y-4" data-testid="page-InsightCollaboration">
      <header><h1 className="text-xl font-semibold text-text-primary">{t('insight.collaboration.title')}</h1><p className="mt-1 text-sm text-text-muted">{t('insight.collaboration.subtitle')}</p></header>
      <CollaborationFiltersBar params={params} update={update} t={t} />
      {query.isLoading ? <State id="collaboration-loading" title={t('insight.collaboration.loading')} /> : null}
      {query.isError ? <CollaborationError error={query.error} t={t} /> : null}
      {query.data ? <>
        <Summary summary={summary} t={t} />
        {view.edges.length === 0 ? <State id="collaboration-empty" title={t('insight.collaboration.empty')} body={t('insight.collaboration.emptyBody')} /> :
          <CollaborationGraph nodes={view.nodes} edges={view.edges} selected={selected} onSelect={setSelected} onClearSelection={() => setSelected(null)} t={t} />}
        {query.hasNextPage ? <button type="button" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()} className="rounded border border-border px-3 py-2 text-sm" data-testid="collaboration-load-more">{query.isFetchingNextPage ? t('insight.collaboration.loadingMore') : t('insight.collaboration.loadMore')}</button> : null}
        <Timeline effects={effects} onSelect={setSelected} t={t} />
      </> : null}
      {selected ? <EvidenceDrawer effect={effect} effectIds={selected} onClose={() => setSelected(null)} t={t} /> : null}
    </section>
  );
}

function filtersFromParams(params: URLSearchParams): CollaborationFilters {
  const relation = params.get('relation_type');
  const polarity = params.get('polarity');
  return {
    project_id: params.get('project_id') ?? '', plan_id: params.get('plan_id') ?? undefined, task_id: params.get('task_id') ?? undefined,
    agent_ref: params.get('agent_ref') ?? undefined,
    relation_type: RELATIONS.includes(relation as CollaborationRelation) ? relation as CollaborationRelation : undefined,
    polarity: POLARITIES.includes(polarity as CollaborationPolarity) ? polarity as CollaborationPolarity : undefined,
    since: params.get('since') ?? undefined, until: params.get('until') ?? undefined, cursor: params.get('cursor') ?? undefined, limit: 100,
  };
}

type Translator = ReturnType<typeof useTranslation>['t'];
function CollaborationFiltersBar({ params, update, t }: { params: URLSearchParams; update: (key: string, value: string, clear?: string[]) => void; t: Translator }) {
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
  return <form aria-label={t('insight.collaboration.filters.label')} className="grid gap-3 rounded-lg border border-border bg-bg-surface p-4 md:grid-cols-4" onSubmit={(e) => e.preventDefault()}>
    <h2 className="text-sm font-semibold text-text-primary md:col-span-4">{t('insight.collaboration.filters.label')}</h2>
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
  return <section aria-label={t('insight.collaboration.summary')} className="grid grid-cols-2 gap-2 md:grid-cols-5">{(['positive', 'negative', 'neutral', 'mixed'] as const).map((key) => <div key={key} className="rounded border border-border bg-bg-surface p-3"><span className="text-xs text-text-muted">{labelFor(t, key)}</span><strong className="block text-lg">{summary[`${key}_count`]}</strong></div>)}<div className="rounded border border-border bg-bg-surface p-3"><span className="text-xs text-text-muted">{t('insight.collaboration.affectedTasks')}</span><strong className="block text-lg">{summary.affected_task_count}</strong></div></section>;
}

function dedupeBy<T>(items: T[], key: (item: T) => string): T[] {
  return [...new Map(items.map((item) => [key(item), item])).values()];
}

function summarizeEffects(effects: Array<{ polarity: CollaborationPolarity; target_task_id: string }>) {
  const count = (polarity: CollaborationPolarity) => effects.filter((effect) => effect.polarity === polarity).length;
  return { positive_count: count('positive'), negative_count: count('negative'), neutral_count: count('neutral'), mixed_count: count('mixed'), affected_task_count: new Set(effects.map((effect) => effect.target_task_id)).size };
}

function accumulateGraph(pages: CollaborationGraphResponse[]): { nodes: CollaborationNode[]; edges: CollaborationEdge[] } {
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
  };
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

function CollaborationGraph({ nodes, edges, selected, onSelect, onClearSelection, t }: { nodes: CollaborationNode[]; edges: CollaborationEdge[]; selected: CollaborationEffectScope[] | null; onSelect: (scopes: CollaborationEffectScope[]) => void; onClearSelection: () => void; t: Translator }) {
  const lanes = { agent: 65, project: 200, plan: 335, stage: 500, task: 660 };
  const laneIndex = new Map<string, number>();
  const baseNodeMap = useMemo(() => new Map(nodes.map((node) => { const i = laneIndex.get(node.kind) ?? 0; laneIndex.set(node.kind, i + 1); return [node.id, { ...node, x: lanes[node.kind], y: 70 + i * 68 }]; })), [nodes]);
  const [dragPositions, setDragPositions] = useState<Record<string, { x: number; y: number }>>({});
  const nodeMap = useMemo(() => new Map([...baseNodeMap.values()].map((node) => [node.id, { ...node, ...(dragPositions[node.id] ?? {}) }])), [baseNodeMap, dragPositions]);
  const graphBounds = useMemo(() => graphViewBox([...nodeMap.values()]), [nodeMap]);
  const [viewport, setViewport] = useState(graphBounds);
  const [focusedNodeId, setFocusedNodeId] = useState<string | null>(null);
  const svgRef = useRef<SVGSVGElement | null>(null);
  const panRef = useRef<{ x: number; y: number; viewport: GraphViewBox } | null>(null);
  const dragRef = useRef<{ id: string; pointer: { x: number; y: number }; origin: { x: number; y: number }; moved: boolean } | null>(null);
  const selectedKey = selected?.map((scope) => `${scope.effect_id}\0${scope.project_id}`).join('\0') ?? '';
  const selectedEffectIds = useMemo(() => new Set(selected?.map((scope) => scope.effect_id) ?? []), [selected]);
  const context = useMemo(() => graphContext(edges, selectedEffectIds, focusedNodeId), [edges, focusedNodeId, selectedEffectIds]);
  const hasNoiseReduction = selectedEffectIds.size > 0 || Boolean(focusedNodeId);
  const fit = useCallback(() => setViewport(graphBounds), [graphBounds]);
  const reset = useCallback(() => {
    setFocusedNodeId(null);
    onClearSelection();
    setDragPositions({});
    setViewport(graphViewBox([...baseNodeMap.values()]));
  }, [baseNodeMap, onClearSelection]);
  const zoom = useCallback((factor: number, center = { x: viewport.x + viewport.width / 2, y: viewport.y + viewport.height / 2 }) => {
    setViewport((current) => zoomViewBox(current, factor, center));
  }, [viewport]);
  const toSvgPoint = useCallback((event: { clientX: number; clientY: number }) => {
    const svg = svgRef.current;
    const matrix = svg?.getScreenCTM();
    if (!svg || !matrix) return { x: viewport.x + viewport.width / 2, y: viewport.y + viewport.height / 2 };
    const point = svg.createSVGPoint();
    point.x = event.clientX;
    point.y = event.clientY;
    return point.matrixTransform(matrix.inverse());
  }, [viewport]);
  const onWheel = useCallback((event: React.WheelEvent<SVGSVGElement>) => {
    event.preventDefault();
    zoom(event.deltaY > 0 ? 1.12 : 0.88, toSvgPoint(event));
  }, [toSvgPoint, zoom]);
  return <section className="rounded-lg border border-border bg-bg-surface p-4" aria-label={t('insight.collaboration.graph')} data-testid="collaboration-graph">
    <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
      <div className="flex flex-wrap gap-3 text-xs text-text-muted"><span>━━ {t('insight.collaboration.legend.relationship')}</span><span>┄┄ {t('insight.collaboration.legend.effect')}</span><span>+/− {t('insight.collaboration.legend.mixed')}</span></div>
      <div className="flex items-center gap-1" aria-label={t('insight.collaboration.viewport.controls')}>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={() => zoom(0.82)} aria-label={t('insight.collaboration.viewport.zoomIn')}>+</button>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={() => zoom(1.18)} aria-label={t('insight.collaboration.viewport.zoomOut')}>-</button>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={fit}>{t('insight.collaboration.viewport.fit')}</button>
        <button type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-bg-subtle" onClick={reset}>{t('insight.collaboration.viewport.reset')}</button>
      </div>
    </div>
    <svg
      ref={svgRef}
      viewBox={`${viewport.x} ${viewport.y} ${viewport.width} ${viewport.height}`}
      className="min-h-64 w-full touch-none cursor-grab rounded border border-border bg-bg-primary active:cursor-grabbing"
      role="img"
      aria-label={t('insight.collaboration.graph')}
      data-testid="collaboration-graph-svg"
      onWheel={onWheel}
      onPointerDown={(event) => { panRef.current = { x: event.clientX, y: event.clientY, viewport }; event.currentTarget.setPointerCapture?.(event.pointerId); }}
      onPointerMove={(event) => {
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
      onPointerLeave={() => { panRef.current = null; dragRef.current = null; }}
    >
      <defs><linearGradient id="collaboration-mixed"><stop offset="0%" stopColor="#16803c"/><stop offset="50%" stopColor="#16803c"/><stop offset="50%" stopColor="#c0362c"/><stop offset="100%" stopColor="#c0362c"/></linearGradient></defs>
      {edges.map((edge) => { const a = nodeMap.get(edge.source); const b = nodeMap.get(edge.target); if (!a || !b) return null; const structural = !edge.effect_id && edge.evidence_count === 0; const active = !hasNoiseReduction || context.edges.has(edge.id); const selectedEdge = selectedEffectIds.size > 0 && edgeHasAnyEffect(edge, selectedEffectIds); return <g key={edge.id} opacity={active ? 1 : 0.16}><line x1={a.x} y1={a.y} x2={b.x} y2={b.y} className={`collaboration-edge collaboration-edge--${edge.polarity}`} strokeWidth={selectedEdge ? edge.magnitude + 3 : structural ? 1.5 : edge.magnitude + 1} strokeDasharray={structural || edge.polarity === 'neutral' ? '3 5' : edge.relation_type === 'assign' ? undefined : '10 4'} /><text x={(a.x+b.x)/2} y={(a.y+b.y)/2-6} textAnchor="middle" className="fill-text-muted text-[11px]">{active ? <>{labelFor(t, edge.relation_type)}{structural ? '' : ` · ${labelFor(t, edge.polarity)}`}</> : null}</text></g>; })}
      {[...nodeMap.values()].map((node) => { const active = !hasNoiseReduction || context.nodes.has(node.id); const focused = focusedNodeId === node.id; return <g key={node.id} role="button" tabIndex={0} aria-label={node.label} className="cursor-pointer outline-none" opacity={active ? 1 : 0.18} onPointerDown={(event) => { event.stopPropagation(); const point = toSvgPoint(event); dragRef.current = { id: node.id, pointer: point, origin: { x: node.x, y: node.y }, moved: false }; svgRef.current?.setPointerCapture?.(event.pointerId); }} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); setFocusedNodeId(node.id); setViewport(focusViewBox(node)); } }}><title>{node.label}</title>{node.kind === 'agent' ? <circle cx={node.x} cy={node.y} r="27" className="fill-bg-primary stroke-brand" strokeWidth={focused ? 4 : 2} /> : <rect x={node.x-54} y={node.y-22} width="108" height="44" rx="5" className="fill-bg-primary stroke-text-muted" strokeWidth={focused ? 4 : 2} />}<text x={node.x} y={node.y+4} textAnchor="middle" className="pointer-events-none fill-text-primary text-[11px]">{truncateLabel(node.label)}</text><text x={node.x} y={node.y+18} textAnchor="middle" className="pointer-events-none fill-text-muted text-[8px]">{node.kind}</text></g>; })}
    </svg>
    <div className="grid gap-2 md:grid-cols-2" aria-label={t('insight.collaboration.edgeList')}>{edges.filter((edge) => edge.effect_id || edge.interaction_count > 0).map((edge) => { const scopes = edge.effect_scopes?.length ? edge.effect_scopes : edge.effect_ids?.length ? edge.effect_ids.map((id) => ({ effect_id: id, project_id: '' })) : edge.effect_id ? [{ effect_id: edge.effect_id, project_id: '' }] : []; const key = scopes.map((scope) => `${scope.effect_id}\0${scope.project_id}`).join('\0'); return <button key={edge.id} type="button" disabled={scopes.length === 0} aria-pressed={selectedKey === key} onClick={() => scopes.length > 0 && onSelect(scopes)} className="rounded border border-border px-3 py-2 text-left text-sm hover:bg-bg-subtle focus:ring-2 focus:ring-brand disabled:cursor-default"><strong>{labelFor(t, edge.relation_type)}</strong> · {labelFor(t, edge.polarity)} · {t('insight.collaboration.magnitude', { value: edge.magnitude })} · {t('insight.collaboration.aggregatedEffects', { count: edge.interaction_count })} · evidence {edge.evidence_count}{edge.last_occurred_at ? ` · ${new Date(edge.last_occurred_at).toLocaleString()}` : ''}</button>; })}</div>
  </section>;
}

type GraphViewBox = { x: number; y: number; width: number; height: number };

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

function edgeHasAnyEffect(edge: CollaborationEdge, effectIds: Set<string>): boolean {
  if (edge.effect_id && effectIds.has(edge.effect_id)) return true;
  return (edge.effect_ids ?? edge.effect_scopes?.map((scope) => scope.effect_id) ?? []).some((id) => effectIds.has(id));
}

function truncateLabel(label: string): string {
  return label.length > 18 ? `${label.slice(0, 17)}...` : label;
}

function Timeline({ effects, onSelect, t }: { effects: { effect_id: string; project_id: string; occurred_at: string; relation_type: string; polarity: string; source_agent_ref: string; target_task_id: string }[]; onSelect: (scopes: CollaborationEffectScope[]) => void; t: Translator }) {
  const ordered = [...effects].sort((a, b) => b.occurred_at.localeCompare(a.occurred_at));
  return <section className="rounded-lg border border-border bg-bg-surface p-4" data-testid="collaboration-timeline"><h2 className="font-semibold">{t('insight.collaboration.timeline')}</h2><ol className="mt-3 border-l border-border pl-4">{ordered.map((item) => <li key={item.effect_id} className="mb-3"><button className="text-left text-sm hover:underline focus:ring-2 focus:ring-brand" onClick={() => onSelect([{ effect_id: item.effect_id, project_id: item.project_id }])}><time className="block text-xs text-text-muted">{new Date(item.occurred_at).toLocaleString()}</time>{labelFor(t, item.relation_type)} · {labelFor(t, item.polarity)} — {item.source_agent_ref} → {item.target_task_id}</button></li>)}</ol></section>;
}

function EvidenceDrawer({ effect, effectIds, onClose, t }: { effect: Pick<CollaborationEffect, 'project_id' | 'explanation_key' | 'before_state' | 'after_state'> | null; effectIds: CollaborationEffectScope[]; onClose: () => void; t: Translator }) {
  const query = useCollaborationEvidenceBundle(effectIds, effect?.project_id);
  return <aside role="dialog" aria-modal="true" aria-labelledby="evidence-title" className="fixed inset-y-0 right-0 z-50 w-full max-w-lg overflow-y-auto border-l border-border bg-bg-primary p-5 shadow-xl" data-testid="collaboration-evidence-drawer"><div className="flex items-center justify-between"><h2 id="evidence-title" className="text-lg font-semibold">{t('insight.collaboration.evidence.title')}</h2><button type="button" onClick={onClose} aria-label={t('insight.collaboration.evidence.close')} className="rounded border border-border px-3 py-1">×</button></div>{effect ? <div className="mt-4 text-sm"><p>{t(effect.explanation_key, { defaultValue: effect.explanation_key })}</p><pre className="mt-2 overflow-auto rounded bg-bg-subtle p-2">{JSON.stringify({ before: effect.before_state, after: effect.after_state }, null, 2)}</pre></div> : null}{query.isLoading ? <p className="mt-4">{t('insight.collaboration.evidence.loading')}</p> : null}{query.isError ? <p role="alert" className="mt-4 text-danger">{t('insight.collaboration.evidence.failed')}</p> : null}<ol className="mt-4 space-y-3">{query.data?.evidence.map((event) => <li key={event.event_id} className="rounded border border-border p-3 text-sm"><strong>{event.event_type}</strong><time className="block text-xs text-text-muted">{new Date(event.occurred_at).toLocaleString()}</time><p>{event.actor_ref}</p><pre className="mt-2 overflow-auto text-xs">{JSON.stringify(event.payload, null, 2)}</pre></li>)}</ol></aside>;
}

function CollaborationError({ error, t }: { error: unknown; t: Translator }) { const forbidden = error instanceof ApiError && (error.status === 401 || error.status === 403); return <State id={forbidden ? 'collaboration-forbidden' : 'collaboration-error'} title={forbidden ? t('insight.collaboration.forbidden') : t('insight.collaboration.failed')} body={error instanceof Error ? error.message : undefined} danger />; }
function State({ id, title, body, danger = false }: { id: string; title: string; body?: string; danger?: boolean }) { return <div role={danger ? 'alert' : 'status'} data-testid={id} className={`rounded border p-4 ${danger ? 'border-danger/40 bg-danger/10' : 'border-border bg-bg-surface'}`}><strong>{title}</strong>{body ? <p className="mt-1 text-sm text-text-muted">{body}</p> : null}</div>; }
function labelFor(t: Translator, value: string): string { return t(`insight.collaboration.values.${value}`, { defaultValue: value.replaceAll('_', ' ') }); }

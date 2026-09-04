import React, { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { ApiError } from '@/api/client';
import {
  useCollaborationEffectsPages,
  useCollaborationEvidence,
  type CollaborationEdge,
  type CollaborationEffect,
  type CollaborationFilters,
  type CollaborationGraphResponse,
  type CollaborationNode,
  type CollaborationPolarity,
  type CollaborationRelation,
} from '@/api/insights';
import { identityRefOf, normalizeIdentityRef, refKind, useMembers } from '@/api/members';
import { useProjectMembers, useProjects } from '@/api/projects';
import { useTasksList } from '@/api/tasks';
import { EntitySelect, type EntityOption } from '@/components/EntitySelect';

const RELATIONS: CollaborationRelation[] = ['assign', 'reassign', 'complete', 'block', 'unblock', 'dependency_release', 'review_accept', 'review_reject'];
const POLARITIES: CollaborationPolarity[] = ['positive', 'negative', 'neutral', 'mixed'];
const PAGE_SIZE = 200;
const GRAPH_VIEW_HEIGHT = 520;
const NODE_ROW = 42;
const MAX_VISIBLE_EDGES = 220;
const MAX_ACCESSIBLE_EDGES = 80;
const MAX_TIMELINE_ITEMS = 120;

export default function InsightCollaboration(): React.ReactElement {
  const { t } = useTranslation('insights');
  const [params, setParams] = useSearchParams();
  const [selected, setSelected] = useState<string | null>(null);
  const filters = filtersFromParams(params);
  const query = useCollaborationEffectsPages(filters, Boolean(filters.project_id));
  const data = useMemo(() => mergeCollaborationPages(query.data?.pages ?? []), [query.data?.pages]);
  const effect = data.effects.find((item) => item.effect_id === selected) ?? null;

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
      {!filters.project_id ? <State id="collaboration-scope-required" title={t('insight.collaboration.scopeRequired')} body={t('insight.collaboration.scopeRequiredBody')} /> : null}
      {query.isLoading ? <State id="collaboration-loading" title={t('insight.collaboration.loading')} /> : null}
      {query.isError ? <CollaborationError error={query.error} t={t} /> : null}
      {query.data ? <>
        <Summary summary={data.summary} t={t} />
        {data.graph.edges.length === 0 ? <State id="collaboration-empty" title={t('insight.collaboration.empty')} body={t('insight.collaboration.emptyBody')} /> :
          <CollaborationGraph nodes={data.graph.nodes} edges={data.graph.edges} selected={selected} onSelect={setSelected} t={t} totalEdges={data.graph.edges.length} />}
        {query.hasNextPage ? <div role="status" className="flex flex-wrap items-center justify-between gap-3 rounded border border-warning/40 bg-warning/10 p-3 text-sm" data-testid="collaboration-truncated"><span>{t('insight.collaboration.truncated', { count: data.effects.length })}</span><button type="button" onClick={() => void query.fetchNextPage()} disabled={query.isFetchingNextPage} className="rounded border border-border bg-bg-primary px-3 py-1.5 font-medium text-text-primary hover:bg-bg-subtle disabled:opacity-60">{query.isFetchingNextPage ? t('insight.collaboration.loadingMore') : t('insight.collaboration.loadMore')}</button></div> : null}
        <Timeline effects={data.effects} onSelect={setSelected} t={t} />
      </> : null}
      {selected ? <EvidenceDrawer effect={effect} effectId={selected} projectId={filters.project_id} onClose={() => setSelected(null)} t={t} /> : null}
    </section>
  );
}

function filtersFromParams(params: URLSearchParams): CollaborationFilters {
  const relation = params.get('relation_type');
  const polarity = params.get('polarity');
  return {
    project_id: params.get('project_id') ?? '', task_id: params.get('task_id') ?? undefined,
    agent_ref: params.get('agent_ref') ?? undefined,
    relation_type: RELATIONS.includes(relation as CollaborationRelation) ? relation as CollaborationRelation : undefined,
    polarity: POLARITIES.includes(polarity as CollaborationPolarity) ? polarity as CollaborationPolarity : undefined,
    since: params.get('since') ?? undefined, until: params.get('until') ?? undefined, cursor: params.get('cursor') ?? undefined, limit: PAGE_SIZE,
  };
}

type Translator = ReturnType<typeof useTranslation>['t'];
function CollaborationFiltersBar({ params, update, t }: { params: URLSearchParams; update: (key: string, value: string, clear?: string[]) => void; t: Translator }) {
  const projectId = params.get('project_id') ?? '';
  const projects = useProjects();
  const tasks = useTasksList(projectId || undefined, { status: ['all'], sort: 'updated', dir: 'desc', page_size: 500 });
  const members = useMembers();
  const projectMembers = useProjectMembers(projectId || undefined);
  const projectOptions = useMemo<EntityOption[]>(() => (projects.data ?? [])
    .map((project) => ({ value: project.id, label: project.name || project.id, hint: project.id, badge: project.status }))
    .sort((a, b) => a.label.localeCompare(b.label) || a.value.localeCompare(b.value)), [projects.data]);
  const taskOptions = useMemo<EntityOption[]>(() => (tasks.data?.items ?? [])
    .map((task) => ({ value: task.id, label: task.title || task.org_ref || task.id, hint: task.org_ref ? `${task.org_ref} · ${task.id}` : task.id, badge: task.status }))
    .sort((a, b) => a.label.localeCompare(b.label) || a.value.localeCompare(b.value)), [tasks.data?.items]);
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
  return <form aria-label={t('insight.collaboration.filters.label')} className="grid gap-3 rounded-lg border border-border bg-bg-surface p-4 md:grid-cols-3" onSubmit={(e) => e.preventDefault()}>
    <EntityFilter
      name="project_id"
      label={t('insight.collaboration.filters.project')}
      value={projectId}
      options={projectOptions}
      disabled={projects.isLoading}
      placeholder={projects.isLoading ? t('insight.collaboration.filters.loadingProjects') : t('insight.collaboration.filters.chooseProject')}
      searchPlaceholder={t('insight.collaboration.filters.searchProjects')}
      emptyLabel={t('insight.collaboration.filters.noProjects')}
      update={(key, value) => update(key, value, value !== projectId ? ['task_id', 'agent_ref'] : [])}
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

type PositionedNode = CollaborationNode & { x: number; y: number };
type AggregateEdge = CollaborationEdge & { count: number; effectIds: string[] };

function mergeCollaborationPages(pages: CollaborationGraphResponse[]): CollaborationGraphResponse {
  const nodes = new Map<string, CollaborationNode>();
  const edges = new Map<string, CollaborationEdge>();
  const effects = new Map<string, CollaborationEffect>();
  for (const page of pages) {
    page.graph.nodes.forEach((node) => nodes.set(node.id, node));
    page.graph.edges.forEach((edge) => edges.set(edge.id, edge));
    page.effects.forEach((effect) => effects.set(effect.effect_id, effect));
  }
  const mergedEffects = [...effects.values()];
  const affectedTasks = new Set(mergedEffects.map((effect) => effect.target_task_id).filter(Boolean));
  const summary = {
    positive_count: mergedEffects.filter((effect) => effect.polarity === 'positive').length,
    negative_count: mergedEffects.filter((effect) => effect.polarity === 'negative').length,
    neutral_count: mergedEffects.filter((effect) => effect.polarity === 'neutral').length,
    mixed_count: mergedEffects.filter((effect) => effect.polarity === 'mixed').length,
    affected_task_count: affectedTasks.size,
  };
  return { graph: { nodes: [...nodes.values()], edges: [...edges.values()] }, effects: mergedEffects, summary, next_cursor: pages.at(-1)?.next_cursor ?? null };
}

function buildGraphModel(nodes: CollaborationNode[], edges: CollaborationEdge[]) {
  const agents = nodes.filter((node) => node.kind === 'agent').sort((a, b) => a.label.localeCompare(b.label) || a.id.localeCompare(b.id));
  const tasks = nodes.filter((node) => node.kind === 'task').sort((a, b) => a.label.localeCompare(b.label) || a.id.localeCompare(b.id));
  const positioned = new Map<string, PositionedNode>();
  const place = (node: CollaborationNode, i: number, x: number) => positioned.set(node.id, { ...node, x, y: 56 + i * NODE_ROW });
  agents.forEach((node, i) => place(node, i, 112));
  tasks.forEach((node, i) => place(node, i, 608));
  const aggregates = new Map<string, AggregateEdge>();
  for (const edge of edges) {
    if (!positioned.has(edge.source) || !positioned.has(edge.target)) continue;
    const key = `${edge.source}\n${edge.target}\n${edge.relation_type}\n${edge.polarity}`;
    const current = aggregates.get(key);
    if (current) {
      current.count += 1;
      current.magnitude = Math.max(current.magnitude, edge.magnitude) as 1 | 2 | 3;
      current.effectIds.push(edge.effect_id);
    } else {
      aggregates.set(key, { ...edge, count: 1, effectIds: [edge.effect_id] });
    }
  }
  return { positioned, edges: [...aggregates.values()], height: Math.max(GRAPH_VIEW_HEIGHT, Math.max(agents.length, tasks.length) * NODE_ROW + 96) };
}

function CollaborationGraph({ nodes, edges, selected, onSelect, t, totalEdges }: { nodes: CollaborationNode[]; edges: CollaborationEdge[]; selected: string | null; onSelect: (id: string) => void; t: Translator; totalEdges: number }) {
  const [offset, setOffset] = useState(0);
  const [zoom, setZoom] = useState(1);
  const model = useMemo(() => buildGraphModel(nodes, edges), [nodes, edges]);
  const visibleTop = offset;
  const visibleBottom = offset + GRAPH_VIEW_HEIGHT / zoom;
  const visibleNodes = [...model.positioned.values()].filter((node) => node.y >= visibleTop - NODE_ROW && node.y <= visibleBottom + NODE_ROW);
  const visibleNodeIds = new Set(visibleNodes.map((node) => node.id));
  const visibleEdges = model.edges
    .filter((edge) => visibleNodeIds.has(edge.source) || visibleNodeIds.has(edge.target))
    .sort((a, b) => b.count - a.count || b.magnitude - a.magnitude || a.id.localeCompare(b.id))
    .slice(0, MAX_VISIBLE_EDGES);
  const accessibleEdges = edges.slice(0, MAX_ACCESSIBLE_EDGES);
  const hideLabels = totalEdges > 120 || zoom < 0.85;
  const pan = (delta: number) => setOffset((value) => Math.max(0, Math.min(model.height - GRAPH_VIEW_HEIGHT / zoom, value + delta)));
  const zoomBy = (delta: number) => setZoom((value) => Math.max(0.7, Math.min(1.6, Number((value + delta).toFixed(2)))));
  return <section className="rounded-lg border border-border bg-bg-surface p-4" aria-label={t('insight.collaboration.graph')} data-testid="collaboration-graph">
    <div className="mb-3 flex flex-wrap items-center justify-between gap-3 text-xs text-text-muted"><div className="flex flex-wrap gap-3"><span>━━ {t('insight.collaboration.legend.relationship')}</span><span>┄┄ {t('insight.collaboration.legend.effect')}</span><span>+/− {t('insight.collaboration.legend.mixed')}</span></div><div className="flex items-center gap-1" aria-label={t('insight.collaboration.viewportControls')}><button type="button" className="h-7 w-7 rounded border border-border text-text-primary" onClick={() => pan(-GRAPH_VIEW_HEIGHT / 2)} aria-label={t('insight.collaboration.panUp')}>↑</button><button type="button" className="h-7 w-7 rounded border border-border text-text-primary" onClick={() => pan(GRAPH_VIEW_HEIGHT / 2)} aria-label={t('insight.collaboration.panDown')}>↓</button><button type="button" className="h-7 w-7 rounded border border-border text-text-primary" onClick={() => zoomBy(0.15)} aria-label={t('insight.collaboration.zoomIn')}>+</button><button type="button" className="h-7 w-7 rounded border border-border text-text-primary" onClick={() => zoomBy(-0.15)} aria-label={t('insight.collaboration.zoomOut')}>−</button></div></div>
    {model.edges.length > visibleEdges.length || nodes.length > visibleNodes.length ? <div role="status" className="mb-3 rounded border border-border bg-bg-subtle px-3 py-2 text-xs text-text-muted" data-testid="collaboration-lod-status">{t('insight.collaboration.lodStatus', { visibleEdges: visibleEdges.length, totalEdges, visibleNodes: visibleNodes.length, totalNodes: nodes.length })}</div> : null}
    <svg viewBox={`0 ${offset} 720 ${GRAPH_VIEW_HEIGHT / zoom}`} className="h-[520px] w-full" role="img" aria-label={t('insight.collaboration.graph')}>
      <defs><linearGradient id="collaboration-mixed"><stop offset="0%" stopColor="#16803c"/><stop offset="50%" stopColor="#16803c"/><stop offset="50%" stopColor="#c0362c"/><stop offset="100%" stopColor="#c0362c"/></linearGradient></defs>
      {visibleEdges.map((edge) => { const a = model.positioned.get(edge.source); const b = model.positioned.get(edge.target); if (!a || !b) return null; return <g key={`${edge.id}:${edge.count}`}><line x1={a.x} y1={a.y} x2={b.x} y2={b.y} className={`collaboration-edge collaboration-edge--${edge.polarity}`} strokeWidth={Math.min(8, edge.magnitude + Math.log2(edge.count + 1))} strokeDasharray={edge.polarity === 'neutral' ? '3 5' : edge.relation_type === 'assign' ? undefined : '10 4'} />{!hideLabels ? <text x={(a.x+b.x)/2} y={(a.y+b.y)/2-6} textAnchor="middle" className="fill-text-muted text-[11px]">{labelFor(t, edge.relation_type)} · {labelFor(t, edge.polarity)}{edge.count > 1 ? ` ×${edge.count}` : ''}</text> : null}</g>; })}
      {visibleNodes.map((node) => <g key={node.id}><title>{node.label}</title>{node.kind === 'agent' ? <circle cx={node.x} cy={node.y} r="20" className="fill-bg-primary stroke-brand" strokeWidth="2" /> : <rect x={node.x-48} y={node.y-17} width="96" height="34" rx="5" className="fill-bg-primary stroke-text-muted" strokeWidth="2" />} {!hideLabels ? <text x={node.x} y={node.y+4} textAnchor="middle" className="fill-text-primary text-[11px]">{node.label.slice(0, 18)}</text> : null}</g>)}
    </svg>
    <div className="mt-3 grid gap-2 md:grid-cols-2" aria-label={t('insight.collaboration.edgeList')}>{accessibleEdges.map((edge) => <button key={edge.id} type="button" aria-pressed={selected === edge.effect_id} onClick={() => onSelect(edge.effect_id)} className="rounded border border-border px-3 py-2 text-left text-sm hover:bg-bg-subtle focus:ring-2 focus:ring-brand"><strong>{labelFor(t, edge.relation_type)}</strong> · {labelFor(t, edge.polarity)} · {t('insight.collaboration.magnitude', { value: edge.magnitude })}</button>)}</div>
    {edges.length > accessibleEdges.length ? <p className="mt-2 text-xs text-text-muted">{t('insight.collaboration.edgeListLimited', { count: accessibleEdges.length, total: edges.length })}</p> : null}
  </section>;
}

function Timeline({ effects, onSelect, t }: { effects: { effect_id: string; occurred_at: string; relation_type: string; polarity: string; source_agent_ref: string; target_task_id: string }[]; onSelect: (id: string) => void; t: Translator }) {
  const ordered = [...effects].sort((a, b) => b.occurred_at.localeCompare(a.occurred_at)).slice(0, MAX_TIMELINE_ITEMS);
  return <section className="rounded-lg border border-border bg-bg-surface p-4" data-testid="collaboration-timeline"><h2 className="font-semibold">{t('insight.collaboration.timeline')}</h2><ol className="mt-3 border-l border-border pl-4">{ordered.map((item) => <li key={item.effect_id} className="mb-3"><button className="text-left text-sm hover:underline focus:ring-2 focus:ring-brand" onClick={() => onSelect(item.effect_id)}><time className="block text-xs text-text-muted">{new Date(item.occurred_at).toLocaleString()}</time>{labelFor(t, item.relation_type)} · {labelFor(t, item.polarity)} — {item.source_agent_ref} → {item.target_task_id}</button></li>)}</ol>{effects.length > ordered.length ? <p className="text-xs text-text-muted">{t('insight.collaboration.timelineLimited', { count: ordered.length, total: effects.length })}</p> : null}</section>;
}

function EvidenceDrawer({ effect, effectId, projectId, onClose, t }: { effect: { explanation_key: string; before_state: Record<string, unknown>; after_state: Record<string, unknown> } | null; effectId: string; projectId: string; onClose: () => void; t: Translator }) {
  const query = useCollaborationEvidence(effectId, projectId);
  return <aside role="dialog" aria-modal="true" aria-labelledby="evidence-title" className="fixed inset-y-0 right-0 z-50 w-full max-w-lg overflow-y-auto border-l border-border bg-bg-primary p-5 shadow-xl" data-testid="collaboration-evidence-drawer"><div className="flex items-center justify-between"><h2 id="evidence-title" className="text-lg font-semibold">{t('insight.collaboration.evidence.title')}</h2><button type="button" onClick={onClose} aria-label={t('insight.collaboration.evidence.close')} className="rounded border border-border px-3 py-1">×</button></div>{effect ? <div className="mt-4 text-sm"><p>{t(effect.explanation_key, { defaultValue: effect.explanation_key })}</p><pre className="mt-2 overflow-auto rounded bg-bg-subtle p-2">{JSON.stringify({ before: effect.before_state, after: effect.after_state }, null, 2)}</pre></div> : null}{query.isLoading ? <p className="mt-4">{t('insight.collaboration.evidence.loading')}</p> : null}{query.isError ? <p role="alert" className="mt-4 text-danger">{t('insight.collaboration.evidence.failed')}</p> : null}<ol className="mt-4 space-y-3">{query.data?.evidence.map((event) => <li key={event.event_id} className="rounded border border-border p-3 text-sm"><strong>{event.event_type}</strong><time className="block text-xs text-text-muted">{new Date(event.occurred_at).toLocaleString()}</time><p>{event.actor_ref}</p><pre className="mt-2 overflow-auto text-xs">{JSON.stringify(event.payload, null, 2)}</pre></li>)}</ol></aside>;
}

function CollaborationError({ error, t }: { error: unknown; t: Translator }) { const forbidden = error instanceof ApiError && (error.status === 401 || error.status === 403); return <State id={forbidden ? 'collaboration-forbidden' : 'collaboration-error'} title={forbidden ? t('insight.collaboration.forbidden') : t('insight.collaboration.failed')} body={error instanceof Error ? error.message : undefined} danger />; }
function State({ id, title, body, danger = false }: { id: string; title: string; body?: string; danger?: boolean }) { return <div role={danger ? 'alert' : 'status'} data-testid={id} className={`rounded border p-4 ${danger ? 'border-danger/40 bg-danger/10' : 'border-border bg-bg-surface'}`}><strong>{title}</strong>{body ? <p className="mt-1 text-sm text-text-muted">{body}</p> : null}</div>; }
function labelFor(t: Translator, value: string): string { return t(`insight.collaboration.values.${value}`, { defaultValue: value.replaceAll('_', ' ') }); }

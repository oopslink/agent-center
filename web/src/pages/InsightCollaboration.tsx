import React, { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { ApiError } from '@/api/client';
import {
  useCollaborationEvidences,
  useInfiniteCollaborationEffects,
  type CollaborationEdge,
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
const GRAPH_LANE_X: Record<CollaborationNode['kind'], number> = { agent: 80, project: 220, plan: 340, stage: 480, task: 620 };

export default function InsightCollaboration(): React.ReactElement {
  const { t } = useTranslation('insights');
  const [params, setParams] = useSearchParams();
  const [selected, setSelected] = useState<string | null>(null);
  const filters = filtersFromParams(params);
  const query = useInfiniteCollaborationEffects(filters, Boolean(filters.project_id));
  const effects = useMemo(() => dedupeBy(query.data?.pages.flatMap((page) => page.effects) ?? [], (item) => item.effect_id), [query.data?.pages]);
  const view = useMemo(() => accumulateGraph(query.data?.pages ?? []), [query.data?.pages]);
  const summary = useMemo(() => summarizeEffects(effects), [effects]);
  const selectedEdge = view.edges.find((item) => item.id === selected) ?? null;
  const selectedEffect = effects.find((item) => item.effect_id === selectedEdge?.effect_id) ?? null;

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
        <Summary summary={summary} t={t} />
        {view.edges.length === 0 ? <State id="collaboration-empty" title={t('insight.collaboration.empty')} body={t('insight.collaboration.emptyBody')} /> :
          <CollaborationGraph nodes={view.nodes} edges={view.edges} selected={selected} onSelect={setSelected} t={t} />}
        {query.hasNextPage ? <button type="button" onClick={() => void query.fetchNextPage()} disabled={query.isFetchingNextPage} className="rounded border border-border px-3 py-2 text-sm hover:bg-bg-subtle disabled:opacity-60" data-testid="collaboration-load-more">{query.isFetchingNextPage ? t('insight.collaboration.loading') : t('insight.collaboration.loadMore')}</button> : null}
        <Timeline effects={effects} onSelect={(effectId) => setSelected(view.edges.find((edge) => edge.evidence_effect_ids.includes(effectId))?.id ?? effectId)} t={t} />
      </> : null}
      {selectedEdge ? <EvidenceDrawer edge={selectedEdge} effect={selectedEffect} projectId={filters.project_id} onClose={() => setSelected(null)} t={t} /> : null}
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
      update={(key, value) => update(key, value, value !== projectId ? ['plan_id', 'task_id', 'agent_ref'] : [])}
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
  const nodes = dedupeBy(pages.flatMap((page) => page.graph.nodes), (node) => node.id);
  const edges = new Map<string, CollaborationEdge>();
  for (const edge of pages.flatMap((page) => page.graph.edges)) {
    const key = semanticEdgeKey(edge);
    const existing = edges.get(key);
    if (!existing) {
      const evidenceEventIds = [...new Set(edge.evidence_event_ids)].sort();
      edges.set(key, { ...edge, id: key, evidence_count: evidenceEventIds.length || edge.evidence_count, evidence_effect_ids: [...new Set(edge.evidence_effect_ids)].sort(), evidence_event_ids: evidenceEventIds });
      continue;
    }
    const evidenceEventIds = [...new Set([...existing.evidence_event_ids, ...edge.evidence_event_ids])].sort();
    edges.set(key, {
      ...existing,
      magnitude: maxMagnitude(existing.magnitude, edge.magnitude),
      interaction_count: existing.interaction_count + Math.max(1, edge.interaction_count),
      evidence_count: evidenceEventIds.length || existing.evidence_count + edge.evidence_count,
      first_occurred_at: minTimestamp(existing.first_occurred_at, edge.first_occurred_at),
      last_occurred_at: maxTimestamp(existing.last_occurred_at, edge.last_occurred_at),
      clustered: true,
      evidence_effect_ids: [...new Set([...existing.evidence_effect_ids, ...edge.evidence_effect_ids])].sort(),
      evidence_event_ids: evidenceEventIds,
    });
  }
  return { nodes, edges: [...edges.values()].sort((a, b) => a.source.localeCompare(b.source) || a.target.localeCompare(b.target) || a.relation_type.localeCompare(b.relation_type) || a.polarity.localeCompare(b.polarity)) };
}

function semanticEdgeKey(edge: Pick<CollaborationEdge, 'source' | 'target' | 'relation_type' | 'polarity'>): string {
  return `${edge.source}\u0000${edge.target}\u0000${edge.relation_type}\u0000${edge.polarity}`;
}

function maxMagnitude(a: 1 | 2 | 3, b: 1 | 2 | 3): 1 | 2 | 3 {
  return (Math.max(a, b) as 1 | 2 | 3);
}

function minTimestamp(a?: string, b?: string): string | undefined {
  if (!a) return b;
  if (!b) return a;
  return a <= b ? a : b;
}

function maxTimestamp(a?: string, b?: string): string | undefined {
  if (!a) return b;
  if (!b) return a;
  return a >= b ? a : b;
}

function CollaborationGraph({ nodes, edges, selected, onSelect, t }: { nodes: CollaborationNode[]; edges: CollaborationEdge[]; selected: string | null; onSelect: (id: string) => void; t: Translator }) {
  const nodeMap = useMemo(() => {
    const laneIndex = new Map<string, number>();
    return new Map(nodes.map((node) => { const i = laneIndex.get(node.kind) ?? 0; laneIndex.set(node.kind, i + 1); return [node.id, { ...node, x: GRAPH_LANE_X[node.kind] ?? 610, y: 70 + i * 62 }]; }));
  }, [nodes]);
  return <section className="rounded-lg border border-border bg-bg-surface p-4" aria-label={t('insight.collaboration.graph')} data-testid="collaboration-graph">
    <div className="mb-3 flex flex-wrap gap-3 text-xs text-text-muted"><span>━━ {t('insight.collaboration.legend.relationship')}</span><span>┄┄ {t('insight.collaboration.legend.effect')}</span><span>+/− {t('insight.collaboration.legend.mixed')}</span></div>
    <svg viewBox={`0 0 720 ${Math.max(260, nodes.length * 64 + 30)}`} className="min-h-64 w-full" role="img" aria-label={t('insight.collaboration.graph')}>
      <defs><linearGradient id="collaboration-mixed"><stop offset="0%" stopColor="#16803c"/><stop offset="50%" stopColor="#16803c"/><stop offset="50%" stopColor="#c0362c"/><stop offset="100%" stopColor="#c0362c"/></linearGradient></defs>
      {edges.map((edge) => { const a = nodeMap.get(edge.source); const b = nodeMap.get(edge.target); if (!a || !b) return null; return <g key={edge.id}><line x1={a.x} y1={a.y} x2={b.x} y2={b.y} className={`collaboration-edge collaboration-edge--${edge.polarity}`} strokeWidth={edge.magnitude + 1} strokeDasharray={edge.polarity === 'neutral' ? '3 5' : edge.relation_type === 'assign' ? undefined : '10 4'} /><text x={(a.x+b.x)/2} y={(a.y+b.y)/2-6} textAnchor="middle" className="fill-text-muted text-[11px]">{labelFor(t, edge.relation_type)} · {labelFor(t, edge.polarity)}</text></g>; })}
      {[...nodeMap.values()].map((node) => <g key={node.id}><title>{node.label}</title>{node.kind === 'agent' ? <circle cx={node.x} cy={node.y} r="27" className="fill-bg-primary stroke-brand" strokeWidth="2" /> : <rect x={node.x-54} y={node.y-22} width="108" height="44" rx="5" className="fill-bg-primary stroke-text-muted" strokeWidth="2" />}<text x={node.x} y={node.y+4} textAnchor="middle" className="fill-text-primary text-[11px]">{node.label.slice(0, 18)}</text></g>)}
    </svg>
    <div className="grid gap-2 md:grid-cols-2" aria-label={t('insight.collaboration.edgeList')}>{edges.map((edge) => <button key={edge.id} type="button" aria-pressed={selected === edge.id} onClick={() => onSelect(edge.id)} className="rounded border border-border px-3 py-2 text-left text-sm hover:bg-bg-subtle focus:ring-2 focus:ring-brand"><strong>{labelFor(t, edge.relation_type)}</strong> · {labelFor(t, edge.polarity)} · {t('insight.collaboration.magnitude', { value: edge.magnitude })} · {t('insight.collaboration.aggregatedEffects', { count: edge.interaction_count })} · {t('insight.collaboration.evidence.count', { count: edge.evidence_count })}{edge.first_occurred_at && edge.last_occurred_at ? ` · ${new Date(edge.first_occurred_at).toLocaleString()} - ${new Date(edge.last_occurred_at).toLocaleString()}` : ''}</button>)}</div>
  </section>;
}

function Timeline({ effects, onSelect, t }: { effects: { effect_id: string; occurred_at: string; relation_type: string; polarity: string; source_agent_ref: string; target_task_id: string }[]; onSelect: (id: string) => void; t: Translator }) {
  const ordered = [...effects].sort((a, b) => b.occurred_at.localeCompare(a.occurred_at));
  return <section className="rounded-lg border border-border bg-bg-surface p-4" data-testid="collaboration-timeline"><h2 className="font-semibold">{t('insight.collaboration.timeline')}</h2><ol className="mt-3 border-l border-border pl-4">{ordered.map((item) => <li key={item.effect_id} className="mb-3"><button className="text-left text-sm hover:underline focus:ring-2 focus:ring-brand" onClick={() => onSelect(item.effect_id)}><time className="block text-xs text-text-muted">{new Date(item.occurred_at).toLocaleString()}</time>{labelFor(t, item.relation_type)} · {labelFor(t, item.polarity)} — {item.source_agent_ref} → {item.target_task_id}</button></li>)}</ol></section>;
}

function EvidenceDrawer({ edge, effect, projectId, onClose, t }: { edge: CollaborationEdge; effect: { explanation_key: string; before_state: Record<string, unknown>; after_state: Record<string, unknown> } | null; projectId: string; onClose: () => void; t: Translator }) {
  const query = useCollaborationEvidences(edge.evidence_effect_ids, projectId);
  return <aside role="dialog" aria-modal="true" aria-labelledby="evidence-title" className="fixed inset-y-0 right-0 z-50 w-full max-w-lg overflow-y-auto border-l border-border bg-bg-primary p-5 shadow-xl" data-testid="collaboration-evidence-drawer"><div className="flex items-center justify-between"><h2 id="evidence-title" className="text-lg font-semibold">{t('insight.collaboration.evidence.title')}</h2><button type="button" onClick={onClose} aria-label={t('insight.collaboration.evidence.close')} className="rounded border border-border px-3 py-1">×</button></div>{effect ? <div className="mt-4 text-sm"><p>{t(effect.explanation_key, { defaultValue: effect.explanation_key })}</p><pre className="mt-2 overflow-auto rounded bg-bg-subtle p-2">{JSON.stringify({ before: effect.before_state, after: effect.after_state }, null, 2)}</pre></div> : null}{query.isLoading ? <p className="mt-4">{t('insight.collaboration.evidence.loading')}</p> : null}{query.isError ? <p role="alert" className="mt-4 text-danger">{t('insight.collaboration.evidence.failed')}</p> : null}<ol className="mt-4 space-y-3">{query.evidence.map((event) => <li key={event.event_id} className="rounded border border-border p-3 text-sm"><strong>{event.event_type}</strong><time className="block text-xs text-text-muted">{new Date(event.occurred_at).toLocaleString()}</time><p>{event.actor_ref}</p><pre className="mt-2 overflow-auto text-xs">{JSON.stringify(event.payload, null, 2)}</pre></li>)}</ol></aside>;
}

function CollaborationError({ error, t }: { error: unknown; t: Translator }) { const forbidden = error instanceof ApiError && (error.status === 401 || error.status === 403); return <State id={forbidden ? 'collaboration-forbidden' : 'collaboration-error'} title={forbidden ? t('insight.collaboration.forbidden') : t('insight.collaboration.failed')} body={error instanceof Error ? error.message : undefined} danger />; }
function State({ id, title, body, danger = false }: { id: string; title: string; body?: string; danger?: boolean }) { return <div role={danger ? 'alert' : 'status'} data-testid={id} className={`rounded border p-4 ${danger ? 'border-danger/40 bg-danger/10' : 'border-border bg-bg-surface'}`}><strong>{title}</strong>{body ? <p className="mt-1 text-sm text-text-muted">{body}</p> : null}</div>; }
function labelFor(t: Translator, value: string): string { return t(`insight.collaboration.values.${value}`, { defaultValue: value.replaceAll('_', ' ') }); }

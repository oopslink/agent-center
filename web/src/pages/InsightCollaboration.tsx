import React, { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { ApiError } from '@/api/client';
import {
  useInfiniteCollaborationEffects,
  useCollaborationEvidence,
  type CollaborationEdge,
  type CollaborationFilters,
  type CollaborationPolarity,
  type CollaborationRelation,
} from '@/api/insights';
import { identityRefOf, normalizeIdentityRef, refKind, useMembers } from '@/api/members';
import { useProjectMembers, useProjects } from '@/api/projects';
import { useTasksList } from '@/api/tasks';
import { usePlans, usePlanStages, type Plan, type PlanStage } from '@/api/plans';
import { EntitySelect, type EntityOption } from '@/components/EntitySelect';

const RELATIONS: CollaborationRelation[] = ['assign', 'reassign', 'complete', 'block', 'unblock', 'dependency_release', 'review_accept', 'review_reject'];
const POLARITIES: CollaborationPolarity[] = ['positive', 'negative', 'neutral', 'mixed'];

export default function InsightCollaboration(): React.ReactElement {
  const { t } = useTranslation('insights');
  const [params, setParams] = useSearchParams();
  const [selected, setSelected] = useState<string | null>(null);
  const filters = filtersFromParams(params);
  const planId = params.get('plan_id') ?? '';
  const plans = usePlans(filters.project_id || undefined);
  const stages = usePlanStages(filters.project_id || undefined, planId || undefined);
  const query = useInfiniteCollaborationEffects(filters, Boolean(filters.project_id));
  const effects = useMemo(() => dedupeBy(query.data?.pages.flatMap((page) => page.effects) ?? [], (item) => item.effect_id), [query.data?.pages]);
  const effect = effects.find((item) => item.effect_id === selected) ?? null;
  const view = useMemo(() => buildOrganizationGraph(effects, plans.data ?? [], stages.data ?? [], planId), [effects, plans.data, stages.data, planId]);
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
      {!filters.project_id ? <State id="collaboration-scope-required" title={t('insight.collaboration.scopeRequired')} body={t('insight.collaboration.scopeRequiredBody')} /> : null}
      {query.isLoading ? <State id="collaboration-loading" title={t('insight.collaboration.loading')} /> : null}
      {query.isError ? <CollaborationError error={query.error} t={t} /> : null}
      {query.data ? <>
        <Summary summary={summary} t={t} />
        {view.edges.length === 0 ? <State id="collaboration-empty" title={t('insight.collaboration.empty')} body={t('insight.collaboration.emptyBody')} /> :
          <CollaborationGraph nodes={view.nodes} edges={view.edges} selected={selected} onSelect={setSelected} t={t} />}
        {query.hasNextPage ? <button type="button" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()} className="rounded border border-border px-3 py-2 text-sm" data-testid="collaboration-load-more">{query.isFetchingNextPage ? t('insight.collaboration.loadingMore') : t('insight.collaboration.loadMore')}</button> : null}
        <Timeline effects={effects} onSelect={setSelected} t={t} />
      </> : null}
      {selected ? <EvidenceDrawer effect={effect} effectId={selected} onClose={() => setSelected(null)} t={t} /> : null}
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

interface GraphNode { id: string; kind: 'agent' | 'plan' | 'stage' | 'task'; label: string }
interface GraphEdge { id: string; source: string; target: string; relation_type: string; polarity: CollaborationPolarity; magnitude: number; effect_id?: string; effect_count?: number; structural?: boolean }

function dedupeBy<T>(items: T[], key: (item: T) => string): T[] {
  return [...new Map(items.map((item) => [key(item), item])).values()];
}

function summarizeEffects(effects: Array<{ polarity: CollaborationPolarity; target_task_id: string }>) {
  const count = (polarity: CollaborationPolarity) => effects.filter((effect) => effect.polarity === polarity).length;
  return { positive_count: count('positive'), negative_count: count('negative'), neutral_count: count('neutral'), mixed_count: count('mixed'), affected_task_count: new Set(effects.map((effect) => effect.target_task_id)).size };
}

function buildOrganizationGraph(effects: Array<CollaborationEdge & { target_task_id: string }>, plans: Plan[], stages: PlanStage[], selectedPlanId: string): { nodes: GraphNode[]; edges: GraphEdge[] } {
  const planByTask = new Map<string, Plan>();
  plans.forEach((plan) => (plan.nodes_preview ?? []).forEach((node) => planByTask.set(node.task_id, plan)));
  const visibleEffects = selectedPlanId ? effects.filter((effect) => planByTask.get(effect.target_task_id)?.id === selectedPlanId) : effects;
  const nodes: GraphNode[] = [];
  const edges: GraphEdge[] = [];
  const seenNodes = new Set<string>();
  const addNode = (node: GraphNode) => { if (!seenNodes.has(node.id)) { seenNodes.add(node.id); nodes.push(node); } };
  const stageByTask = new Map(stages.flatMap((stage) => stage.members.map((member) => [member.task_id, stage] as const)));
  const aggregateEdges = new Map<string, GraphEdge>();
  visibleEffects.forEach((effect) => {
    const agentId = effect.source;
    const taskId = effect.target_task_id;
    const taskNodeId = `task:${taskId}`;
    const plan = planByTask.get(taskId);
    const stage = selectedPlanId ? stageByTask.get(taskId) : undefined;
    addNode({ id: agentId, kind: 'agent', label: agentId.replace(/^agent:/, '') });
    addNode({ id: taskNodeId, kind: 'task', label: plan?.nodes_preview?.find((node) => node.task_id === taskId)?.title || taskId });
    if (plan) {
      const planNodeId = `plan:${plan.id}`;
      addNode({ id: planNodeId, kind: 'plan', label: plan.name || plan.org_ref || plan.id });
      edges.push({ id: `${planNodeId}:${taskNodeId}`, source: planNodeId, target: stage ? `stage:${stage.id}` : taskNodeId, relation_type: stage ? 'contains_stage' : 'contains_task', polarity: 'neutral', magnitude: 1, structural: true });
      if (stage) {
        const stageNodeId = `stage:${stage.id}`;
        addNode({ id: stageNodeId, kind: 'stage', label: stage.name || stage.id });
        edges.push({ id: `${stageNodeId}:${taskNodeId}`, source: stageNodeId, target: taskNodeId, relation_type: 'contains_task', polarity: 'neutral', magnitude: 1, structural: true });
      }
      const aggregateId = `${agentId}:${planNodeId}:${effect.relation_type}:${effect.polarity}`;
      const existing = aggregateEdges.get(aggregateId);
      aggregateEdges.set(aggregateId, existing
        ? { ...existing, magnitude: Math.min(3, existing.magnitude + effect.magnitude), effect_count: (existing.effect_count ?? 1) + 1 }
        : { ...effect, id: aggregateId, target: planNodeId, effect_count: 1 });
    } else {
      aggregateEdges.set(effect.id, { ...effect, target: taskNodeId, effect_count: 1 });
    }
  });
  return { nodes, edges: dedupeBy([...aggregateEdges.values(), ...edges], (edge) => edge.id) };
}

function CollaborationGraph({ nodes, edges, selected, onSelect, t }: { nodes: GraphNode[]; edges: GraphEdge[]; selected: string | null; onSelect: (id: string) => void; t: Translator }) {
  const lanes = { agent: 80, plan: 290, stage: 480, task: 660 };
  const laneIndex = new Map<string, number>();
  const nodeMap = useMemo(() => new Map(nodes.map((node) => { const i = laneIndex.get(node.kind) ?? 0; laneIndex.set(node.kind, i + 1); return [node.id, { ...node, x: lanes[node.kind], y: 70 + i * 68 }]; })), [nodes]);
  return <section className="rounded-lg border border-border bg-bg-surface p-4" aria-label={t('insight.collaboration.graph')} data-testid="collaboration-graph">
    <div className="mb-3 flex flex-wrap gap-3 text-xs text-text-muted"><span>━━ {t('insight.collaboration.legend.relationship')}</span><span>┄┄ {t('insight.collaboration.legend.effect')}</span><span>+/− {t('insight.collaboration.legend.mixed')}</span></div>
    <svg viewBox={`0 0 720 ${Math.max(260, nodes.length * 64 + 30)}`} className="min-h-64 w-full" role="img" aria-label={t('insight.collaboration.graph')}>
      <defs><linearGradient id="collaboration-mixed"><stop offset="0%" stopColor="#16803c"/><stop offset="50%" stopColor="#16803c"/><stop offset="50%" stopColor="#c0362c"/><stop offset="100%" stopColor="#c0362c"/></linearGradient></defs>
      {edges.map((edge) => { const a = nodeMap.get(edge.source); const b = nodeMap.get(edge.target); if (!a || !b) return null; return <g key={edge.id}><line x1={a.x} y1={a.y} x2={b.x} y2={b.y} className={`collaboration-edge collaboration-edge--${edge.polarity}`} strokeWidth={edge.structural ? 1.5 : edge.magnitude + 1} strokeDasharray={edge.structural || edge.polarity === 'neutral' ? '3 5' : edge.relation_type === 'assign' ? undefined : '10 4'} /><text x={(a.x+b.x)/2} y={(a.y+b.y)/2-6} textAnchor="middle" className="fill-text-muted text-[11px]">{labelFor(t, edge.relation_type)}{edge.structural ? '' : ` · ${labelFor(t, edge.polarity)}`}</text></g>; })}
      {[...nodeMap.values()].map((node) => <g key={node.id}><title>{node.label}</title>{node.kind === 'agent' ? <circle cx={node.x} cy={node.y} r="27" className="fill-bg-primary stroke-brand" strokeWidth="2" /> : <rect x={node.x-54} y={node.y-22} width="108" height="44" rx="5" className="fill-bg-primary stroke-text-muted" strokeWidth="2" />}<text x={node.x} y={node.y+4} textAnchor="middle" className="fill-text-primary text-[11px]">{node.label.slice(0, 18)}</text><text x={node.x} y={node.y+18} textAnchor="middle" className="fill-text-muted text-[8px]">{node.kind}</text></g>)}
    </svg>
    <div className="grid gap-2 md:grid-cols-2" aria-label={t('insight.collaboration.edgeList')}>{edges.filter((edge) => edge.effect_id).map((edge) => <button key={edge.id} type="button" aria-pressed={selected === edge.effect_id} onClick={() => edge.effect_id && onSelect(edge.effect_id)} className="rounded border border-border px-3 py-2 text-left text-sm hover:bg-bg-subtle focus:ring-2 focus:ring-brand"><strong>{labelFor(t, edge.relation_type)}</strong> · {labelFor(t, edge.polarity)} · {t('insight.collaboration.magnitude', { value: edge.magnitude })}{(edge.effect_count ?? 1) > 1 ? ` · ${t('insight.collaboration.aggregatedEffects', { count: edge.effect_count })}` : ''}</button>)}</div>
  </section>;
}

function Timeline({ effects, onSelect, t }: { effects: { effect_id: string; occurred_at: string; relation_type: string; polarity: string; source_agent_ref: string; target_task_id: string }[]; onSelect: (id: string) => void; t: Translator }) {
  const ordered = [...effects].sort((a, b) => b.occurred_at.localeCompare(a.occurred_at));
  return <section className="rounded-lg border border-border bg-bg-surface p-4" data-testid="collaboration-timeline"><h2 className="font-semibold">{t('insight.collaboration.timeline')}</h2><ol className="mt-3 border-l border-border pl-4">{ordered.map((item) => <li key={item.effect_id} className="mb-3"><button className="text-left text-sm hover:underline focus:ring-2 focus:ring-brand" onClick={() => onSelect(item.effect_id)}><time className="block text-xs text-text-muted">{new Date(item.occurred_at).toLocaleString()}</time>{labelFor(t, item.relation_type)} · {labelFor(t, item.polarity)} — {item.source_agent_ref} → {item.target_task_id}</button></li>)}</ol></section>;
}

function EvidenceDrawer({ effect, effectId, onClose, t }: { effect: { explanation_key: string; before_state: Record<string, unknown>; after_state: Record<string, unknown> } | null; effectId: string; onClose: () => void; t: Translator }) {
  const query = useCollaborationEvidence(effectId);
  return <aside role="dialog" aria-modal="true" aria-labelledby="evidence-title" className="fixed inset-y-0 right-0 z-50 w-full max-w-lg overflow-y-auto border-l border-border bg-bg-primary p-5 shadow-xl" data-testid="collaboration-evidence-drawer"><div className="flex items-center justify-between"><h2 id="evidence-title" className="text-lg font-semibold">{t('insight.collaboration.evidence.title')}</h2><button type="button" onClick={onClose} aria-label={t('insight.collaboration.evidence.close')} className="rounded border border-border px-3 py-1">×</button></div>{effect ? <div className="mt-4 text-sm"><p>{t(effect.explanation_key, { defaultValue: effect.explanation_key })}</p><pre className="mt-2 overflow-auto rounded bg-bg-subtle p-2">{JSON.stringify({ before: effect.before_state, after: effect.after_state }, null, 2)}</pre></div> : null}{query.isLoading ? <p className="mt-4">{t('insight.collaboration.evidence.loading')}</p> : null}{query.isError ? <p role="alert" className="mt-4 text-danger">{t('insight.collaboration.evidence.failed')}</p> : null}<ol className="mt-4 space-y-3">{query.data?.evidence.map((event) => <li key={event.event_id} className="rounded border border-border p-3 text-sm"><strong>{event.event_type}</strong><time className="block text-xs text-text-muted">{new Date(event.occurred_at).toLocaleString()}</time><p>{event.actor_ref}</p><pre className="mt-2 overflow-auto text-xs">{JSON.stringify(event.payload, null, 2)}</pre></li>)}</ol></aside>;
}

function CollaborationError({ error, t }: { error: unknown; t: Translator }) { const forbidden = error instanceof ApiError && (error.status === 401 || error.status === 403); return <State id={forbidden ? 'collaboration-forbidden' : 'collaboration-error'} title={forbidden ? t('insight.collaboration.forbidden') : t('insight.collaboration.failed')} body={error instanceof Error ? error.message : undefined} danger />; }
function State({ id, title, body, danger = false }: { id: string; title: string; body?: string; danger?: boolean }) { return <div role={danger ? 'alert' : 'status'} data-testid={id} className={`rounded border p-4 ${danger ? 'border-danger/40 bg-danger/10' : 'border-border bg-bg-surface'}`}><strong>{title}</strong>{body ? <p className="mt-1 text-sm text-text-muted">{body}</p> : null}</div>; }
function labelFor(t: Translator, value: string): string { return t(`insight.collaboration.values.${value}`, { defaultValue: value.replaceAll('_', ' ') }); }

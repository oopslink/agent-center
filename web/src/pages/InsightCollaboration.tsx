import React, { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { ApiError } from '@/api/client';
import {
  useCollaborationEffects,
  useCollaborationEvidence,
  type CollaborationEdge,
  type CollaborationFilters,
  type CollaborationPolarity,
  type CollaborationRelation,
} from '@/api/insights';

const RELATIONS: CollaborationRelation[] = ['assign', 'reassign', 'complete', 'block', 'unblock', 'dependency_release', 'review_accept', 'review_reject'];
const POLARITIES: CollaborationPolarity[] = ['positive', 'negative', 'neutral', 'mixed'];

export default function InsightCollaboration(): React.ReactElement {
  const { t } = useTranslation('insights');
  const [params, setParams] = useSearchParams();
  const [selected, setSelected] = useState<string | null>(null);
  const filters = filtersFromParams(params);
  const query = useCollaborationEffects(filters, Boolean(filters.project_id && filters.task_id));
  const effect = query.data?.effects.find((item) => item.effect_id === selected) ?? null;

  const update = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value); else next.delete(key);
    next.delete('cursor');
    setParams(next);
    setSelected(null);
  };

  return (
    <section className="space-y-4" data-testid="page-InsightCollaboration">
      <header><h1 className="text-xl font-semibold text-text-primary">{t('insight.collaboration.title')}</h1><p className="mt-1 text-sm text-text-muted">{t('insight.collaboration.subtitle')}</p></header>
      <CollaborationFiltersBar params={params} update={update} t={t} />
      {!filters.project_id || !filters.task_id ? <State id="collaboration-scope-required" title={t('insight.collaboration.scopeRequired')} body={t('insight.collaboration.scopeRequiredBody')} /> : null}
      {query.isLoading ? <State id="collaboration-loading" title={t('insight.collaboration.loading')} /> : null}
      {query.isError ? <CollaborationError error={query.error} t={t} /> : null}
      {query.data ? <>
        <Summary summary={query.data.summary} t={t} />
        {query.data.graph.edges.length === 0 ? <State id="collaboration-empty" title={t('insight.collaboration.empty')} body={t('insight.collaboration.emptyBody')} /> :
          <CollaborationGraph nodes={query.data.graph.nodes} edges={query.data.graph.edges} selected={selected} onSelect={setSelected} t={t} />}
        {query.data.next_cursor ? <div role="status" className="rounded border border-warning/40 bg-warning/10 p-3 text-sm" data-testid="collaboration-truncated">{t('insight.collaboration.truncated', { count: 100 })}</div> : null}
        <Timeline effects={query.data.effects} onSelect={setSelected} t={t} />
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
function CollaborationFiltersBar({ params, update, t }: { params: URLSearchParams; update: (key: string, value: string) => void; t: Translator }) {
  const fields = [
    ['project_id', t('insight.collaboration.filters.project'), 'text'], ['task_id', t('insight.collaboration.filters.task'), 'text'],
    ['agent_ref', t('insight.collaboration.filters.agent'), 'text'], ['since', t('insight.collaboration.filters.since'), 'datetime-local'],
    ['until', t('insight.collaboration.filters.until'), 'datetime-local'],
  ];
  return <form aria-label={t('insight.collaboration.filters.label')} className="grid gap-3 rounded-lg border border-border bg-bg-surface p-4 md:grid-cols-3" onSubmit={(e) => e.preventDefault()}>
    {fields.map(([name, label, type]) => <label key={name} className="text-xs text-text-muted">{label}<input aria-label={label} type={type} value={params.get(name) ?? ''} onChange={(e) => update(name, e.target.value)} className="mt-1 w-full rounded border border-border bg-bg-primary px-2 py-1.5 text-sm text-text-primary" /></label>)}
    <SelectFilter name="relation_type" label={t('insight.collaboration.filters.relation')} values={RELATIONS} value={params.get('relation_type') ?? ''} update={update} t={t} />
    <SelectFilter name="polarity" label={t('insight.collaboration.filters.polarity')} values={POLARITIES} value={params.get('polarity') ?? ''} update={update} t={t} />
  </form>;
}

function SelectFilter({ name, label, values, value, update, t }: { name: string; label: string; values: string[]; value: string; update: (k: string, v: string) => void; t: Translator }) {
  return <label className="text-xs text-text-muted">{label}<select aria-label={label} value={value} onChange={(e) => update(name, e.target.value)} className="mt-1 w-full rounded border border-border bg-bg-primary px-2 py-1.5 text-sm text-text-primary"><option value="">{t('insight.collaboration.filters.all')}</option>{values.map((item) => <option key={item} value={item}>{labelFor(t, item)}</option>)}</select></label>;
}

function Summary({ summary, t }: { summary: { positive_count: number; negative_count: number; neutral_count: number; mixed_count: number; affected_task_count: number }; t: Translator }) {
  return <section aria-label={t('insight.collaboration.summary')} className="grid grid-cols-2 gap-2 md:grid-cols-5">{(['positive', 'negative', 'neutral', 'mixed'] as const).map((key) => <div key={key} className="rounded border border-border bg-bg-surface p-3"><span className="text-xs text-text-muted">{labelFor(t, key)}</span><strong className="block text-lg">{summary[`${key}_count`]}</strong></div>)}<div className="rounded border border-border bg-bg-surface p-3"><span className="text-xs text-text-muted">{t('insight.collaboration.affectedTasks')}</span><strong className="block text-lg">{summary.affected_task_count}</strong></div></section>;
}

function CollaborationGraph({ nodes, edges, selected, onSelect, t }: { nodes: { id: string; kind: string; label: string }[]; edges: CollaborationEdge[]; selected: string | null; onSelect: (id: string) => void; t: Translator }) {
  const nodeMap = useMemo(() => new Map(nodes.map((node, i) => [node.id, { ...node, x: node.kind === 'agent' ? 110 : 610, y: 70 + i * 62 }])), [nodes]);
  return <section className="rounded-lg border border-border bg-bg-surface p-4" aria-label={t('insight.collaboration.graph')} data-testid="collaboration-graph">
    <div className="mb-3 flex flex-wrap gap-3 text-xs text-text-muted"><span>━━ {t('insight.collaboration.legend.relationship')}</span><span>┄┄ {t('insight.collaboration.legend.effect')}</span><span>+/− {t('insight.collaboration.legend.mixed')}</span></div>
    <svg viewBox={`0 0 720 ${Math.max(260, nodes.length * 64 + 30)}`} className="min-h-64 w-full" role="img" aria-label={t('insight.collaboration.graph')}>
      <defs><linearGradient id="collaboration-mixed"><stop offset="0%" stopColor="#16803c"/><stop offset="50%" stopColor="#16803c"/><stop offset="50%" stopColor="#c0362c"/><stop offset="100%" stopColor="#c0362c"/></linearGradient></defs>
      {edges.map((edge) => { const a = nodeMap.get(edge.source); const b = nodeMap.get(edge.target); if (!a || !b) return null; return <g key={edge.id}><line x1={a.x} y1={a.y} x2={b.x} y2={b.y} className={`collaboration-edge collaboration-edge--${edge.polarity}`} strokeWidth={edge.magnitude + 1} strokeDasharray={edge.polarity === 'neutral' ? '3 5' : edge.relation_type === 'assign' ? undefined : '10 4'} /><text x={(a.x+b.x)/2} y={(a.y+b.y)/2-6} textAnchor="middle" className="fill-text-muted text-[11px]">{labelFor(t, edge.relation_type)} · {labelFor(t, edge.polarity)}</text></g>; })}
      {[...nodeMap.values()].map((node) => <g key={node.id}><title>{node.label}</title>{node.kind === 'agent' ? <circle cx={node.x} cy={node.y} r="27" className="fill-bg-primary stroke-brand" strokeWidth="2" /> : <rect x={node.x-54} y={node.y-22} width="108" height="44" rx="5" className="fill-bg-primary stroke-text-muted" strokeWidth="2" />}<text x={node.x} y={node.y+4} textAnchor="middle" className="fill-text-primary text-[11px]">{node.label.slice(0, 18)}</text></g>)}
    </svg>
    <div className="grid gap-2 md:grid-cols-2" aria-label={t('insight.collaboration.edgeList')}>{edges.map((edge) => <button key={edge.id} type="button" aria-pressed={selected === edge.effect_id} onClick={() => onSelect(edge.effect_id)} className="rounded border border-border px-3 py-2 text-left text-sm hover:bg-bg-subtle focus:ring-2 focus:ring-brand"><strong>{labelFor(t, edge.relation_type)}</strong> · {labelFor(t, edge.polarity)} · {t('insight.collaboration.magnitude', { value: edge.magnitude })}</button>)}</div>
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

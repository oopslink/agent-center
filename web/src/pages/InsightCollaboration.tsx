import React, { useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { ApiError } from '@/api/client';
import {
  useCollaborationEffects,
  useCollaborationEvidence,
  type CollaborationEdge,
  type CollaborationEffect,
  type CollaborationFilters,
  type CollaborationNode,
  type CollaborationPolarity,
  type CollaborationRelation,
} from '@/api/insights';

const RELATIONS: CollaborationRelation[] = ['assign', 'reassign', 'complete', 'block', 'unblock', 'dependency_release', 'review_accept', 'review_reject'];
const POLARITIES: CollaborationPolarity[] = ['positive', 'negative', 'neutral', 'mixed'];
const GRAPH_LIMIT = 500;
const INITIAL_LOD_LIMIT = 100;

type Translator = ReturnType<typeof useTranslation>['t'];
type GraphNodeKind = CollaborationNode['kind'];
type LayoutNode = CollaborationNode & { x: number; y: number; cluster: string };
type Point = { x: number; y: number };
type SelectionBox = { x: number; y: number; w: number; h: number };

export default function InsightCollaboration(): React.ReactElement {
  const { t } = useTranslation('insights');
  const [params, setParams] = useSearchParams();
  const [selectedEffectId, setSelectedEffectId] = useState<string | null>(null);
  const [focusedNode, setFocusedNode] = useState<string | null>(params.get('agent_ref') || (params.get('task_id') ? `task:${params.get('task_id')}` : null));
  const [search, setSearch] = useState('');
  const [loadedCursor, setLoadedCursor] = useState(params.get('cursor') ?? '');
  const filters = filtersFromParams(params, loadedCursor);
  const query = useCollaborationEffects(filters);
  const selectedEffect = query.data?.effects.find((item) => item.effect_id === selectedEffectId) ?? null;

  const update = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    next.delete('cursor');
    setLoadedCursor('');
    setParams(next);
    setSelectedEffectId(null);
  };

  const clear = () => {
    setParams(new URLSearchParams());
    setLoadedCursor('');
    setFocusedNode(null);
    setSearch('');
    setSelectedEffectId(null);
  };

  const locate = () => {
    const needle = search.trim().toLowerCase();
    if (!needle) return;
    const hit = query.data?.graph.nodes.find((node) => node.id.toLowerCase().includes(needle) || node.label.toLowerCase().includes(needle));
    if (hit) setFocusedNode(hit.id);
  };

  return (
    <section className="space-y-4" data-testid="page-InsightCollaboration">
      <header className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary">{t('insight.collaboration.title')}</h1>
          <p className="mt-1 text-sm text-text-muted">{t('insight.collaboration.subtitle')}</p>
        </div>
        <SearchControls search={search} setSearch={setSearch} locate={locate} clear={clear} t={t} />
      </header>

      <FilterBar params={params} update={update} clear={clear} t={t} />

      {query.isLoading ? <State id="collaboration-loading" title={t('insight.collaboration.loading')} /> : null}
      {query.isError ? <CollaborationError error={query.error} t={t} /> : null}
      {query.data ? (
        <>
          <OrganizationGraph
            nodes={query.data.graph.nodes}
            edges={query.data.graph.edges}
            effects={query.data.effects}
            selectedEffectId={selectedEffectId}
            focusedNode={focusedNode}
            search={search}
            truncated={Boolean(query.data.truncated || query.data.next_cursor)}
            onSelectEffect={setSelectedEffectId}
            onFocusNode={setFocusedNode}
            t={t}
          />
          {query.data.graph.edges.length === 0 ? <State id="collaboration-empty" title={t('insight.collaboration.empty')} body={t('insight.collaboration.emptyBody')} /> : null}
          {query.data.truncated || query.data.next_cursor ? (
            <div role="status" className="flex flex-wrap items-center justify-between gap-2 rounded border border-warning/40 bg-warning/10 p-3 text-sm" data-testid="collaboration-truncated">
              <span>{t('insight.collaboration.truncated', { count: GRAPH_LIMIT })}</span>
              {query.data.next_cursor ? <button type="button" className="rounded border border-border bg-bg-primary px-3 py-1.5 text-sm" onClick={() => setLoadedCursor(query.data?.next_cursor ?? '')}>{t('insight.collaboration.loadMore')}</button> : null}
            </div>
          ) : null}
          <SecondaryInsightPanels summary={query.data.summary} effects={query.data.effects} onSelect={setSelectedEffectId} t={t} />
        </>
      ) : null}

      {selectedEffectId ? <EvidenceDrawer effect={selectedEffect} effectId={selectedEffectId} onClose={() => setSelectedEffectId(null)} t={t} /> : null}
    </section>
  );
}

function filtersFromParams(params: URLSearchParams, cursor: string): CollaborationFilters {
  const relation = params.get('relation_type');
  const polarity = params.get('polarity');
  return {
    project_id: params.get('project_id') ?? undefined,
    task_id: params.get('task_id') ?? undefined,
    agent_ref: params.get('agent_ref') ?? undefined,
    relation_type: RELATIONS.includes(relation as CollaborationRelation) ? relation as CollaborationRelation : undefined,
    polarity: POLARITIES.includes(polarity as CollaborationPolarity) ? polarity as CollaborationPolarity : undefined,
    since: normalizeDateParam(params.get('since')),
    until: normalizeDateParam(params.get('until')),
    cursor: cursor || undefined,
    limit: GRAPH_LIMIT,
  };
}

function normalizeDateParam(value: string | null): string | undefined {
  if (!value) return undefined;
  return value.includes('T') && !value.endsWith('Z') ? `${value}:00Z` : value;
}

function SearchControls({ search, setSearch, locate, clear, t }: { search: string; setSearch: (value: string) => void; locate: () => void; clear: () => void; t: Translator }) {
  return (
    <div className="flex flex-wrap gap-2">
      <label className="min-w-60 text-xs text-text-muted">
        {t('insight.collaboration.search')}
        <input
          aria-label={t('insight.collaboration.search')}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          onKeyDown={(event) => { if (event.key === 'Enter') locate(); }}
          className="mt-1 w-full rounded border border-border bg-bg-primary px-2 py-1.5 text-sm text-text-primary"
        />
      </label>
      <button type="button" className="self-end rounded border border-border bg-bg-surface px-3 py-1.5 text-sm" onClick={locate}>{t('insight.collaboration.locate')}</button>
      <button type="button" className="self-end rounded border border-border bg-bg-surface px-3 py-1.5 text-sm" onClick={clear}>{t('insight.collaboration.clear')}</button>
    </div>
  );
}

function FilterBar({ params, update, clear, t }: { params: URLSearchParams; update: (key: string, value: string) => void; clear: () => void; t: Translator }) {
  return (
    <form aria-label={t('insight.collaboration.filters.label')} className="grid gap-3 rounded border border-border bg-bg-surface p-3 md:grid-cols-4" onSubmit={(event) => event.preventDefault()}>
      {[
        ['project_id', t('insight.collaboration.filters.project'), 'text'],
        ['task_id', t('insight.collaboration.filters.task'), 'text'],
        ['agent_ref', t('insight.collaboration.filters.agent'), 'text'],
        ['since', t('insight.collaboration.filters.since'), 'datetime-local'],
        ['until', t('insight.collaboration.filters.until'), 'datetime-local'],
      ].map(([name, label, type]) => (
        <label key={name} className="text-xs text-text-muted">
          {label}
          <input aria-label={label} type={type} value={params.get(name) ?? ''} onChange={(event) => update(name, event.target.value)} className="mt-1 w-full rounded border border-border bg-bg-primary px-2 py-1.5 text-sm text-text-primary" />
        </label>
      ))}
      <SelectFilter name="relation_type" label={t('insight.collaboration.filters.relation')} values={RELATIONS} value={params.get('relation_type') ?? ''} update={update} t={t} />
      <SelectFilter name="polarity" label={t('insight.collaboration.filters.polarity')} values={POLARITIES} value={params.get('polarity') ?? ''} update={update} t={t} />
      <button type="button" className="self-end rounded border border-border bg-bg-primary px-3 py-1.5 text-sm" onClick={clear}>{t('insight.collaboration.clear')}</button>
    </form>
  );
}

function SelectFilter({ name, label, values, value, update, t }: { name: string; label: string; values: string[]; value: string; update: (key: string, value: string) => void; t: Translator }) {
  return (
    <label className="text-xs text-text-muted">
      {label}
      <select aria-label={label} value={value} onChange={(event) => update(name, event.target.value)} className="mt-1 w-full rounded border border-border bg-bg-primary px-2 py-1.5 text-sm text-text-primary">
        <option value="">{t('insight.collaboration.filters.all')}</option>
        {values.map((item) => <option key={item} value={item}>{labelFor(t, item)}</option>)}
      </select>
    </label>
  );
}

function OrganizationGraph({
  nodes,
  edges,
  effects,
  selectedEffectId,
  focusedNode,
  search,
  truncated,
  onSelectEffect,
  onFocusNode,
  t,
}: {
  nodes: CollaborationNode[];
  edges: CollaborationEdge[];
  effects: CollaborationEffect[];
  selectedEffectId: string | null;
  focusedNode: string | null;
  search: string;
  truncated: boolean;
  onSelectEffect: (id: string) => void;
  onFocusNode: (id: string | null) => void;
  t: Translator;
}) {
  const [zoom, setZoom] = useState(0.82);
  const [pan, setPan] = useState<Point>({ x: 16, y: 28 });
  const [drag, setDrag] = useState<{ id?: string; start: Point; panStart: Point } | null>(null);
  const [overrides, setOverrides] = useState<Record<string, Point>>({});
  const [selection, setSelection] = useState<SelectionBox | null>(null);
  const svgRef = useRef<SVGSVGElement | null>(null);
  const prepared = useMemo(() => prepareGraph(nodes, effects), [nodes, effects]);
  const highlighted = useMemo(() => neighborhood(focusedNode, edges), [focusedNode, edges]);
  const visible = useMemo(() => visibleGraph(prepared.nodes, edges, highlighted, search, zoom), [prepared.nodes, edges, edges, highlighted, search, zoom]);
  const lod = visible.nodes.length > INITIAL_LOD_LIMIT && zoom < 1.15;
  const shownNodes = lod ? visible.nodes.slice(0, INITIAL_LOD_LIMIT).map((node) => ({ ...node, ...(overrides[node.id] ?? {}) })) : visible.nodes.map((node) => ({ ...node, ...(overrides[node.id] ?? {}) }));
  const nodeMap = new Map(shownNodes.map((node) => [node.id, node]));
  const shownEdges = visible.edges.filter((edge) => nodeMap.has(edge.source) && nodeMap.has(edge.target));

  const toGraphPoint = (clientX: number, clientY: number): Point => {
    const rect = svgRef.current?.getBoundingClientRect();
    return { x: ((clientX - (rect?.left ?? 0)) - pan.x) / zoom, y: ((clientY - (rect?.top ?? 0)) - pan.y) / zoom };
  };

  const pointerDown = (event: React.PointerEvent<SVGSVGElement>) => {
    const id = (event.target as Element).closest('[data-node-id]')?.getAttribute('data-node-id') ?? undefined;
    const point = { x: event.clientX, y: event.clientY };
    setDrag({ id, start: point, panStart: pan });
    if (!id && event.shiftKey) {
      const p = toGraphPoint(event.clientX, event.clientY);
      setSelection({ x: p.x, y: p.y, w: 0, h: 0 });
    }
  };
  const pointerMove = (event: React.PointerEvent<SVGSVGElement>) => {
    if (!drag) return;
    const point = { x: event.clientX, y: event.clientY };
    if (selection) {
      const p = toGraphPoint(event.clientX, event.clientY);
      setSelection({ x: selection.x, y: selection.y, w: p.x - selection.x, h: p.y - selection.y });
    } else if (drag.id) {
      setOverrides((prev) => ({ ...prev, [drag.id ?? '']: toGraphPoint(event.clientX, event.clientY) }));
    } else {
      setPan({ x: drag.panStart.x + point.x - drag.start.x, y: drag.panStart.y + point.y - drag.start.y });
    }
  };
  const pointerUp = () => {
    if (selection) {
      const box = normalizeBox(selection);
      const hit = shownNodes.find((node) => node.x >= box.x && node.x <= box.x + box.w && node.y >= box.y && node.y <= box.y + box.h);
      if (hit) onFocusNode(hit.id);
    }
    setSelection(null);
    setDrag(null);
  };

  return (
    <section className="min-h-[32rem] rounded border border-border bg-bg-surface p-3" aria-label={t('insight.collaboration.graph')} data-testid="collaboration-graph">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <Legend t={t} />
        <div className="flex gap-1" aria-label={t('insight.collaboration.zoomControls')}>
          <button type="button" className="rounded border border-border px-2 py-1 text-sm" onClick={() => setZoom((value) => Math.min(2.2, value + 0.15))}>{t('insight.collaboration.zoomIn')}</button>
          <button type="button" className="rounded border border-border px-2 py-1 text-sm" onClick={() => setZoom((value) => Math.max(0.45, value - 0.15))}>{t('insight.collaboration.zoomOut')}</button>
          <button type="button" className="rounded border border-border px-2 py-1 text-sm" onClick={() => { setZoom(0.82); setPan({ x: 16, y: 28 }); setOverrides({}); onFocusNode(null); }}>{t('insight.collaboration.resetView')}</button>
        </div>
      </div>
      <svg
        ref={svgRef}
        role="img"
        aria-label={t('insight.collaboration.graph')}
        tabIndex={0}
        viewBox="0 0 1120 660"
        className="h-[32rem] w-full touch-none rounded border border-border bg-bg-primary"
        onPointerDown={pointerDown}
        onPointerMove={pointerMove}
        onPointerUp={pointerUp}
        onKeyDown={(event) => {
          if (event.key === '+' || event.key === '=') setZoom((value) => Math.min(2.2, value + 0.15));
          if (event.key === '-') setZoom((value) => Math.max(0.45, value - 0.15));
          if (event.key === 'Escape') onFocusNode(null);
        }}
      >
        <defs>
          <marker id="collab-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="#475569" /></marker>
        </defs>
        <g transform={`translate(${pan.x} ${pan.y}) scale(${zoom})`}>
          {prepared.clusters.map((cluster) => <PlanContainer key={cluster.id} cluster={cluster} t={t} />)}
          {shownEdges.map((edge) => <GraphEdgeView key={edge.id} edge={edge} a={nodeMap.get(edge.source)} b={nodeMap.get(edge.target)} selected={selectedEffectId === edge.effect_id} dimmed={highlighted.size > 0 && !highlighted.has(edge.source) && !highlighted.has(edge.target)} onSelect={onSelectEffect} t={t} />)}
          {shownNodes.map((node) => <GraphNodeView key={node.id} node={node} focused={focusedNode === node.id} dimmed={highlighted.size > 0 && !highlighted.has(node.id)} onFocus={onFocusNode} t={t} />)}
          {selection ? <rect {...normalizeBox(selection)} className="fill-brand/10 stroke-brand" strokeDasharray="5 4" /> : null}
        </g>
      </svg>
      <div className="mt-3 flex flex-wrap gap-3 text-xs text-text-muted">
        <span data-testid="collaboration-lod">{lod ? t('insight.collaboration.lod', { shown: shownNodes.length, total: visible.nodes.length }) : t('insight.collaboration.lodFull', { total: shownNodes.length })}</span>
        <span>{t('insight.collaboration.interactions')}</span>
        {truncated ? <span>{t('insight.collaboration.truncationHint')}</span> : null}
      </div>
      <div className="mt-3 grid gap-2 md:grid-cols-3" aria-label={t('insight.collaboration.edgeList')}>
        {shownEdges.slice(0, 48).map((edge) => <button key={edge.id} type="button" aria-pressed={selectedEffectId === edge.effect_id} onClick={() => onSelectEffect(edge.effect_id)} className="rounded border border-border px-3 py-2 text-left text-sm hover:bg-bg-subtle focus:ring-2 focus:ring-brand"><strong>{labelFor(t, edge.relation_type)}</strong> / {labelFor(t, edge.polarity)} / {t('insight.collaboration.magnitude', { value: edge.magnitude })}</button>)}
      </div>
    </section>
  );
}

function prepareGraph(nodes: CollaborationNode[], effects: CollaborationEffect[]): { nodes: LayoutNode[]; clusters: { id: string; label: string; x: number; y: number; w: number; h: number }[] } {
  const enriched = new Map<string, CollaborationNode>();
  for (const node of nodes) enriched.set(node.id, node);
  for (const effect of effects) enriched.set(`plan:${effect.project_id}`, { id: `plan:${effect.project_id}`, kind: 'plan', label: effect.project_id, project_id: effect.project_id });
  const clusterIds = Array.from(new Set(effects.map((effect) => effect.project_id || 'organization')));
  const clusterById = new Map(clusterIds.map((id, i) => [id, { id, label: id, x: 46 + i * 380, y: 52, w: 330, h: 510 }]));
  const taskProject = new Map<string, string>();
  for (const effect of effects) taskProject.set(`task:${effect.target_task_id}`, effect.project_id);
  const layout: LayoutNode[] = [];
  Array.from(enriched.values()).filter((node) => node.kind === 'agent').forEach((node, i) => layout.push({ ...node, x: 150 + (i % 5) * 180, y: 40 + Math.floor(i / 5) * 95, cluster: 'agents' }));
  Array.from(enriched.values()).filter((node) => node.kind === 'plan').forEach((node, i) => {
    const cluster = clusterById.get(node.project_id ?? node.label) ?? { id: node.id, label: node.label, x: 46 + i * 380, y: 52, w: 330, h: 510 };
    layout.push({ ...node, x: cluster.x + cluster.w / 2, y: cluster.y + 46, cluster: cluster.id });
  });
  Array.from(enriched.values()).filter((node) => node.kind === 'task').forEach((node, i) => {
    const cluster = clusterById.get(taskProject.get(node.id) ?? clusterIds[i % Math.max(1, clusterIds.length)] ?? 'organization') ?? { id: 'organization', label: 'organization', x: 46, y: 52, w: 330, h: 510 };
    const offset = i % 12;
    layout.push({ ...node, x: cluster.x + 78 + (offset % 2) * 160, y: cluster.y + 126 + Math.floor(offset / 2) * 62, cluster: cluster.id });
  });
  return { nodes: layout, clusters: Array.from(clusterById.values()) };
}

function visibleGraph(nodes: LayoutNode[], edges: CollaborationEdge[], highlighted: Set<string>, search: string, zoom: number) {
  const needle = search.trim().toLowerCase();
  let keep = new Set(nodes.map((node) => node.id));
  if (highlighted.size > 0) keep = highlighted;
  if (needle && highlighted.size === 0) keep = new Set(nodes.filter((node) => keep.has(node.id) && (node.id.toLowerCase().includes(needle) || node.label.toLowerCase().includes(needle))).map((node) => node.id));
  const nodeList = nodes.filter((node) => keep.has(node.id));
  const edgeList = edges.filter((edge) => keep.has(edge.source) && keep.has(edge.target));
  if (zoom < 0.65 && nodeList.length > INITIAL_LOD_LIMIT) {
    return { nodes: nodeList.filter((node) => node.kind !== 'task').concat(nodeList.filter((node) => node.kind === 'task').slice(0, 80)), edges: edgeList.slice(0, 120) };
  }
  return { nodes: nodeList, edges: edgeList };
}

function neighborhood(id: string | null, edges: CollaborationEdge[]): Set<string> {
  const out = new Set<string>();
  if (!id) return out;
  out.add(id);
  for (const edge of edges) {
    if (edge.source === id || edge.target === id) {
      out.add(edge.source);
      out.add(edge.target);
    }
  }
  return out;
}

function PlanContainer({ cluster, t }: { cluster: { id: string; label: string; x: number; y: number; w: number; h: number }; t: Translator }) {
  return (
    <g data-testid="collaboration-plan-container">
      <path d={`M${cluster.x + 18} ${cluster.y} H${cluster.x + cluster.w - 18} L${cluster.x + cluster.w} ${cluster.y + 18} V${cluster.y + cluster.h - 18} L${cluster.x + cluster.w - 18} ${cluster.y + cluster.h} H${cluster.x + 18} L${cluster.x} ${cluster.y + cluster.h - 18} V${cluster.y + 18} Z`} className="fill-bg-subtle stroke-border-strong" strokeWidth="2" />
      <text x={cluster.x + 18} y={cluster.y + 26} className="fill-text-muted text-[12px]">{t('insight.collaboration.planContainer')} {cluster.label}</text>
    </g>
  );
}

function GraphEdgeView({ edge, a, b, selected, dimmed, onSelect, t }: { edge: CollaborationEdge; a?: LayoutNode; b?: LayoutNode; selected: boolean; dimmed: boolean; onSelect: (id: string) => void; t: Translator }) {
  if (!a || !b) return null;
  const stroke = edge.polarity === 'positive' ? '#0f766e' : edge.polarity === 'negative' ? '#b42318' : edge.polarity === 'mixed' ? '#7c3aed' : '#64748b';
  const dash = edge.relation_type === 'assign' || edge.relation_type === 'reassign' ? undefined : edge.polarity === 'neutral' ? '2 6' : '8 5';
  return (
    <g role="button" tabIndex={0} aria-label={`${labelFor(t, edge.relation_type)} ${labelFor(t, edge.polarity)}`} onClick={() => onSelect(edge.effect_id)} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') onSelect(edge.effect_id); }} className={dimmed ? 'opacity-25' : undefined}>
      <line x1={a.x} y1={a.y} x2={b.x} y2={b.y} stroke={stroke} strokeWidth={selected ? edge.magnitude + 4 : edge.magnitude + 1} strokeDasharray={dash} markerEnd="url(#collab-arrow)" data-testid={a.kind === 'agent' && b.kind === 'agent' ? 'collaboration-agent-agent-edge' : 'collaboration-force-edge'} />
      <text x={(a.x + b.x) / 2} y={(a.y + b.y) / 2 - 7} textAnchor="middle" className="fill-text-primary text-[11px]">{labelFor(t, edge.relation_type)} / {polarityToken(edge.polarity)}</text>
    </g>
  );
}

function GraphNodeView({ node, focused, dimmed, onFocus, t }: { node: LayoutNode; focused: boolean; dimmed: boolean; onFocus: (id: string) => void; t: Translator }) {
  const cls = `${dimmed ? 'opacity-30' : ''} cursor-grab`;
  return (
    <g data-node-id={node.id} tabIndex={0} role="button" aria-label={`${kindLabel(t, node.kind)} ${node.label}`} onClick={() => onFocus(node.id)} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') onFocus(node.id); }} className={cls}>
      {node.kind === 'agent' ? <circle cx={node.x} cy={node.y} r={focused ? 34 : 28} className="fill-bg-primary stroke-brand" strokeWidth="3" /> : null}
      {node.kind === 'task' ? <rect x={node.x - 58} y={node.y - 24} width="116" height="48" rx="3" className="fill-bg-primary stroke-text-muted" strokeWidth={focused ? 4 : 2} /> : null}
      {node.kind === 'plan' ? <path d={`M${node.x - 62} ${node.y - 24} H${node.x + 48} L${node.x + 66} ${node.y} L${node.x + 48} ${node.y + 24} H${node.x - 62} L${node.x - 78} ${node.y} Z`} className="fill-bg-subtle stroke-accent" strokeWidth={focused ? 4 : 2} data-testid="collaboration-plan-node" /> : null}
      <text x={node.x} y={node.y + 4} textAnchor="middle" className="pointer-events-none fill-text-primary text-[11px]">{shortLabel(node.label)}</text>
    </g>
  );
}

function Legend({ t }: { t: Translator }) {
  return (
    <div className="flex flex-wrap gap-3 text-xs text-text-muted" data-testid="collaboration-legend">
      <span><span className="mr-1 inline-block h-3 w-3 rounded-full border-2 border-brand align-middle" />{t('insight.collaboration.legend.agent')}</span>
      <span><span className="mr-1 inline-block h-3 w-5 rounded-sm border-2 border-text-muted align-middle" />{t('insight.collaboration.legend.task')}</span>
      <span><span className="mr-1 inline-block h-3 w-5 border-2 border-accent align-middle" />{t('insight.collaboration.legend.plan')}</span>
      <span>{t('insight.collaboration.legend.relationship')}</span>
      <span>{t('insight.collaboration.legend.effect')}</span>
      <span>{t('insight.collaboration.legend.colorblind')}</span>
    </div>
  );
}

function SecondaryInsightPanels({ summary, effects, onSelect, t }: { summary: { positive_count: number; negative_count: number; neutral_count: number; mixed_count: number; affected_task_count: number }; effects: CollaborationEffect[]; onSelect: (id: string) => void; t: Translator }) {
  const ordered = [...effects].sort((a, b) => b.occurred_at.localeCompare(a.occurred_at)).slice(0, 12);
  return (
    <section className="grid gap-4 lg:grid-cols-[18rem_1fr]">
      <div aria-label={t('insight.collaboration.summary')} className="rounded border border-border bg-bg-surface p-3 text-sm">
        <h2 className="font-semibold">{t('insight.collaboration.summary')}</h2>
        <dl className="mt-2 grid grid-cols-2 gap-2">
          {(['positive', 'negative', 'neutral', 'mixed'] as const).map((key) => <div key={key}><dt className="text-xs text-text-muted">{labelFor(t, key)}</dt><dd className="font-semibold">{summary[`${key}_count`]}</dd></div>)}
          <div><dt className="text-xs text-text-muted">{t('insight.collaboration.affectedTasks')}</dt><dd className="font-semibold">{summary.affected_task_count}</dd></div>
        </dl>
      </div>
      <div className="rounded border border-border bg-bg-surface p-3" data-testid="collaboration-timeline">
        <h2 className="font-semibold">{t('insight.collaboration.timeline')}</h2>
        <ol className="mt-3 grid gap-2 md:grid-cols-2">
          {ordered.map((item) => <li key={item.effect_id}><button className="w-full rounded border border-border px-3 py-2 text-left text-sm hover:bg-bg-subtle focus:ring-2 focus:ring-brand" onClick={() => onSelect(item.effect_id)}><time className="block text-xs text-text-muted">{new Date(item.occurred_at).toLocaleString()}</time>{labelFor(t, item.relation_type)} / {labelFor(t, item.polarity)} / {item.source_agent_ref} to {item.target_task_id}</button></li>)}
        </ol>
      </div>
    </section>
  );
}

function EvidenceDrawer({ effect, effectId, onClose, t }: { effect: CollaborationEffect | null; effectId: string; onClose: () => void; t: Translator }) {
  const query = useCollaborationEvidence(effectId, effect?.project_id);
  return (
    <aside role="dialog" aria-modal="true" aria-labelledby="evidence-title" className="fixed inset-y-0 right-0 z-50 w-full max-w-lg overflow-y-auto border-l border-border bg-bg-primary p-5 shadow-xl" data-testid="collaboration-evidence-drawer">
      <div className="flex items-center justify-between"><h2 id="evidence-title" className="text-lg font-semibold">{t('insight.collaboration.evidence.title')}</h2><button type="button" onClick={onClose} aria-label={t('insight.collaboration.evidence.close')} className="rounded border border-border px-3 py-1">x</button></div>
      {effect ? <div className="mt-4 text-sm"><p>{t(effect.explanation_key, { defaultValue: effect.explanation_key })}</p><pre className="mt-2 overflow-auto rounded bg-bg-subtle p-2">{JSON.stringify({ before: effect.before_state, after: effect.after_state }, null, 2)}</pre></div> : null}
      {query.isLoading ? <p className="mt-4">{t('insight.collaboration.evidence.loading')}</p> : null}
      {query.isError ? <p role="alert" className="mt-4 text-danger">{t('insight.collaboration.evidence.failed')}</p> : null}
      <ol className="mt-4 space-y-3">{query.data?.evidence.map((event) => <li key={event.event_id} className="rounded border border-border p-3 text-sm"><strong>{event.event_type}</strong><time className="block text-xs text-text-muted">{new Date(event.occurred_at).toLocaleString()}</time><p>{event.actor_ref}</p><pre className="mt-2 overflow-auto text-xs">{JSON.stringify(event.payload, null, 2)}</pre></li>)}</ol>
    </aside>
  );
}

function CollaborationError({ error, t }: { error: unknown; t: Translator }) {
  const forbidden = error instanceof ApiError && (error.status === 401 || error.status === 403);
  return <State id={forbidden ? 'collaboration-forbidden' : 'collaboration-error'} title={forbidden ? t('insight.collaboration.forbidden') : t('insight.collaboration.failed')} body={error instanceof Error ? error.message : undefined} danger />;
}

function State({ id, title, body, danger = false }: { id: string; title: string; body?: string; danger?: boolean }) {
  return <div role={danger ? 'alert' : 'status'} data-testid={id} className={`rounded border p-4 ${danger ? 'border-danger/40 bg-danger/10' : 'border-border bg-bg-surface'}`}><strong>{title}</strong>{body ? <p className="mt-1 text-sm text-text-muted">{body}</p> : null}</div>;
}

function normalizeBox(box: SelectionBox): SelectionBox {
  return { x: Math.min(box.x, box.x + box.w), y: Math.min(box.y, box.y + box.h), w: Math.abs(box.w), h: Math.abs(box.h) };
}

function kindLabel(t: Translator, kind: GraphNodeKind): string {
  return t(`insight.collaboration.legend.${kind}`, { defaultValue: kind });
}
function shortLabel(label: string): string { return label.length > 18 ? `${label.slice(0, 16)}..` : label; }
function polarityToken(value: CollaborationPolarity): string { return value === 'positive' ? 'POS' : value === 'negative' ? 'NEG' : value === 'mixed' ? 'MIX' : 'NEU'; }
function labelFor(t: Translator, value: string): string { return t(`insight.collaboration.values.${value}`, { defaultValue: value.replaceAll('_', ' ') }); }

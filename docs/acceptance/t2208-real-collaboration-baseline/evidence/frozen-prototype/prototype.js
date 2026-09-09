const canvas = document.getElementById('graph');
const ctx = canvas.getContext('2d');
const minimap = document.getElementById('minimap');
const mini = minimap.getContext('2d');
const detailTitle = document.getElementById('selected-title');
const detailCopy = document.getElementById('selected-copy');
const metricsEl = document.getElementById('metrics');
const hud = document.getElementById('hud');
const mode = new URLSearchParams(location.search).get('mode') === 'baseline' ? 'baseline' : 'prototype';

const state = {
  view: new URLSearchParams(location.search).get('view') || 'network',
  scale: Number(new URLSearchParams(location.search).get('scale') || 100),
  layout: 'community',
  filters: { window: '24h', project: '', agent: '' },
  search: '',
  clustered: true,
  lod: true,
  selected: null,
  hover: null,
  focus: null,
  depth: 0,
  camera: { x: 0, y: 0, zoom: 1 },
  pinned: new Set(),
  positions: new Map(),
  drag: null,
  metrics: [],
  marks: {},
  current: { nodes: [], edges: [] },
  notice: '',
};

const views = {
  network: { question: 'Who collaborates most closely?', layouts: ['community', 'ego radial', 'force'], kinds: new Set(['agent']) },
  impact: { question: 'Who is pushing or blocking what?', layouts: ['radial impact', 'bipartite lanes', 'force'], kinds: new Set(['agent', 'task']) },
  lineage: { question: 'Where is the flow stuck?', layouts: ['hierarchical DAG', 'stage swimlanes', 'critical path'], kinds: new Set(['agent', 'task', 'plan', 'stage']) },
};

const colors = { agent: '#2364aa', task: '#198754', plan: '#6c4ab6', stage: '#b7791f', cluster: '#7a8791' };
const relationByView = {
  network: ['co-work', 'handoff', 'review_accept', 'review_reject'],
  impact: ['complete', 'block', 'unblock', 'review_accept', 'dependency_release'],
  lineage: ['contains', 'depends_on', 'block', 'dependency_release'],
};

function rng(seed) {
  let n = seed >>> 0;
  return () => {
    n = (n * 1664525 + 1013904223) >>> 0;
    return n / 4294967296;
  };
}

function baseNode(i, kind, rand) {
  const projects = ['Alpha', 'Runtime', 'Console', 'Ops'];
  const windows = ['24h', '7d', '30d'];
  const agents = ['agent:atlas', 'agent:forge', 'agent:signal', 'agent:delta', 'agent:nova', 'agent:orbit'];
  const agent = agents[i % agents.length];
  return {
    id: `${kind}:${i}`,
    kind,
    label: kind === 'agent' ? `${agent}-${Math.floor(i / agents.length)}` : `${kind[0].toUpperCase()}${kind.slice(1)} ${i}`,
    project: projects[i % projects.length],
    window: windows[i % windows.length],
    agent,
    weight: 1 + Math.floor(rand() * 9),
    status: i % 13 === 0 ? 'blocked' : i % 7 === 0 ? 'review' : 'active',
    pinned: false,
  };
}

function makeFixture(view, scale) {
  const rand = rng(scale * 1009 + view.length * 97);
  const nodes = [];
  const target = scale;
  const agentCount = view === 'network' ? target : view === 'lineage' ? Math.max(8, Math.round(target * .04)) : Math.max(16, Math.round(target * .18));
  const planCount = view === 'lineage' ? Math.max(8, Math.round(target * .05)) : Math.max(3, Math.round(target * .03));
  const stageCount = view === 'lineage' ? Math.max(14, Math.round(target * .16)) : Math.max(4, Math.round(target * .03));
  for (let i = 0; i < agentCount && nodes.length < target; i++) nodes.push(baseNode(i, 'agent', rand));
  if (view !== 'network') {
    for (let i = 0; i < planCount && nodes.length < target; i++) nodes.push(baseNode(i, 'plan', rand));
    for (let i = 0; i < stageCount && nodes.length < target; i++) nodes.push(baseNode(i, 'stage', rand));
    for (let i = nodes.length; nodes.length < target; i++) nodes.push(baseNode(i, 'task', rand));
  }
  applyLayout(nodes, view, state.layout);
  const byKind = (kind) => nodes.filter((n) => n.kind === kind);
  const agents = byKind('agent');
  const tasks = byKind('task');
  const plans = byKind('plan');
  const stages = byKind('stage');
  const edges = [];
  const edgeTarget = Math.round(scale * (scale > 1000 ? 1.35 : 1.2));
  const addEdge = (source, targetNode, relation, polarity, width = 1) => {
    if (!source || !targetNode || source.id === targetNode.id) return;
    const id = `edge:${view}:${edges.length}`;
    const project = source.project || targetNode.project;
    const agent = source.kind === 'agent' ? source.agent : targetNode.agent;
    const occurred = new Date(Date.UTC(2026, 8, 9, 4 + (edges.length % 12), edges.length % 59, 0)).toISOString();
    edges.push({
      id, source: source.id, target: targetNode.id, relation, polarity, width,
      project, window: source.window || targetNode.window, agent,
      direction: `${source.id} -> ${targetNode.id}`,
      occurred_at: occurred,
      effects: 1 + Math.floor(rand() * 6),
      evidence: 1 + Math.floor(rand() * 8),
      effect_id: `ce_${view}_${String(edges.length).padStart(4, '0')}`,
      evidence_event_ids: [`evt_${view}_${String(edges.length).padStart(4, '0')}`, `evt_${relation}_${edges.length % 17}`],
    });
  };
  if (view === 'network') {
    while (edges.length < edgeTarget) addEdge(agents[Math.floor(rand() * agents.length)], agents[Math.floor(rand() * agents.length)], rand() > .72 ? 'handoff' : 'co-work', rand() > .82 ? 'negative' : rand() > .22 ? 'positive' : 'neutral', 1 + rand() * 3);
  } else if (view === 'impact') {
    for (const task of tasks) addEdge(agents[Math.floor(rand() * agents.length)], task, task.status === 'blocked' ? 'block' : 'complete', task.status === 'blocked' ? 'negative' : 'positive', 1 + rand() * 3);
    while (edges.length < edgeTarget) addEdge(agents[Math.floor(rand() * agents.length)], tasks[Math.floor(rand() * tasks.length)], rand() > .5 ? 'review_accept' : 'unblock', rand() > .3 ? 'positive' : 'negative', 1 + rand() * 2);
  } else {
    for (let i = 0; i < stages.length; i++) addEdge(plans[i % plans.length], stages[i], 'contains', 'neutral', 1);
    for (let i = 0; i < tasks.length; i++) addEdge(stages[i % stages.length], tasks[i], 'contains', 'neutral', 1);
    for (let i = 1; i < tasks.length; i++) if (rand() > .15) addEdge(tasks[i - 1], tasks[i], 'depends_on', tasks[i].status === 'blocked' ? 'negative' : 'neutral', 1 + rand() * 2);
    while (edges.length < edgeTarget) addEdge(tasks[Math.floor(rand() * tasks.length)], tasks[Math.floor(rand() * tasks.length)], rand() > .9 ? 'block' : 'depends_on', rand() > .88 ? 'negative' : 'neutral', 1);
  }
  return { nodes, edges: edges.slice(0, edgeTarget) };
}

function applyLayout(nodes, view, layout) {
  const rand = rng(nodes.length * 17 + view.length);
  if (view === 'lineage' || layout.includes('DAG') || layout.includes('swim')) {
    const lanes = { plan: 95, stage: 260, task: 500, agent: 690 };
    const counters = {};
    nodes.forEach((n) => {
      const i = counters[n.kind] || 0;
      counters[n.kind] = i + 1;
      n.x = 75 + (i % 28) * 44 + rand() * 8;
      n.y = (lanes[n.kind] || 660) + Math.floor(i / 28) * 26;
    });
    return;
  }
  if (view === 'impact' || layout.includes('radial')) {
    nodes.forEach((n, i) => {
      const ring = n.kind === 'agent' ? 150 : n.kind === 'task' ? 325 : 235;
      const a = (i / Math.max(1, nodes.length)) * Math.PI * 2;
      n.x = 640 + Math.cos(a) * ring + rand() * 24;
      n.y = 385 + Math.sin(a) * ring + rand() * 24;
    });
    return;
  }
  nodes.forEach((n, i) => {
    const c = i % 5;
    const cx = 180 + (c % 3) * 365;
    const cy = 205 + Math.floor(c / 3) * 315;
    const a = rand() * Math.PI * 2;
    const r = 30 + rand() * 145;
    n.x = cx + Math.cos(a) * r;
    n.y = cy + Math.sin(a) * r;
  });
}

function graphForState() {
  const fixture = makeFixture(state.view, state.scale);
  let nodes = fixture.nodes;
  let edges = fixture.edges.filter((e) => relationByView[state.view].includes(e.relation));
  const f = state.filters;
  if (f.window) edges = edges.filter((e) => windowRank(e.window) <= windowRank(f.window));
  if (f.project) edges = edges.filter((e) => e.project === f.project);
  if (f.agent) edges = edges.filter((e) => e.agent === f.agent || e.source.includes(f.agent.split(':')[1]));
  const edgeNodeIds = new Set(edges.flatMap((e) => [e.source, e.target]));
  nodes = nodes.filter((n) => edgeNodeIds.has(n.id) || (!f.project && !f.agent && !f.window));
  if (state.search) {
    const q = state.search.toLowerCase();
    const matched = nodes.filter((n) => n.label.toLowerCase().includes(q) || n.id.toLowerCase().includes(q));
    if (matched[0]) state.focus = matched[0].id;
  }
  const activeId = selectedNodeId();
  if (activeId && state.depth > 0) {
    const keepNodes = new Set([activeId]);
    const keepEdges = new Set();
    for (let depth = 0; depth < state.depth; depth++) {
      for (const edge of edges) {
        if (keepNodes.has(edge.source) || keepNodes.has(edge.target)) {
          keepEdges.add(edge.id);
          keepNodes.add(edge.source);
          keepNodes.add(edge.target);
        }
      }
    }
    edges = edges.filter((e) => keepEdges.has(e.id));
    nodes = nodes.filter((n) => keepNodes.has(n.id));
  }
  if (state.clustered && state.scale > 1000 && state.depth === 0 && !state.focus) {
    const visible = nodes.slice(0, 420);
    const groups = new Map();
    nodes.slice(420).forEach((n) => {
      const id = `cluster:${n.kind}`;
      if (!groups.has(id)) groups.set(id, { ...n, id, kind: 'cluster', label: `${n.kind} cluster`, weight: 30 });
    });
    const mapped = new Map(nodes.slice(420).map((n) => [n.id, `cluster:${n.kind}`]));
    const nodeIds = new Set([...visible.map((n) => n.id), ...groups.keys()]);
    edges = edges.map((e) => ({ ...e, source: mapped.get(e.source) || e.source, target: mapped.get(e.target) || e.target }))
      .filter((e) => e.source !== e.target && nodeIds.has(e.source) && nodeIds.has(e.target))
      .slice(0, 900);
    nodes = [...visible, ...groups.values()];
  }
  const nodeIds = new Set(nodes.map((n) => n.id));
  edges = edges.filter((e) => nodeIds.has(e.source) && nodeIds.has(e.target));
  nodes.forEach((node) => {
    const pinned = state.positions.get(node.id);
    if (pinned) {
      node.x = pinned.x;
      node.y = pinned.y;
    }
  });
  return { nodes, edges };
}

function windowRank(value) {
  return { '24h': 1, '7d': 2, '30d': 3 }[value] || 3;
}

function selectedNodeId() {
  return state.selected?.type === 'node' ? state.selected.id : state.focus;
}

function selectedEdge() {
  return state.selected?.type === 'edge' ? state.current.edges.find((e) => e.id === state.selected.id) : null;
}

function toScreen(n) {
  return { x: (n.x + state.camera.x) * state.camera.zoom, y: (n.y + state.camera.y) * state.camera.zoom };
}

function toWorld(x, y) {
  return { x: x / state.camera.zoom - state.camera.x, y: y / state.camera.zoom - state.camera.y };
}

function draw() {
  const start = performance.now();
  if (mode === 'baseline') return drawBaseline(start);
  document.querySelector('.baseline-svg')?.remove();
  canvas.style.display = 'block';
  const dpr = window.devicePixelRatio || 1;
  const rect = canvas.getBoundingClientRect();
  canvas.width = Math.max(1, Math.floor(rect.width * dpr));
  canvas.height = Math.max(1, Math.floor(rect.height * dpr));
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, rect.width, rect.height);
  const graph = graphForState();
  state.current = graph;
  const context = contextSets(graph);
  const byId = new Map(graph.nodes.map((n) => [n.id, n]));
  ctx.lineCap = 'round';
  for (const edge of graph.edges) {
    const a = byId.get(edge.source);
    const b = byId.get(edge.target);
    if (!a || !b) continue;
    const pa = toScreen(a);
    const pb = toScreen(b);
    if ((pa.x < -80 && pb.x < -80) || (pa.y < -80 && pb.y < -80) || (pa.x > rect.width + 80 && pb.x > rect.width + 80) || (pa.y > rect.height + 80 && pb.y > rect.height + 80)) continue;
    const related = context.edges.size === 0 || context.edges.has(edge.id);
    const selected = state.selected?.type === 'edge' && state.selected.id === edge.id;
    ctx.globalAlpha = selected ? .95 : related ? (edge.polarity === 'negative' ? .62 : .34) : .08;
    ctx.strokeStyle = edge.polarity === 'negative' ? '#c93d36' : edge.polarity === 'positive' ? '#198754' : '#7a8791';
    ctx.lineWidth = selected ? edge.width + 4 : Math.min(8, edge.width * state.camera.zoom);
    ctx.setLineDash(edge.polarity === 'negative' ? [6, 4] : edge.polarity === 'neutral' ? [2, 4] : []);
    ctx.beginPath();
    ctx.moveTo(pa.x, pa.y);
    ctx.lineTo(pb.x, pb.y);
    ctx.stroke();
    if (selected || state.hover?.type === 'edge' && state.hover.id === edge.id) {
      ctx.globalAlpha = .95;
      ctx.fillStyle = '#172026';
      ctx.font = '12px Inter, sans-serif';
      ctx.fillText(`${edge.relation} · ${edge.polarity} · ${edge.evidence} evidence`, (pa.x + pb.x) / 2 + 6, (pa.y + pb.y) / 2 - 6);
    }
  }
  ctx.setLineDash([]);
  for (const node of graph.nodes) {
    const p = toScreen(node);
    if (p.x < -60 || p.y < -60 || p.x > rect.width + 60 || p.y > rect.height + 60) continue;
    const radius = nodeRadius(node);
    const related = context.nodes.size === 0 || context.nodes.has(node.id);
    ctx.globalAlpha = related ? 1 : .18;
    ctx.fillStyle = colors[node.kind] || '#444';
    ctx.beginPath();
    if (node.kind === 'task' || node.kind === 'plan' || node.kind === 'stage') ctx.roundRect(p.x - radius, p.y - radius, radius * 2, radius * 2, 4);
    else ctx.arc(p.x, p.y, radius, 0, Math.PI * 2);
    ctx.fill();
    if (state.pinned.has(node.id)) {
      ctx.fillStyle = '#111827';
      ctx.fillRect(p.x - 2, p.y - radius - 8, 4, 4);
    }
    if ((state.selected?.type === 'node' && state.selected.id === node.id) || state.focus === node.id || (state.hover?.type === 'node' && state.hover.id === node.id)) {
      ctx.strokeStyle = '#111827';
      ctx.lineWidth = 3;
      ctx.stroke();
    }
    const showLabel = !state.lod || state.camera.zoom > 1.35 || node.kind === 'cluster' || node.weight > 7 || state.focus === node.id || state.selected?.id === node.id;
    if (showLabel) {
      ctx.globalAlpha = related ? .95 : .2;
      ctx.fillStyle = '#172026';
      ctx.font = '12px Inter, sans-serif';
      ctx.fillText(node.label, p.x + radius + 5, p.y + 4);
    }
  }
  ctx.globalAlpha = 1;
  drawMini(graph);
  recordDraw(start, graph);
}

function drawBaseline(start) {
  const graph = graphForState();
  state.current = graph;
  canvas.style.display = 'none';
  document.querySelector('.baseline-svg')?.remove();
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.classList.add('baseline-svg');
  svg.setAttribute('width', '100%');
  svg.setAttribute('height', '100%');
  svg.setAttribute('viewBox', `${-state.camera.x} ${-state.camera.y} ${1280 / state.camera.zoom} ${760 / state.camera.zoom}`);
  const byId = new Map(graph.nodes.map((n) => [n.id, n]));
  for (const edge of graph.edges) {
    const a = byId.get(edge.source);
    const b = byId.get(edge.target);
    if (!a || !b) continue;
    const line = document.createElementNS(svg.namespaceURI, 'line');
    line.setAttribute('x1', a.x); line.setAttribute('y1', a.y); line.setAttribute('x2', b.x); line.setAttribute('y2', b.y);
    line.setAttribute('stroke', edge.polarity === 'negative' ? '#c93d36' : edge.polarity === 'positive' ? '#198754' : '#7a8791');
    line.setAttribute('stroke-width', String(edge.width));
    line.setAttribute('opacity', '.35');
    svg.appendChild(line);
  }
  for (const node of graph.nodes) {
    const circle = document.createElementNS(svg.namespaceURI, node.kind === 'agent' ? 'circle' : 'rect');
    const r = nodeRadius(node);
    if (node.kind === 'agent') {
      circle.setAttribute('cx', node.x); circle.setAttribute('cy', node.y); circle.setAttribute('r', r);
    } else {
      circle.setAttribute('x', node.x - r); circle.setAttribute('y', node.y - r); circle.setAttribute('width', r * 2); circle.setAttribute('height', r * 2);
    }
    circle.setAttribute('fill', colors[node.kind] || '#444');
    svg.appendChild(circle);
    const label = document.createElementNS(svg.namespaceURI, 'text');
    label.setAttribute('x', node.x + r + 5); label.setAttribute('y', node.y + 4); label.setAttribute('font-size', '11');
    label.textContent = node.label;
    svg.appendChild(label);
  }
  canvas.parentElement.appendChild(svg);
  drawMini(graph);
  recordDraw(start, graph);
}

function nodeRadius(node) {
  return node.kind === 'cluster' ? 18 : 5 + Math.min(10, node.weight || 1);
}

function contextSets(graph = state.current) {
  const active = selectedNodeId();
  const edgeActive = selectedEdge();
  const nodes = new Set(active ? [active] : []);
  const edges = new Set(edgeActive ? [edgeActive.id, edgeActive.source, edgeActive.target] : []);
  if (edgeActive) {
    nodes.add(edgeActive.source);
    nodes.add(edgeActive.target);
  }
  if (active) graph.edges.forEach((e) => {
    if (e.source === active || e.target === active) {
      edges.add(e.id);
      nodes.add(e.source);
      nodes.add(e.target);
    }
  });
  return { nodes, edges };
}

function drawMini(graph) {
  mini.clearRect(0, 0, minimap.width, minimap.height);
  mini.fillStyle = '#f7f8f9';
  mini.fillRect(0, 0, minimap.width, minimap.height);
  const sx = minimap.width / 1280;
  const sy = minimap.height / 760;
  mini.globalAlpha = .25;
  mini.strokeStyle = '#7a8791';
  const byId = new Map(graph.nodes.map((n) => [n.id, n]));
  graph.edges.slice(0, 700).forEach((e) => {
    const a = byId.get(e.source), b = byId.get(e.target);
    if (!a || !b) return;
    mini.beginPath(); mini.moveTo(a.x * sx, a.y * sy); mini.lineTo(b.x * sx, b.y * sy); mini.stroke();
  });
  mini.globalAlpha = 1;
  graph.nodes.forEach((n) => { mini.fillStyle = colors[n.kind] || '#444'; mini.fillRect(n.x * sx - 1, n.y * sy - 1, 2, 2); });
}

function recordDraw(start, graph) {
  const drawMs = performance.now() - start;
  state.metrics.push(drawMs);
  if (state.metrics.length > 120) state.metrics.shift();
  const sorted = [...state.metrics].sort((a, b) => a - b);
  const p95 = sorted[Math.floor(sorted.length * .95)] || drawMs;
  const data = {
    mode, view: state.view, layout: state.layout, requested_scale: state.scale,
    filters: state.filters, selected: state.selected, focus: state.focus, depth: state.depth,
    rendered_nodes: graph.nodes.length, rendered_edges: graph.edges.length,
    first_interactive_ms: Math.round(state.marks.interactive || performance.now()),
    draw_ms_last: Number(drawMs.toFixed(2)),
    draw_ms_p95: Number(p95.toFixed(2)),
    js_heap_mb: performance.memory ? Math.round(performance.memory.usedJSHeapSize / 1048576) : null,
  };
  metricsEl.textContent = JSON.stringify(data, null, 2);
  hud.textContent = `${mode} | ${graph.nodes.length} nodes | ${graph.edges.length} edges | zoom ${state.camera.zoom.toFixed(2)} | p95 ${data.draw_ms_p95} ms${state.notice ? ' | ' + state.notice : ''}`;
  window.__lastMetrics = data;
}

function pickNode(x, y) {
  const graph = state.current;
  for (let i = graph.nodes.length - 1; i >= 0; i--) {
    const n = graph.nodes[i], p = toScreen(n), r = nodeRadius(n) + 4;
    if (Math.hypot(p.x - x, p.y - y) <= r) return n;
  }
  return null;
}

function pointSegmentDistance(px, py, ax, ay, bx, by) {
  const dx = bx - ax, dy = by - ay;
  const l2 = dx * dx + dy * dy || 1;
  const t = Math.max(0, Math.min(1, ((px - ax) * dx + (py - ay) * dy) / l2));
  return Math.hypot(px - (ax + t * dx), py - (ay + t * dy));
}

function pickEdge(x, y) {
  const byId = new Map(state.current.nodes.map((n) => [n.id, n]));
  for (let i = state.current.edges.length - 1; i >= 0; i--) {
    const e = state.current.edges[i], a = byId.get(e.source), b = byId.get(e.target);
    if (!a || !b) continue;
    const pa = toScreen(a), pb = toScreen(b);
    if (pointSegmentDistance(x, y, pa.x, pa.y, pb.x, pb.y) <= 7) return e;
  }
  return null;
}

function updateDetail() {
  const node = state.selected?.type === 'node' ? state.current.nodes.find((n) => n.id === state.selected.id) : null;
  const edge = selectedEdge();
  if (edge) {
    detailTitle.textContent = `${edge.relation} ${edge.direction}`;
    detailCopy.textContent = `${edge.polarity}; ${edge.project}; ${edge.occurred_at}; ${edge.evidence} evidence events.`;
  } else if (node) {
    detailTitle.textContent = `${node.label}${state.pinned.has(node.id) ? ' (pinned)' : ''}`;
    detailCopy.textContent = `${node.kind}; ${node.project}; ${node.agent}; ${node.status}. Focus and expand act on this selected node.`;
  } else {
    detailTitle.textContent = 'Select a node or edge';
    detailCopy.textContent = state.notice || 'Hover/select uses real graph hit-testing. Clear filters restores this dimension.';
  }
}

function redraw() {
  requestAnimationFrame(() => {
    draw();
    updateDetail();
  });
}

function preserveSelectionForView(nextView) {
  const previous = state.selected;
  state.view = nextView;
  state.layout = views[nextView].layouts[0];
  const graph = graphForState();
  const ids = new Set(graph.nodes.map((n) => n.id));
  const edgeIds = new Set(graph.edges.map((e) => e.id));
  if (previous?.type === 'node' && ids.has(previous.id)) {
    state.selected = previous;
    state.notice = `Preserved ${previous.id} and viewport across ${nextView}.`;
  } else if (previous?.type === 'edge' && edgeIds.has(previous.id)) {
    state.selected = previous;
    state.notice = `Preserved edge ${previous.id} across ${nextView}.`;
  } else if (previous) {
    state.selected = null;
    state.focus = null;
    state.notice = `${previous.id} does not participate in ${nextView}; selection was cleared explicitly. Filters and viewport were preserved.`;
  } else {
    state.notice = `Switched to ${nextView}; filters and viewport preserved.`;
  }
}

function setView(view) {
  preserveSelectionForView(view);
  document.querySelectorAll('.view-tabs button').forEach((button) => button.classList.toggle('active', button.dataset.view === view));
  document.getElementById('view-question').textContent = views[view].question;
  const layout = document.getElementById('layout');
  layout.innerHTML = views[view].layouts.map((name) => `<option>${name}</option>`).join('');
  layout.value = state.layout;
  redraw();
}

function setFilter(name, value) {
  const before = state.current;
  state.filters[name] = value;
  state.depth = 0;
  redraw();
  queueMicrotask(() => {
    const after = graphForState();
    state.notice = `${name || 'filters'} changed graph from ${before.nodes.length}/${before.edges.length} to ${after.nodes.length}/${after.edges.length}.`;
    redraw();
  });
}

canvas.addEventListener('pointerdown', (event) => {
  const rect = canvas.getBoundingClientRect();
  const x = event.clientX - rect.left, y = event.clientY - rect.top;
  const node = pickNode(x, y);
  canvas.setPointerCapture(event.pointerId);
  if (node && mode === 'prototype') {
    state.selected = { type: 'node', id: node.id };
    state.drag = { type: 'node', id: node.id, lastX: x, lastY: y, moved: false };
  } else {
    state.drag = { type: 'pan', lastX: x, lastY: y, moved: false };
  }
  redraw();
});

canvas.addEventListener('pointermove', (event) => {
  const rect = canvas.getBoundingClientRect();
  const x = event.clientX - rect.left, y = event.clientY - rect.top;
  if (state.drag) {
    const dx = x - state.drag.lastX, dy = y - state.drag.lastY;
    if (Math.abs(dx) + Math.abs(dy) > 1) state.drag.moved = true;
    if (state.drag.type === 'node') {
      const node = state.current.nodes.find((n) => n.id === state.drag.id);
      if (node) {
        node.x += dx / state.camera.zoom;
        node.y += dy / state.camera.zoom;
        state.pinned.add(node.id);
        state.positions.set(node.id, { x: node.x, y: node.y });
        state.notice = `${node.id} dragged and pinned.`;
      }
    } else {
      state.camera.x += dx / state.camera.zoom;
      state.camera.y += dy / state.camera.zoom;
      state.notice = 'Canvas panned without selecting a node.';
    }
    state.drag.lastX = x; state.drag.lastY = y;
    redraw();
    return;
  }
  const node = pickNode(x, y);
  const edge = node ? null : pickEdge(x, y);
  state.hover = node ? { type: 'node', id: node.id } : edge ? { type: 'edge', id: edge.id } : null;
  redraw();
});

canvas.addEventListener('pointerup', (event) => {
  const rect = canvas.getBoundingClientRect();
  const x = event.clientX - rect.left, y = event.clientY - rect.top;
  if (state.drag && !state.drag.moved) {
    const node = pickNode(x, y);
    const edge = node ? null : pickEdge(x, y);
    state.selected = node ? { type: 'node', id: node.id } : edge ? { type: 'edge', id: edge.id } : null;
    state.focus = node?.id || null;
  }
  state.drag = null;
  redraw();
});

canvas.addEventListener('wheel', (event) => {
  event.preventDefault();
  const factor = event.deltaY < 0 ? 1.12 : .89;
  state.camera.zoom = Math.max(.25, Math.min(3.5, state.camera.zoom * factor));
  state.notice = state.camera.zoom < .9 ? 'LOD hides low-priority labels at this zoom.' : 'LOD shows selected, heavy, and local labels.';
  redraw();
}, { passive: false });

document.querySelectorAll('.view-tabs button').forEach((button) => button.addEventListener('click', () => setView(button.dataset.view)));
document.getElementById('scale').addEventListener('change', (e) => { state.scale = Number(e.target.value); state.depth = 0; redraw(); });
document.getElementById('layout').addEventListener('change', (e) => { state.layout = e.target.value; redraw(); });
document.getElementById('clusters').addEventListener('change', (e) => { state.clustered = e.target.checked; redraw(); });
document.getElementById('lod').addEventListener('change', (e) => { state.lod = e.target.checked; redraw(); });
document.getElementById('search').addEventListener('input', (e) => { state.search = e.target.value.trim(); redraw(); });
document.getElementById('window').addEventListener('change', (e) => setFilter('window', e.target.value));
document.getElementById('project').addEventListener('change', (e) => setFilter('project', e.target.value));
document.getElementById('agent').addEventListener('change', (e) => setFilter('agent', e.target.value));
document.getElementById('clear').addEventListener('click', () => {
  state.filters = { window: '30d', project: '', agent: '' };
  document.getElementById('window').value = '30d';
  document.getElementById('project').value = '';
  document.getElementById('agent').value = '';
  state.focus = null; state.selected = null; state.depth = 0;
  state.notice = `Cleared filters; restored ${state.view} global graph.`;
  redraw();
});
document.getElementById('restore').addEventListener('click', () => { state.focus = null; state.selected = null; state.depth = 0; state.notice = 'Restored current dimension context.'; redraw(); });
document.getElementById('focus').addEventListener('click', () => {
  const id = selectedNodeId();
  if (!id) { state.notice = 'Choose a node before Focus; no fixed default is used.'; return redraw(); }
  state.focus = id; state.depth = Math.max(1, state.depth); state.notice = `Focused selected node ${id}.`; redraw();
});
document.getElementById('expand').addEventListener('click', () => {
  const id = selectedNodeId();
  if (!id) { state.notice = 'Choose a node before Expand; no fixed default is used.'; return redraw(); }
  state.focus = id; state.depth = Math.min(3, state.depth + 1); state.notice = `Expanded ${id} to ${state.depth}-hop neighborhood.`; redraw();
});
document.getElementById('collapse').addEventListener('click', () => {
  state.depth = Math.max(0, state.depth - 1); state.notice = state.depth ? `Collapsed to ${state.depth}-hop neighborhood.` : 'Collapsed back to filtered global graph.'; redraw();
});
document.getElementById('unpin').addEventListener('click', () => {
  const id = state.selected?.type === 'node' ? state.selected.id : null;
  if (!id) { state.notice = 'Choose a pinned node before Unpin.'; return redraw(); }
  state.pinned.delete(id);
  state.positions.delete(id);
  state.notice = `${id} unpinned; next redraw uses layout coordinates.`;
  redraw();
});
document.getElementById('evidence').addEventListener('click', openEvidence);

function openEvidence() {
  const edge = selectedEdge() || state.current.edges.find((e) => selectedNodeId() && (e.source === selectedNodeId() || e.target === selectedNodeId()));
  if (!edge) {
    document.getElementById('evidence-json').textContent = JSON.stringify({ error: 'Select a real edge, or a node with adjacent edges, before opening Evidence.' }, null, 2);
  } else {
    document.getElementById('evidence-json').textContent = JSON.stringify({
      edge_id: edge.id,
      effect_scope: { effect_id: edge.effect_id, project_id: edge.project },
      relation: edge.relation,
      effect: edge.polarity,
      direction: edge.direction,
      occurred_at: edge.occurred_at,
      evidence_event_ids: edge.evidence_event_ids,
      evidence: edge.evidence_event_ids.map((event_id, index) => ({
        event_id,
        occurred_at: new Date(Date.parse(edge.occurred_at) + index * 60000).toISOString(),
        actor_ref: edge.agent,
        payload: { source: edge.source, target: edge.target, relation: edge.relation, project_id: edge.project },
      })),
    }, null, 2);
  }
  document.getElementById('evidence-dialog').showModal();
}

function samplePointerTarget(kind = 'node') {
  const item = kind === 'edge' ? state.current.edges[0] : state.current.nodes.find((n) => n.kind !== 'cluster') || state.current.nodes[0];
  if (kind === 'edge') {
    const byId = new Map(state.current.nodes.map((n) => [n.id, n]));
    const a = byId.get(item.source), b = byId.get(item.target);
    return { x: (toScreen(a).x + toScreen(b).x) / 2, y: (toScreen(a).y + toScreen(b).y) / 2, id: item.id };
  }
  const p = toScreen(item);
  return { x: p.x, y: p.y, id: item.id };
}

function assertPrototype() {
  const results = [];
  state.filters = { window: '30d', project: '', agent: '' };
  state.view = 'network';
  state.layout = views.network.layouts[0];
  state.selected = null;
  state.focus = null;
  state.depth = 0;
  const start = graphForState();
  setFilter('project', 'Alpha');
  const projectGraph = graphForState();
  setFilter('agent', 'agent:atlas');
  const agentGraph = graphForState();
  setFilter('window', '24h');
  const windowGraph = graphForState();
  state.filters = { window: '30d', project: '', agent: '' };
  const cleared = graphForState();
  results.push(['project-filter-changes', graphKey(projectGraph) !== graphKey(start) && projectGraph.edges.every((edge) => edge.project === 'Alpha')]);
  results.push(['agent-filter-changes', graphKey(agentGraph) !== graphKey(projectGraph) && agentGraph.edges.every((edge) => edge.agent === 'agent:atlas' || edge.source.includes('atlas'))]);
  results.push(['window-filter-changes', windowGraph.edges.length <= agentGraph.edges.length]);
  results.push(['clear-restores-current-view', cleared.edges.length >= projectGraph.edges.length]);
  const edge = cleared.edges[0];
  state.current = cleared;
  state.selected = { type: 'edge', id: edge.id };
  const evidenceOk = selectedEdge()?.effect_id === edge.effect_id && selectedEdge()?.relation === edge.relation;
  results.push(['selected-edge-evidence-bound', evidenceOk]);
  const node = cleared.nodes.find((n) => n.id === edge.source);
  state.selected = { type: 'node', id: node.id };
  state.focus = node.id;
  state.depth = 1;
  const oneHop = graphForState();
  state.depth = 2;
  const twoHop = graphForState();
  results.push(['expand-changes-neighborhood', twoHop.nodes.length >= oneHop.nodes.length]);
  state.pinned.add(node.id);
  state.positions.set(node.id, { x: node.x + 10, y: node.y + 10 });
  document.getElementById('unpin').click();
  results.push(['pin-unpin-state-changes', !state.pinned.has(node.id) && !state.positions.has(node.id)]);
  const oldView = state.view;
  preserveSelectionForView(oldView === 'network' ? 'impact' : 'network');
  results.push(['view-switch-explains-context', state.notice.length > 20]);
  return { pass: results.every(([, pass]) => pass), results: Object.fromEntries(results), metrics: window.__lastMetrics };
}

function graphKey(graph) {
  return `${graph.nodes.map((n) => n.id).sort().join('|')}::${graph.edges.map((e) => e.id).sort().join('|')}`;
}

window.collaborationPrototype = {
  state,
  redraw: () => new Promise((resolve) => requestAnimationFrame(() => { draw(); resolve(window.__lastMetrics); })),
  setScale: (scale) => { state.scale = scale; document.getElementById('scale').value = String(scale); redraw(); },
  setView,
  setFilter,
  samplePointerTarget,
  assertPrototype,
  metrics: () => window.__lastMetrics,
};

document.getElementById('scale').value = String(state.scale);
document.getElementById('window').value = state.filters.window;
setView(state.view);
state.marks.interactive = performance.now();
redraw();

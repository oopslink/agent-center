const canvas = document.getElementById('graph');
const ctx = canvas.getContext('2d');
const minimap = document.getElementById('minimap');
const mini = minimap.getContext('2d');
const state = {
  view: 'network',
  scale: 100,
  layout: 'community',
  search: '',
  clustered: true,
  lod: true,
  focus: null,
  selected: null,
  camera: { x: 0, y: 0, zoom: 1 },
  fixed: new Set(),
  dragging: null,
  metrics: [],
  current: null,
};

const views = {
  network: {
    question: 'Who collaborates most closely?',
    layouts: ['community', 'ego radial', 'force'],
  },
  impact: {
    question: 'Who is pushing or blocking what?',
    layouts: ['radial impact', 'bipartite lanes', 'force'],
  },
  lineage: {
    question: 'Where is the flow stuck?',
    layouts: ['hierarchical DAG', 'stage swimlanes', 'critical path'],
  },
};

const colors = {
  agent: '#2364aa',
  task: '#198754',
  plan: '#6c4ab6',
  stage: '#b7791f',
  cluster: '#7a8791',
};

function rng(seed) {
  let n = seed >>> 0;
  return () => {
    n = (n * 1664525 + 1013904223) >>> 0;
    return n / 4294967296;
  };
}

function generateGraph(view, scale) {
  const rand = rng((view.length * 991) + scale);
  const nodeTarget = scale;
  const edgeTarget = Math.round(scale * (scale > 1000 ? 1.35 : 1.2));
  const nodes = [];
  const edges = [];
  const agentCount = view === 'network' ? nodeTarget : view === 'lineage' ? Math.max(4, Math.round(nodeTarget * .02)) : Math.max(10, Math.round(nodeTarget * .18));
  const planCount = view === 'lineage' ? Math.max(6, Math.round(nodeTarget * .05)) : Math.max(2, Math.round(nodeTarget * .03));
  const stageCount = view === 'lineage' ? Math.max(12, Math.round(nodeTarget * .20)) : Math.max(4, Math.round(nodeTarget * .04));
  for (let i = 0; i < agentCount && nodes.length < nodeTarget; i++) nodes.push({ id: `agent:${i}`, kind: 'agent', label: `agent:${['atlas','forge','signal','delta','nova'][i % 5]}-${i}`, weight: 1 + Math.floor(rand() * 8) });
  if (view !== 'network') {
    for (let i = 0; i < planCount && nodes.length < nodeTarget; i++) nodes.push({ id: `plan:${i}`, kind: 'plan', label: `Plan ${i}`, status: i % 4 === 0 ? 'blocked' : 'running', weight: 2 });
    for (let i = 0; i < stageCount && nodes.length < nodeTarget; i++) nodes.push({ id: `stage:${i}`, kind: 'stage', label: `Stage ${i}`, status: i % 5 === 0 ? 'blocked' : 'ready', weight: 1 });
    while (nodes.length < nodeTarget) {
      const i = nodes.length;
      nodes.push({ id: `task:${i}`, kind: 'task', label: `Task ${i}`, status: i % 11 === 0 ? 'blocked' : i % 7 === 0 ? 'review' : 'active', weight: 1 });
    }
  }
  const byKind = (kind) => nodes.filter((n) => n.kind === kind);
  const agents = byKind('agent');
  const tasks = byKind('task');
  const plans = byKind('plan');
  const stages = byKind('stage');
  const addEdge = (source, target, relation, polarity, width = 1) => {
    if (!source || !target || source.id === target.id) return;
    edges.push({
      id: `edge:${edges.length}`,
      source: source.id,
      target: target.id,
      relation,
      polarity,
      width,
      effects: 1 + Math.floor(rand() * 6),
      evidence: 1 + Math.floor(rand() * 8),
    });
  };
  if (view === 'network') {
    while (edges.length < edgeTarget) addEdge(agents[Math.floor(rand() * agents.length)], agents[Math.floor(rand() * agents.length)], rand() > .72 ? 'handoff' : 'co-work', rand() > .82 ? 'negative' : rand() > .22 ? 'positive' : 'neutral', 1 + rand() * 3);
  } else if (view === 'impact') {
    for (const task of tasks) addEdge(agents[Math.floor(rand() * agents.length)], task, rand() > .35 ? 'complete' : 'block', task.status === 'blocked' ? 'negative' : 'positive', 1 + rand() * 3);
    while (edges.length < edgeTarget) addEdge(agents[Math.floor(rand() * agents.length)], tasks[Math.floor(rand() * tasks.length)], rand() > .5 ? 'review' : 'unblock', rand() > .3 ? 'positive' : 'negative', 1 + rand() * 2);
  } else {
    for (let i = 0; i < stages.length; i++) addEdge(plans[i % plans.length], stages[i], 'contains', 'neutral', 1);
    for (let i = 0; i < tasks.length; i++) addEdge(stages[i % stages.length], tasks[i], 'contains', 'neutral', 1);
    for (let i = 1; i < tasks.length; i++) if (rand() > .15) addEdge(tasks[i - 1], tasks[i], 'depends_on', tasks[i].status === 'blocked' ? 'negative' : 'neutral', 1 + rand() * 2);
    while (edges.length < edgeTarget) addEdge(tasks[Math.floor(rand() * tasks.length)], tasks[Math.floor(rand() * tasks.length)], 'depends_on', rand() > .88 ? 'negative' : 'neutral', 1);
  }
  applyLayout(nodes, edges, view, state.layout);
  return { nodes, edges: edges.slice(0, edgeTarget) };
}

function applyLayout(nodes, edges, view, layout) {
  const w = 1200;
  const h = 760;
  const rand = rng(nodes.length * 17 + edges.length);
  if (view === 'lineage' || layout.includes('DAG') || layout.includes('swim')) {
    const lanes = { plan: 105, stage: 285, task: 520, agent: 690 };
    const counters = {};
    nodes.forEach((n) => {
      const i = counters[n.kind] || 0;
      counters[n.kind] = i + 1;
      const row = Math.floor(i / 24);
      n.x = 70 + (i % 24) * 46 + rand() * 8;
      n.y = (lanes[n.kind] || 660) + row * 28;
    });
    return;
  }
  if (view === 'impact' || layout.includes('radial')) {
    const center = { x: w / 2, y: h / 2 };
    nodes.forEach((n, i) => {
      const ring = n.kind === 'agent' ? 150 : n.kind === 'task' ? 310 : 240;
      const a = (i / nodes.length) * Math.PI * 2;
      n.x = center.x + Math.cos(a) * ring + rand() * 24;
      n.y = center.y + Math.sin(a) * ring + rand() * 24;
    });
    return;
  }
  const communities = 5;
  nodes.forEach((n, i) => {
    const c = i % communities;
    const cx = 180 + (c % 3) * 360;
    const cy = 200 + Math.floor(c / 3) * 310;
    const a = rand() * Math.PI * 2;
    const r = 30 + rand() * 135;
    n.x = cx + Math.cos(a) * r;
    n.y = cy + Math.sin(a) * r;
  });
}

function filteredGraph() {
  const generated = generateGraph(state.view, state.scale);
  let nodes = generated.nodes;
  let edges = generated.edges;
  if (state.search) {
    const hit = nodes.find((n) => n.label.includes(state.search) || n.id.includes(state.search));
    if (hit) state.focus = hit.id;
  }
  if (state.focus) {
    const keep = new Set([state.focus]);
    edges.forEach((e) => {
      if (e.source === state.focus || e.target === state.focus) {
        keep.add(e.source);
        keep.add(e.target);
      }
    });
    edges = edges.filter((e) => keep.has(e.source) && keep.has(e.target));
    nodes = nodes.filter((n) => keep.has(n.id));
  }
  if (state.clustered && state.scale > 1000 && !state.focus) {
    const visible = nodes.slice(0, 420);
    const groups = new Map();
    nodes.slice(420).forEach((n) => {
      const id = `cluster:${n.kind}`;
      if (!groups.has(id)) groups.set(id, { id, kind: 'cluster', label: `${n.kind} cluster`, x: n.x, y: n.y, weight: 30 });
    });
    const nodeIds = new Set([...visible.map((n) => n.id), ...groups.keys()]);
    const mapped = new Map(nodes.slice(420).map((n) => [n.id, `cluster:${n.kind}`]));
    edges = edges.map((e) => ({ ...e, source: mapped.get(e.source) || e.source, target: mapped.get(e.target) || e.target }))
      .filter((e) => e.source !== e.target && nodeIds.has(e.source) && nodeIds.has(e.target))
      .slice(0, 900);
    nodes = [...visible, ...groups.values()];
  }
  return { nodes, edges };
}

function draw() {
  const start = performance.now();
  const dpr = window.devicePixelRatio || 1;
  const rect = canvas.getBoundingClientRect();
  canvas.width = Math.max(1, Math.floor(rect.width * dpr));
  canvas.height = Math.max(1, Math.floor(rect.height * dpr));
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, rect.width, rect.height);
  const graph = filteredGraph();
  state.current = graph;
  const cam = state.camera;
  const project = (n) => ({ x: (n.x + cam.x) * cam.zoom, y: (n.y + cam.y) * cam.zoom });
  const nodesById = new Map(graph.nodes.map((n) => [n.id, n]));
  const active = state.selected || state.focus;
  const neighborEdges = new Set();
  const neighborNodes = new Set(active ? [active] : []);
  if (active) graph.edges.forEach((e) => {
    if (e.source === active || e.target === active) {
      neighborEdges.add(e.id);
      neighborNodes.add(e.source);
      neighborNodes.add(e.target);
    }
  });
  ctx.lineCap = 'round';
  for (const edge of graph.edges) {
    const a = nodesById.get(edge.source);
    const b = nodesById.get(edge.target);
    if (!a || !b) continue;
    const pa = project(a);
    const pb = project(b);
    if ((pa.x < -80 && pb.x < -80) || (pa.y < -80 && pb.y < -80) || (pa.x > rect.width + 80 && pb.x > rect.width + 80) || (pa.y > rect.height + 80 && pb.y > rect.height + 80)) continue;
    ctx.globalAlpha = active && !neighborEdges.has(edge.id) ? .08 : edge.polarity === 'negative' ? .55 : .28;
    ctx.strokeStyle = edge.polarity === 'negative' ? '#c93d36' : edge.polarity === 'positive' ? '#198754' : '#7a8791';
    ctx.lineWidth = Math.min(8, edge.width * cam.zoom);
    ctx.setLineDash(edge.polarity === 'negative' ? [6, 4] : edge.polarity === 'neutral' ? [2, 4] : []);
    ctx.beginPath();
    ctx.moveTo(pa.x, pa.y);
    ctx.lineTo(pb.x, pb.y);
    ctx.stroke();
  }
  ctx.setLineDash([]);
  for (const node of graph.nodes) {
    const p = project(node);
    if (p.x < -60 || p.y < -60 || p.x > rect.width + 60 || p.y > rect.height + 60) continue;
    const radius = node.kind === 'cluster' ? 18 : 5 + Math.min(10, node.weight || 1);
    ctx.globalAlpha = active && !neighborNodes.has(node.id) ? .18 : 1;
    ctx.fillStyle = colors[node.kind] || '#444';
    ctx.beginPath();
    if (node.kind === 'task' || node.kind === 'plan' || node.kind === 'stage') ctx.roundRect(p.x - radius, p.y - radius, radius * 2, radius * 2, 4);
    else ctx.arc(p.x, p.y, radius, 0, Math.PI * 2);
    ctx.fill();
    if (node.id === state.selected || node.id === state.focus) {
      ctx.strokeStyle = '#111827';
      ctx.lineWidth = 3;
      ctx.stroke();
    }
    const showLabel = !state.lod || cam.zoom > 1.45 || node.kind === 'cluster' || node.weight > 7 || node.id === state.focus;
    if (showLabel) {
      ctx.globalAlpha = active && !neighborNodes.has(node.id) ? .2 : .95;
      ctx.fillStyle = '#172026';
      ctx.font = '12px Inter, sans-serif';
      ctx.fillText(node.label, p.x + radius + 5, p.y + 4);
    }
  }
  ctx.globalAlpha = 1;
  drawMini(graph);
  const drawMs = performance.now() - start;
  state.metrics.push(drawMs);
  if (state.metrics.length > 80) state.metrics.shift();
  publishMetrics(graph, drawMs);
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
    const a = byId.get(e.source);
    const b = byId.get(e.target);
    if (!a || !b) return;
    mini.beginPath();
    mini.moveTo(a.x * sx, a.y * sy);
    mini.lineTo(b.x * sx, b.y * sy);
    mini.stroke();
  });
  mini.globalAlpha = 1;
  graph.nodes.forEach((n) => {
    mini.fillStyle = colors[n.kind] || '#444';
    mini.fillRect(n.x * sx - 1, n.y * sy - 1, 2, 2);
  });
}

function publishMetrics(graph, drawMs) {
  const sorted = [...state.metrics].sort((a, b) => a - b);
  const p95 = sorted[Math.floor(sorted.length * .95)] || drawMs;
  const avg = state.metrics.reduce((a, b) => a + b, 0) / state.metrics.length;
  const budget = state.scale <= 100 ? 16 : state.scale <= 500 ? 24 : 40;
  const data = {
    view: state.view,
    layout: state.layout,
    requested_scale: state.scale,
    rendered_nodes: graph.nodes.length,
    rendered_edges: graph.edges.length,
    first_interactive_ms: Math.round(performance.now()),
    draw_ms_last: Number(drawMs.toFixed(2)),
    draw_ms_avg: Number(avg.toFixed(2)),
    draw_ms_p95: Number(p95.toFixed(2)),
    estimated_fps_p95: Math.max(1, Math.round(1000 / Math.max(16.7, p95))),
    js_heap_mb: performance.memory ? Math.round(performance.memory.usedJSHeapSize / 1048576) : null,
    budget_ms_per_frame: budget,
    pass: p95 <= budget,
  };
  document.getElementById('metrics').textContent = JSON.stringify(data, null, 2);
  document.getElementById('hud').textContent = `${graph.nodes.length} nodes | ${graph.edges.length} edges | p95 ${data.draw_ms_p95} ms`;
}

function setView(view) {
  state.view = view;
  document.querySelectorAll('.view-tabs button').forEach((button) => button.classList.toggle('active', button.dataset.view === view));
  document.getElementById('view-question').textContent = views[view].question;
  const layout = document.getElementById('layout');
  layout.innerHTML = views[view].layouts.map((name) => `<option>${name}</option>`).join('');
  state.layout = views[view].layouts[0];
  state.focus = null;
  state.selected = null;
  state.camera = { x: 0, y: 0, zoom: 1 };
  requestAnimationFrame(draw);
}

document.querySelectorAll('.view-tabs button').forEach((button) => button.addEventListener('click', () => setView(button.dataset.view)));
document.getElementById('scale').addEventListener('change', (e) => { state.scale = Number(e.target.value); requestAnimationFrame(draw); });
document.getElementById('layout').addEventListener('change', (e) => { state.layout = e.target.value; requestAnimationFrame(draw); });
document.getElementById('clusters').addEventListener('change', (e) => { state.clustered = e.target.checked; requestAnimationFrame(draw); });
document.getElementById('lod').addEventListener('change', (e) => { state.lod = e.target.checked; requestAnimationFrame(draw); });
document.getElementById('search').addEventListener('input', (e) => { state.search = e.target.value.trim(); requestAnimationFrame(draw); });
document.getElementById('clear').addEventListener('click', () => {
  document.getElementById('project').value = '';
  document.getElementById('agent').value = '';
  state.focus = null;
  state.selected = null;
  requestAnimationFrame(draw);
});
document.getElementById('restore').addEventListener('click', () => { state.focus = null; state.selected = null; state.camera = { x: 0, y: 0, zoom: 1 }; requestAnimationFrame(draw); });
document.getElementById('focus').addEventListener('click', () => { state.focus = state.current?.nodes[0]?.id || null; requestAnimationFrame(draw); });
document.getElementById('expand').addEventListener('click', () => { state.focus = null; state.scale = Math.min(2200, state.scale === 100 ? 500 : 2200); document.getElementById('scale').value = String(state.scale); requestAnimationFrame(draw); });
document.getElementById('collapse').addEventListener('click', () => { state.scale = Math.max(100, state.scale === 2200 ? 500 : 100); document.getElementById('scale').value = String(state.scale); requestAnimationFrame(draw); });
document.getElementById('evidence').addEventListener('click', () => {
  const node = state.focus || state.selected || state.current?.nodes[0]?.id || 'agent:0';
  document.getElementById('evidence-json').textContent = JSON.stringify({
    selected: node,
    effect_scopes: [
      { effect_id: 'ce_01HZX4_demo', project_id: 'Alpha' },
      { effect_id: 'ce_01HZX5_demo', project_id: 'Runtime' },
    ],
    evidence_event_ids: ['evt_dependency_added', 'evt_task_completed'],
  }, null, 2);
  document.getElementById('evidence-dialog').showModal();
});

canvas.addEventListener('wheel', (event) => {
  event.preventDefault();
  state.camera.zoom = Math.max(.35, Math.min(2.4, state.camera.zoom * (event.deltaY > 0 ? .9 : 1.1)));
  requestAnimationFrame(draw);
}, { passive: false });
canvas.addEventListener('pointerdown', (event) => {
  canvas.setPointerCapture(event.pointerId);
  state.dragging = { x: event.clientX, y: event.clientY, camera: { ...state.camera } };
});
canvas.addEventListener('pointermove', (event) => {
  if (!state.dragging) return;
  state.camera.x = state.dragging.camera.x + (event.clientX - state.dragging.x) / state.camera.zoom;
  state.camera.y = state.dragging.camera.y + (event.clientY - state.dragging.y) / state.camera.zoom;
  requestAnimationFrame(draw);
});
canvas.addEventListener('pointerup', (event) => {
  state.dragging = null;
  canvas.releasePointerCapture(event.pointerId);
});
canvas.addEventListener('keydown', (event) => {
  if (event.key === 'Enter') document.getElementById('evidence').click();
  if (event.key === 'Escape') document.getElementById('restore').click();
  if (event.key === '+') { state.camera.zoom *= 1.1; requestAnimationFrame(draw); }
  if (event.key === '-') { state.camera.zoom *= .9; requestAnimationFrame(draw); }
});

const initialParams = new URLSearchParams(location.search);
const initialScale = Number(initialParams.get('scale'));
if (initialScale) {
  state.scale = initialScale;
  document.getElementById('scale').value = String(initialScale);
}
setView(initialParams.get('view') || 'network');
requestAnimationFrame(draw);

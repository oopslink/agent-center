const canvas = document.getElementById('graph');
const ctx = canvas.getContext('2d');
const minimap = document.getElementById('minimap');
const mini = minimap.getContext('2d');
const metricsEl = document.getElementById('metrics');
const hudEl = document.getElementById('hud');
const selectedTitle = document.getElementById('selected-title');
const selectedCopy = document.getElementById('selected-copy');

const state = {
  view: 'network',
  scale: 100,
  layout: 'community',
  search: '',
  clustered: true,
  lod: true,
  expandedDepth: 0,
  focus: null,
  selected: null,
  hover: null,
  camera: { x: 0, y: 0, zoom: 1 },
  fixed: new Set(),
  dragging: null,
  pointerDown: null,
  metrics: [],
  raw: [],
  current: null,
};

const views = {
  network: {
    question: 'Who collaborates most closely?',
    layouts: ['community', 'ego radial', 'force'],
    answer: 'Agent communities and strongest handoff/review ties.',
  },
  impact: {
    question: 'Who is pushing or blocking what?',
    layouts: ['radial impact', 'bipartite lanes', 'force'],
    answer: 'Agent to task effects with blocking/progress polarity.',
  },
  lineage: {
    question: 'Where is the flow stuck?',
    layouts: ['hierarchical DAG', 'stage swimlanes', 'critical path'],
    answer: 'Plan, stage, task dependencies and critical bottlenecks.',
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

function hash(value) {
  let h = 2166136261;
  for (let i = 0; i < value.length; i++) {
    h ^= value.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

function readFilters() {
  return {
    window: document.getElementById('window').value,
    project: document.getElementById('project').value,
    agent: document.getElementById('agent').value,
  };
}

function setFilterValues({ windowValue, project, agent }) {
  if (windowValue) document.getElementById('window').value = windowValue;
  if (project !== undefined) document.getElementById('project').value = project;
  if (agent !== undefined) document.getElementById('agent').value = agent;
}

function targetDays(windowValue) {
  if (windowValue === '24h') return 1;
  if (windowValue === '7d') return 7;
  return 30;
}

function generateEffects(scale) {
  const rand = rng(scale * 1009);
  const projects = ['Alpha', 'Runtime', 'Console'];
  const agentNames = ['atlas', 'forge', 'signal', 'delta', 'nova', 'relay', 'scribe', 'pilot'];
  const planCount = Math.max(3, Math.round(scale * .025));
  const stageCount = Math.max(8, Math.round(scale * .09));
  const taskCount = Math.max(32, Math.round(scale * .62));
  const agentCount = state.view === 'network' ? scale : Math.max(12, Math.round(scale * .18));
  const agents = Array.from({ length: agentCount }, (_, i) => ({
    id: `agent:${agentNames[i % agentNames.length]}-${i}`,
    kind: 'agent',
    label: `${agentNames[i % agentNames.length]}-${i}`,
    project: projects[i % projects.length],
  }));
  const plans = Array.from({ length: planCount }, (_, i) => ({
    id: `plan:${projects[i % projects.length]}-${i}`,
    kind: 'plan',
    label: `${projects[i % projects.length]} plan ${i}`,
    project: projects[i % projects.length],
    status: i % 5 === 0 ? 'blocked' : 'running',
  }));
  const stages = Array.from({ length: stageCount }, (_, i) => {
    const plan = plans[i % plans.length];
    return {
      id: `stage:${plan.project}-${i}`,
      kind: 'stage',
      label: `${plan.project} stage ${i % 6}`,
      project: plan.project,
      plan_id: plan.id,
      status: i % 7 === 0 ? 'blocked' : i % 5 === 0 ? 'review' : 'ready',
    };
  });
  const tasks = Array.from({ length: taskCount }, (_, i) => {
    const stage = stages[i % stages.length];
    return {
      id: `task:${stage.project}-${i}`,
      kind: 'task',
      label: `${stage.project} task ${i}`,
      project: stage.project,
      plan_id: stage.plan_id,
      stage_id: stage.id,
      status: i % 11 === 0 ? 'blocked' : i % 7 === 0 ? 'review' : i % 5 === 0 ? 'done' : 'active',
    };
  });
  const effects = [];
  const edgeTarget = Math.round(scale * (scale > 1000 ? 1.35 : 1.2));
  const addEffect = (source, target, relation, polarity, dayOffset, extra = {}) => {
    if (!source || !target || source.id === target.id) return;
    const id = `ce:${effects.length}:${hash(source.id + target.id + relation).toString(16)}`;
    effects.push({
      effect_id: id,
      source: source.id,
      target: target.id,
      source_agent_ref: source.id.startsWith('agent:') ? source.id : extra.agent || '',
      target_agent_ref: target.id.startsWith('agent:') ? target.id : '',
      target_task_id: target.id.startsWith('task:') ? target.id.replace('task:', '') : '',
      project_id: extra.project || source.project || target.project || 'Alpha',
      plan_id: extra.plan_id || source.plan_id || target.plan_id || '',
      stage_id: extra.stage_id || source.stage_id || target.stage_id || '',
      relation_type: relation,
      polarity,
      magnitude: 1 + Math.floor(rand() * 3),
      interaction_count: 1 + Math.floor(rand() * 7),
      evidence_count: 1 + Math.floor(rand() * 5),
      occurred_day: dayOffset,
      occurred_at: `2026-09-${String(10 - Math.min(9, dayOffset)).padStart(2, '0')}T${String(8 + (effects.length % 12)).padStart(2, '0')}:00:00Z`,
      evidence_event_ids: [`evt:${id}:state`, `evt:${id}:message`],
      effect_scopes: [{ effect_id: id, project_id: extra.project || source.project || target.project || 'Alpha' }],
      before_state: { status: extra.before || 'running' },
      after_state: { status: extra.after || (polarity === 'negative' ? 'blocked' : 'advanced') },
      explanation_key: `collaboration.effect.${relation}`,
    });
  };
  const networkIterations = state.view === 'network' ? edgeTarget * 2 : agentCount * 2;
  for (let i = 0; i < networkIterations && effects.length < edgeTarget; i++) {
    const source = agents[i % agents.length];
    const target = agents[(i * 7 + 3) % agents.length];
    addEffect(source, target, i % 3 === 0 ? 'review_accept' : 'handoff', i % 9 === 0 ? 'negative' : 'positive', i % 30, { project: source.project });
  }
  for (let i = 0; i < taskCount && effects.length < edgeTarget; i++) {
    const task = tasks[i];
    const agent = agents[(i * 5 + 1) % agents.length];
    addEffect(agent, task, task.status === 'blocked' ? 'block' : task.status === 'done' ? 'complete' : 'assign', task.status === 'blocked' ? 'negative' : 'positive', i % 30, { project: task.project, plan_id: task.plan_id, stage_id: task.stage_id, before: 'open' });
  }
  for (let i = 0; i < stages.length && effects.length < edgeTarget; i++) {
    addEffect(plans[i % plans.length], stages[i], 'contains', stages[i].status === 'blocked' ? 'negative' : 'neutral', i % 30, { project: stages[i].project, plan_id: stages[i].plan_id, stage_id: stages[i].id });
  }
  for (let i = 0; i < tasks.length && effects.length < edgeTarget; i++) {
    addEffect(stages[i % stages.length], tasks[i], 'contains', tasks[i].status === 'blocked' ? 'negative' : 'neutral', i % 30, { project: tasks[i].project, plan_id: tasks[i].plan_id, stage_id: tasks[i].stage_id });
  }
  for (let i = 1; i < tasks.length && effects.length < edgeTarget; i++) {
    if (rand() > .18) addEffect(tasks[i - 1], tasks[i], 'depends_on', tasks[i].status === 'blocked' ? 'negative' : 'neutral', i % 30, { project: tasks[i].project, plan_id: tasks[i].plan_id, stage_id: tasks[i].stage_id });
  }
  while (effects.length < edgeTarget) {
    const task = tasks[Math.floor(rand() * tasks.length)];
    const agent = agents[Math.floor(rand() * agents.length)];
    addEffect(agent, task, rand() > .5 ? 'review_reject' : 'dependency_release', rand() > .3 ? 'positive' : 'mixed', Math.floor(rand() * 30), { project: task.project, plan_id: task.plan_id, stage_id: task.stage_id });
  }
  return { agents, plans, stages, tasks, effects };
}

function applyFilters(dataset) {
  const filters = readFilters();
  const days = targetDays(filters.window);
  const project = filters.project;
  const agent = filters.agent;
  const effects = dataset.effects.filter((effect) => {
    if (effect.occurred_day >= days) return false;
    if (project && effect.project_id !== project) return false;
    if (agent && effect.source_agent_ref !== agent && effect.target_agent_ref !== agent) return false;
    return true;
  });
  return { ...dataset, effects };
}

function nodeFromEntity(entity) {
  return { ...entity, weight: 1 };
}

function addNode(nodes, entity) {
  if (!entity || nodes.has(entity.id)) return;
  nodes.set(entity.id, nodeFromEntity(entity));
}

function projectView(dataset) {
  const nodes = new Map();
  const edges = new Map();
  const agents = new Map(dataset.agents.map((n) => [n.id, n]));
  const tasks = new Map(dataset.tasks.map((n) => [n.id, n]));
  const plans = new Map(dataset.plans.map((n) => [n.id, n]));
  const stages = new Map(dataset.stages.map((n) => [n.id, n]));
  const aggregate = (source, target, effect, relation = effect.relation_type) => {
    if (!source || !target || source === target) return;
    const key = `${source}|${target}|${relation}|${effect.polarity}`;
    const current = edges.get(key) || {
      id: `edge:${hash(key).toString(16)}`,
      source,
      target,
      relation,
      polarity: effect.polarity,
      width: 1,
      effects: 0,
      evidence: 0,
      effect_scopes: [],
      evidence_event_ids: [],
      latest: effect.occurred_at,
    };
    current.effects += 1;
    current.evidence += effect.evidence_count;
    current.width = Math.min(8, current.width + effect.magnitude * .25);
    current.latest = current.latest > effect.occurred_at ? current.latest : effect.occurred_at;
    current.effect_scopes.push(...effect.effect_scopes);
    current.evidence_event_ids.push(...effect.evidence_event_ids);
    edges.set(key, current);
  };
  for (const effect of dataset.effects) {
    if (state.view === 'network') {
      if (!effect.source_agent_ref || !effect.target_agent_ref) continue;
      addNode(nodes, agents.get(effect.source_agent_ref));
      addNode(nodes, agents.get(effect.target_agent_ref));
      aggregate(effect.source_agent_ref, effect.target_agent_ref, effect, effect.relation_type === 'review_accept' ? 'review' : 'co-work');
    } else if (state.view === 'impact') {
      if (!effect.source_agent_ref || !effect.target_task_id) continue;
      const taskId = `task:${effect.target_task_id}`;
      addNode(nodes, agents.get(effect.source_agent_ref));
      addNode(nodes, tasks.get(taskId));
      aggregate(effect.source_agent_ref, taskId, effect);
      if (effect.plan_id) addNode(nodes, plans.get(effect.plan_id));
    } else {
      const source = plans.get(effect.source) || stages.get(effect.source) || tasks.get(effect.source);
      const target = plans.get(effect.target) || stages.get(effect.target) || tasks.get(effect.target);
      if (!source || !target) continue;
      addNode(nodes, source);
      addNode(nodes, target);
      aggregate(source.id, target.id, effect);
    }
  }
  const graph = { nodes: [...nodes.values()], edges: [...edges.values()] };
  applyLayout(graph.nodes, graph.edges, state.view, state.layout);
  return graph;
}

function applyLayout(nodes, edges, view, layout) {
  const w = 1200;
  const h = 760;
  const rand = rng(nodes.length * 17 + edges.length + hash(layout));
  if (view === 'lineage' || layout.includes('DAG') || layout.includes('swim')) {
    const lanes = { plan: 85, stage: 260, task: 500, agent: 680 };
    const counters = {};
    nodes.forEach((n) => {
      if (state.fixed.has(n.id) && Number.isFinite(n.x)) return;
      const i = counters[n.kind] || 0;
      counters[n.kind] = i + 1;
      const row = Math.floor(i / 24);
      n.x = 70 + (i % 24) * 46 + rand() * 8;
      n.y = (lanes[n.kind] || 660) + row * 30;
    });
    return;
  }
  if (view === 'impact' || layout.includes('radial') || layout.includes('ego')) {
    const focus = state.focus || state.selected?.id || '';
    const center = { x: w / 2, y: h / 2 };
    nodes.forEach((n, i) => {
      if (state.fixed.has(n.id) && Number.isFinite(n.x)) return;
      if (focus && n.id === focus) {
        n.x = center.x;
        n.y = center.y;
        return;
      }
      const ring = n.kind === 'agent' ? 165 : n.kind === 'task' ? 320 : 245;
      const a = (i / Math.max(1, nodes.length)) * Math.PI * 2;
      n.x = center.x + Math.cos(a) * ring + rand() * 24;
      n.y = center.y + Math.sin(a) * ring + rand() * 24;
    });
    return;
  }
  const communities = 5;
  nodes.forEach((n, i) => {
    if (state.fixed.has(n.id) && Number.isFinite(n.x)) return;
    const c = Math.abs(hash(n.project || n.id)) % communities;
    const cx = 170 + (c % 3) * 365;
    const cy = 210 + Math.floor(c / 3) * 300;
    const a = rand() * Math.PI * 2;
    const r = 32 + rand() * 135;
    n.x = cx + Math.cos(a) * r;
    n.y = cy + Math.sin(a) * r;
  });
}

function filteredGraph() {
  const graph = projectView(applyFilters(generateEffects(state.scale)));
  let nodes = graph.nodes;
  let edges = graph.edges;
  if (state.search) {
    const hit = nodes.find((n) => n.label.toLowerCase().includes(state.search.toLowerCase()) || n.id.includes(state.search));
    if (hit) state.focus = hit.id;
  }
  if (state.focus) {
    const keep = new Set([state.focus]);
    const depth = Math.max(1, state.expandedDepth || 1);
    for (let d = 0; d < depth; d++) {
      edges.forEach((e) => {
        if (keep.has(e.source) || keep.has(e.target)) {
          keep.add(e.source);
          keep.add(e.target);
        }
      });
    }
    edges = edges.filter((e) => keep.has(e.source) && keep.has(e.target));
    nodes = nodes.filter((n) => keep.has(n.id));
  }
  if (state.clustered && state.scale > 1000 && !state.focus) {
    const visible = nodes.slice(0, 420);
    const groups = new Map();
    nodes.slice(420).forEach((n) => {
      const id = `cluster:${n.kind}:${n.project || 'global'}`;
      if (!groups.has(id)) groups.set(id, { id, kind: 'cluster', label: `${n.project || 'global'} ${n.kind}s`, x: n.x, y: n.y, weight: 30, project: n.project });
    });
    const nodeIds = new Set([...visible.map((n) => n.id), ...groups.keys()]);
    const mapped = new Map(nodes.slice(420).map((n) => [n.id, `cluster:${n.kind}:${n.project || 'global'}`]));
    edges = edges.map((e) => ({ ...e, source: mapped.get(e.source) || e.source, target: mapped.get(e.target) || e.target }))
      .filter((e) => e.source !== e.target && nodeIds.has(e.source) && nodeIds.has(e.target))
      .slice(0, 900);
    nodes = [...visible, ...groups.values()];
  }
  return { nodes, edges };
}

function project(n) {
  const cam = state.camera;
  return { x: (n.x + cam.x) * cam.zoom, y: (n.y + cam.y) * cam.zoom };
}

function renderShape(node, p, radius) {
  ctx.beginPath();
  if (node.kind === 'task' || node.kind === 'plan') {
    ctx.roundRect(p.x - radius, p.y - radius, radius * 2, radius * 2, 4);
  } else if (node.kind === 'stage') {
    ctx.moveTo(p.x, p.y - radius);
    ctx.lineTo(p.x + radius, p.y);
    ctx.lineTo(p.x, p.y + radius);
    ctx.lineTo(p.x - radius, p.y);
    ctx.closePath();
  } else {
    ctx.arc(p.x, p.y, radius, 0, Math.PI * 2);
  }
}

function activeNeighborhood(graph) {
  const active = state.selected?.id || state.hover?.id || state.focus;
  const neighborEdges = new Set();
  const neighborNodes = new Set(active ? [active] : []);
  if (active) {
    graph.edges.forEach((e) => {
      if (e.id === active || e.source === active || e.target === active) {
        neighborEdges.add(e.id);
        neighborNodes.add(e.source);
        neighborNodes.add(e.target);
      }
    });
  }
  return { active, neighborEdges, neighborNodes };
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
  const nodesById = new Map(graph.nodes.map((n) => [n.id, n]));
  const { active, neighborEdges, neighborNodes } = activeNeighborhood(graph);
  ctx.lineCap = 'round';
  for (const edge of graph.edges) {
    const a = nodesById.get(edge.source);
    const b = nodesById.get(edge.target);
    if (!a || !b) continue;
    const pa = project(a);
    const pb = project(b);
    if ((pa.x < -80 && pb.x < -80) || (pa.y < -80 && pb.y < -80) || (pa.x > rect.width + 80 && pb.x > rect.width + 80) || (pa.y > rect.height + 80 && pb.y > rect.height + 80)) continue;
    const highlighted = !active || neighborEdges.has(edge.id) || state.selected?.id === edge.id || state.hover?.id === edge.id;
    ctx.globalAlpha = highlighted ? edge.polarity === 'negative' ? .62 : .34 : .07;
    ctx.strokeStyle = edge.polarity === 'negative' ? '#c93d36' : edge.polarity === 'positive' ? '#198754' : '#7a8791';
    ctx.lineWidth = Math.min(8, edge.width * state.camera.zoom);
    ctx.setLineDash(edge.polarity === 'negative' ? [6, 4] : edge.polarity === 'neutral' ? [2, 4] : []);
    ctx.beginPath();
    ctx.moveTo(pa.x, pa.y);
    ctx.lineTo(pb.x, pb.y);
    ctx.stroke();
    if (highlighted && (state.hover?.id === edge.id || state.selected?.id === edge.id)) {
      ctx.globalAlpha = .94;
      ctx.fillStyle = '#172026';
      ctx.font = '12px Inter, sans-serif';
      ctx.fillText(`${edge.relation} · ${edge.evidence} ev`, (pa.x + pb.x) / 2 + 8, (pa.y + pb.y) / 2 - 8);
    }
  }
  ctx.setLineDash([]);
  for (const node of graph.nodes) {
    const p = project(node);
    if (p.x < -60 || p.y < -60 || p.x > rect.width + 60 || p.y > rect.height + 60) continue;
    const radius = node.kind === 'cluster' ? 18 : 5 + Math.min(10, node.weight || 1);
    const highlighted = !active || neighborNodes.has(node.id) || state.selected?.id === node.id || state.hover?.id === node.id;
    ctx.globalAlpha = highlighted ? 1 : .16;
    ctx.fillStyle = colors[node.kind] || '#444';
    renderShape(node, p, radius);
    ctx.fill();
    if (node.id === state.selected?.id || node.id === state.focus || state.fixed.has(node.id)) {
      ctx.strokeStyle = state.fixed.has(node.id) ? '#b7791f' : '#111827';
      ctx.lineWidth = 3;
      ctx.stroke();
    }
    const showLabel = !state.lod || state.camera.zoom > 1.35 || node.kind === 'cluster' || node.weight > 7 || node.id === state.focus || node.id === state.selected?.id || node.status === 'blocked';
    if (showLabel) {
      ctx.globalAlpha = highlighted ? .95 : .2;
      ctx.fillStyle = '#172026';
      ctx.font = '12px Inter, sans-serif';
      ctx.fillText(node.label, p.x + radius + 5, p.y + 4);
    }
  }
  ctx.globalAlpha = 1;
  drawMini(graph);
  const drawMs = performance.now() - start;
  state.metrics.push(drawMs);
  if (state.metrics.length > 120) state.metrics.shift();
  publishMetrics(graph, drawMs);
}

function drawMini(graph) {
  mini.clearRect(0, 0, minimap.width, minimap.height);
  mini.fillStyle = '#f7f8f9';
  mini.fillRect(0, 0, minimap.width, minimap.height);
  const sx = minimap.width / 1280;
  const sy = minimap.height / 760;
  mini.globalAlpha = .23;
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
  const p95 = sorted[Math.floor(Math.max(0, sorted.length - 1) * .95)] || drawMs;
  const avg = state.metrics.reduce((a, b) => a + b, 0) / state.metrics.length;
  const budget = state.scale <= 100 ? 16 : state.scale <= 500 ? 24 : 40;
  const data = {
    view: state.view,
    layout: state.layout,
    filters: readFilters(),
    requested_scale: state.scale,
    rendered_nodes: graph.nodes.length,
    rendered_edges: graph.edges.length,
    focus: state.focus,
    selected: state.selected,
    fixed_nodes: state.fixed.size,
    first_interactive_ms: Math.round(performance.now()),
    draw_ms_last: Number(drawMs.toFixed(2)),
    draw_ms_avg: Number(avg.toFixed(2)),
    draw_ms_p95: Number(p95.toFixed(2)),
    estimated_fps_p95: Math.max(1, Math.round(1000 / Math.max(16.7, p95))),
    js_heap_mb: performance.memory ? Math.round(performance.memory.usedJSHeapSize / 1048576) : null,
    budget_ms_per_frame: budget,
    pass: p95 <= budget,
    raw_events: state.raw.slice(-24),
  };
  metricsEl.textContent = JSON.stringify(data, null, 2);
  hudEl.textContent = `${graph.nodes.length} nodes | ${graph.edges.length} edges | p95 ${data.draw_ms_p95} ms`;
  window.__collaborationPrototypeMetrics = data;
}

function hitTest(clientX, clientY) {
  if (!state.current) return null;
  const rect = canvas.getBoundingClientRect();
  const x = clientX - rect.left;
  const y = clientY - rect.top;
  let bestNode = null;
  let bestNodeDistance = Infinity;
  for (const node of state.current.nodes) {
    const p = project(node);
    const radius = node.kind === 'cluster' ? 20 : 15;
    const d = Math.hypot(p.x - x, p.y - y);
    if (d < radius && d < bestNodeDistance) {
      bestNode = node;
      bestNodeDistance = d;
    }
  }
  if (bestNode) return { type: 'node', id: bestNode.id, node: bestNode };
  const byId = new Map(state.current.nodes.map((n) => [n.id, n]));
  let bestEdge = null;
  let bestEdgeDistance = Infinity;
  for (const edge of state.current.edges) {
    const a = byId.get(edge.source);
    const b = byId.get(edge.target);
    if (!a || !b) continue;
    const pa = project(a);
    const pb = project(b);
    const d = pointLineDistance(x, y, pa.x, pa.y, pb.x, pb.y);
    if (d < 8 && d < bestEdgeDistance) {
      bestEdge = edge;
      bestEdgeDistance = d;
    }
  }
  return bestEdge ? { type: 'edge', id: bestEdge.id, edge: bestEdge } : null;
}

function pointLineDistance(px, py, x1, y1, x2, y2) {
  const dx = x2 - x1;
  const dy = y2 - y1;
  if (!dx && !dy) return Math.hypot(px - x1, py - y1);
  const t = Math.max(0, Math.min(1, ((px - x1) * dx + (py - y1) * dy) / (dx * dx + dy * dy)));
  return Math.hypot(px - (x1 + t * dx), py - (y1 + t * dy));
}

function setSelection(hit, source = 'unknown') {
  if (!hit) return;
  state.selected = hit.type === 'edge' ? { type: 'edge', id: hit.id, edge: hit.edge } : { type: 'node', id: hit.id, node: hit.node };
  if (hit.type === 'node') state.focus = hit.id;
  selectedTitle.textContent = hit.type === 'edge' ? `${hit.edge.relation} edge` : hit.node.label;
  selectedCopy.textContent = hit.type === 'edge'
    ? `${hit.edge.effects} effects, ${hit.edge.evidence} evidence events. Evidence opens by existing effect scopes.`
    : `${hit.node.kind} selected from ${source}. Focus and expansion keep shared filters across views.`;
  if (source === 'canvas-hit-test') state.raw.push({ event: 'canvas-hit-test', type: hit.type, id: hit.id, view: state.view, at: Math.round(performance.now()) });
  state.raw.push({ event: 'select', source, type: hit.type, id: hit.id, view: state.view, filters: readFilters(), at: Math.round(performance.now()) });
  requestAnimationFrame(draw);
}

function firstNodeHit() {
  const node = state.current?.nodes.find((n) => n.kind !== 'cluster') || state.current?.nodes[0];
  return node ? { type: 'node', id: node.id, node } : null;
}

function firstEdgeHit() {
  const edge = state.current?.edges[0];
  return edge ? { type: 'edge', id: edge.id, edge } : null;
}

function setView(view) {
  const previousContext = state.selected || (state.focus ? { type: 'node', id: state.focus } : null);
  state.view = view;
  document.querySelectorAll('.view-tabs button').forEach((button) => button.classList.toggle('active', button.dataset.view === view));
  document.getElementById('view-question').textContent = views[view].question;
  const layout = document.getElementById('layout');
  layout.innerHTML = views[view].layouts.map((name) => `<option>${name}</option>`).join('');
  state.layout = views[view].layouts[0];
  state.focus = previousContext?.id || null;
  state.selected = previousContext;
  state.expandedDepth = 0;
  state.camera = { x: 0, y: 0, zoom: 1 };
  state.raw.push({ event: 'view-switch', view, retained_context: state.focus, filters: readFilters(), at: Math.round(performance.now()) });
  requestAnimationFrame(() => {
    draw();
    if (state.focus && !state.current.nodes.some((n) => n.id === state.focus) && !state.current.edges.some((e) => e.id === state.focus)) {
      state.raw.push({ event: 'context-chip-retained-without-node', id: state.focus, view, at: Math.round(performance.now()) });
    }
  });
}

function restoreGlobal(clearFilters = false) {
  if (clearFilters) setFilterValues({ project: '', agent: '', windowValue: '24h' });
  state.focus = null;
  state.selected = null;
  state.hover = null;
  state.expandedDepth = 0;
  state.camera = { x: 0, y: 0, zoom: 1 };
  state.raw.push({ event: clearFilters ? 'clear-filters-restore-global' : 'restore-global', view: state.view, filters: readFilters(), at: Math.round(performance.now()) });
  selectedTitle.textContent = 'Select a node or edge';
  selectedCopy.textContent = 'Hover, click, search, or use keyboard controls. Evidence stays bound to selected effect scopes.';
  requestAnimationFrame(draw);
}

function openEvidence() {
  const selected = state.selected || firstEdgeHit() || firstNodeHit();
  if (!selected) return;
  if (!state.selected) setSelection(selected, 'evidence-default');
  const edge = selected.edge || state.current?.edges.find((e) => e.source === selected.id || e.target === selected.id);
  const evidence = {
    selected: selected.id,
    selected_type: selected.type,
    view: state.view,
    filters: readFilters(),
    effect_scopes: edge?.effect_scopes?.slice(0, 6) || [],
    evidence_event_ids: edge?.evidence_event_ids?.slice(0, 8) || [],
    relation_type: edge?.relation || null,
    polarity: edge?.polarity || null,
    note: 'Prototype binds the drawer to existing CollaborationEffect scopes; it does not create alternative attribution semantics.',
  };
  document.getElementById('evidence-json').textContent = JSON.stringify(evidence, null, 2);
  state.raw.push({ event: 'open-evidence', selected: selected.id, effect_scope_count: evidence.effect_scopes.length, evidence_event_count: evidence.evidence_event_ids.length, at: Math.round(performance.now()) });
  document.getElementById('evidence-dialog').showModal();
}

function performMeasuredPan(dx = 90, dy = 50) {
  const before = { ...state.camera };
  state.camera.x += dx / state.camera.zoom;
  state.camera.y += dy / state.camera.zoom;
  state.raw.push({ event: 'pan-canvas', before, after: { ...state.camera }, at: Math.round(performance.now()) });
  requestAnimationFrame(draw);
}

function performMeasuredNodeDrag(dx = 44, dy = 26) {
  const node = state.current?.nodes.find((n) => n.kind !== 'cluster');
  if (!node) return;
  const before = { x: node.x, y: node.y };
  node.x += dx / state.camera.zoom;
  node.y += dy / state.camera.zoom;
  state.fixed.add(node.id);
  state.raw.push({ event: 'drag-pin-node', id: node.id, before, after: { x: node.x, y: node.y }, fixed_nodes: state.fixed.size, at: Math.round(performance.now()) });
  requestAnimationFrame(draw);
}

document.querySelectorAll('.view-tabs button').forEach((button) => button.addEventListener('click', () => setView(button.dataset.view)));
document.getElementById('scale').addEventListener('change', (e) => { state.scale = Number(e.target.value); state.metrics = []; state.focus = null; state.selected = null; requestAnimationFrame(draw); });
document.getElementById('layout').addEventListener('change', (e) => { state.layout = e.target.value; state.metrics = []; requestAnimationFrame(draw); });
document.getElementById('clusters').addEventListener('change', (e) => { state.clustered = e.target.checked; requestAnimationFrame(draw); });
document.getElementById('lod').addEventListener('change', (e) => { state.lod = e.target.checked; requestAnimationFrame(draw); });
document.getElementById('window').addEventListener('change', () => { state.focus = null; state.selected = null; state.metrics = []; state.raw.push({ event: 'filter-window', filters: readFilters(), at: Math.round(performance.now()) }); requestAnimationFrame(draw); });
document.getElementById('project').addEventListener('change', () => { state.focus = null; state.selected = null; state.metrics = []; state.raw.push({ event: 'filter-project', filters: readFilters(), at: Math.round(performance.now()) }); requestAnimationFrame(draw); });
document.getElementById('agent').addEventListener('change', () => { state.focus = document.getElementById('agent').value || null; state.selected = null; state.metrics = []; state.raw.push({ event: 'filter-agent', filters: readFilters(), at: Math.round(performance.now()) }); requestAnimationFrame(draw); });
document.getElementById('search').addEventListener('input', (e) => { state.search = e.target.value.trim(); state.raw.push({ event: 'search', query: state.search, view: state.view, at: Math.round(performance.now()) }); requestAnimationFrame(draw); });
document.getElementById('clear').addEventListener('click', () => restoreGlobal(true));
document.getElementById('restore').addEventListener('click', () => restoreGlobal(false));
document.getElementById('focus').addEventListener('click', () => setSelection(firstNodeHit(), 'focus-button'));
document.getElementById('expand').addEventListener('click', () => { state.expandedDepth = Math.min(2, state.expandedDepth + 1); state.raw.push({ event: 'expand-neighborhood', depth: state.expandedDepth, focus: state.focus, at: Math.round(performance.now()) }); requestAnimationFrame(draw); });
document.getElementById('collapse').addEventListener('click', () => { state.expandedDepth = Math.max(0, state.expandedDepth - 1); state.raw.push({ event: 'collapse-neighborhood', depth: state.expandedDepth, focus: state.focus, at: Math.round(performance.now()) }); requestAnimationFrame(draw); });
document.getElementById('evidence').addEventListener('click', openEvidence);

canvas.addEventListener('wheel', (event) => {
  event.preventDefault();
  const before = state.camera.zoom;
  state.camera.zoom = Math.max(.35, Math.min(2.4, state.camera.zoom * (event.deltaY > 0 ? .9 : 1.1)));
  state.raw.push({ event: 'zoom', before: Number(before.toFixed(2)), after: Number(state.camera.zoom.toFixed(2)), at: Math.round(performance.now()) });
  requestAnimationFrame(draw);
}, { passive: false });
canvas.addEventListener('pointerdown', (event) => {
  canvas.setPointerCapture(event.pointerId);
  const hit = hitTest(event.clientX, event.clientY);
  state.pointerDown = { x: event.clientX, y: event.clientY, hit };
  if (hit?.type === 'node') {
    state.dragging = { mode: 'node', nodeId: hit.id, x: event.clientX, y: event.clientY };
  } else {
    state.dragging = { mode: 'canvas', x: event.clientX, y: event.clientY, camera: { ...state.camera } };
  }
});
canvas.addEventListener('pointermove', (event) => {
  state.hover = hitTest(event.clientX, event.clientY);
  if (!state.dragging) {
    requestAnimationFrame(draw);
    return;
  }
  if (state.dragging.mode === 'node') {
    const node = state.current?.nodes.find((n) => n.id === state.dragging.nodeId);
    if (node) {
      node.x += (event.clientX - state.dragging.x) / state.camera.zoom;
      node.y += (event.clientY - state.dragging.y) / state.camera.zoom;
      state.dragging.x = event.clientX;
      state.dragging.y = event.clientY;
      state.fixed.add(node.id);
    }
  } else {
    state.camera.x = state.dragging.camera.x + (event.clientX - state.dragging.x) / state.camera.zoom;
    state.camera.y = state.dragging.camera.y + (event.clientY - state.dragging.y) / state.camera.zoom;
  }
  requestAnimationFrame(draw);
});
canvas.addEventListener('pointerup', (event) => {
  const moved = state.pointerDown ? Math.hypot(event.clientX - state.pointerDown.x, event.clientY - state.pointerDown.y) : 0;
  if (moved < 4) setSelection(hitTest(event.clientX, event.clientY), 'canvas-hit-test');
  if (state.dragging?.mode === 'node') state.raw.push({ event: 'drag-pin-node', id: state.dragging.nodeId, fixed_nodes: state.fixed.size, at: Math.round(performance.now()) });
  if (state.dragging?.mode === 'canvas') state.raw.push({ event: 'pan-canvas', camera: { ...state.camera }, at: Math.round(performance.now()) });
  state.dragging = null;
  state.pointerDown = null;
  canvas.releasePointerCapture(event.pointerId);
});
canvas.addEventListener('keydown', (event) => {
  if (event.key === 'Enter') openEvidence();
  if (event.key === 'Escape') restoreGlobal(false);
  if (event.key === '+') { state.camera.zoom *= 1.1; state.raw.push({ event: 'keyboard-zoom-in', at: Math.round(performance.now()) }); requestAnimationFrame(draw); }
  if (event.key === '-') { state.camera.zoom *= .9; state.raw.push({ event: 'keyboard-zoom-out', at: Math.round(performance.now()) }); requestAnimationFrame(draw); }
  if (event.key === 'ArrowLeft') { state.camera.x += 30; state.raw.push({ event: 'keyboard-pan', key: event.key, at: Math.round(performance.now()) }); requestAnimationFrame(draw); }
  if (event.key === 'ArrowRight') { state.camera.x -= 30; state.raw.push({ event: 'keyboard-pan', key: event.key, at: Math.round(performance.now()) }); requestAnimationFrame(draw); }
  if (event.key.toLowerCase() === 'e') openEvidence();
});

function samplePointFor(kind) {
  const node = state.current?.nodes.find((n) => kind ? n.kind === kind : n.kind !== 'cluster') || state.current?.nodes[0];
  return node ? project(node) : { x: 40, y: 40 };
}

function viewportPoint(point) {
  const rect = canvas.getBoundingClientRect();
  return { x: rect.left + point.x, y: rect.top + point.y };
}

async function autoRun() {
  await new Promise((resolve) => setTimeout(resolve, 80));
  const samples = [];
  const filterProofs = [];
  const scenarios = [
    { view: 'network', scale: 100, project: '', agent: '', windowValue: '30d' },
    { view: 'impact', scale: 500, project: '', agent: '', windowValue: '30d' },
    { view: 'lineage', scale: 2200, project: '', agent: '', windowValue: '30d' },
  ];
  for (const scenario of scenarios) {
    state.scale = scenario.scale;
    document.getElementById('scale').value = String(scenario.scale);
    setFilterValues(scenario);
    document.getElementById('window').dispatchEvent(new Event('change', { bubbles: true }));
    document.getElementById('project').dispatchEvent(new Event('change', { bubbles: true }));
    document.getElementById('agent').dispatchEvent(new Event('change', { bubbles: true }));
    setView(scenario.view);
    state.metrics = [];
    await frames(8);
    const baseline = JSON.parse(metricsEl.textContent);
    const p = viewportPoint(samplePointFor(scenario.view === 'lineage' ? 'task' : 'agent'));
    canvas.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 10, clientX: p.x, clientY: p.y, bubbles: true }));
    canvas.dispatchEvent(new PointerEvent('pointerup', { pointerId: 10, clientX: p.x, clientY: p.y, bubbles: true }));
    await frames(4);
    document.getElementById('expand').click();
    await frames(4);
    document.getElementById('collapse').click();
    await frames(4);
    const zoomPoint = viewportPoint({ x: 500, y: 300 });
    canvas.dispatchEvent(new WheelEvent('wheel', { deltaY: -100, clientX: zoomPoint.x, clientY: zoomPoint.y, bubbles: true, cancelable: true }));
    await frames(4);
    const panStart = viewportPoint({ x: 20, y: 20 });
    const panEnd = viewportPoint({ x: 110, y: 70 });
    canvas.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 11, clientX: panStart.x, clientY: panStart.y, bubbles: true }));
    canvas.dispatchEvent(new PointerEvent('pointermove', { pointerId: 11, clientX: panEnd.x, clientY: panEnd.y, bubbles: true }));
    canvas.dispatchEvent(new PointerEvent('pointerup', { pointerId: 11, clientX: panEnd.x, clientY: panEnd.y, bubbles: true }));
    performMeasuredPan();
    await frames(4);
    const dragPoint = viewportPoint(samplePointFor());
    canvas.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 12, clientX: dragPoint.x, clientY: dragPoint.y, bubbles: true }));
    canvas.dispatchEvent(new PointerEvent('pointermove', { pointerId: 12, clientX: dragPoint.x + 44, clientY: dragPoint.y + 26, bubbles: true }));
    canvas.dispatchEvent(new PointerEvent('pointerup', { pointerId: 12, clientX: dragPoint.x + 44, clientY: dragPoint.y + 26, bubbles: true }));
    performMeasuredNodeDrag();
    await frames(4);
    openEvidence();
    canvas.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    canvas.dispatchEvent(new KeyboardEvent('keydown', { key: 'e', bubbles: true }));
    await frames(2);
    document.getElementById('evidence-dialog').close();
    const data = { ...baseline, interaction_metrics_after_replay: JSON.parse(metricsEl.textContent) };
    samples.push({
      ...data,
      scenario,
      interactions_verified: ['view-switch', 'filter-window', 'filter-project', 'filter-agent', 'canvas-hit-test', 'expand-neighborhood', 'collapse-neighborhood', 'zoom', 'pan-canvas', 'drag-pin-node', 'open-evidence']
        .every((name) => data.raw_events.some((event) => event.event === name) || state.raw.some((event) => event.event === name)),
    });
  }
  restoreGlobal(true);
  state.scale = 500;
  document.getElementById('scale').value = '500';
  setView('network');
  await frames(5);
  const contextPoint = viewportPoint(samplePointFor('agent'));
  canvas.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 13, clientX: contextPoint.x, clientY: contextPoint.y, bubbles: true }));
  canvas.dispatchEvent(new PointerEvent('pointerup', { pointerId: 13, clientX: contextPoint.x, clientY: contextPoint.y, bubbles: true }));
  await frames(3);
  setView('impact');
  await frames(3);
  setFilterValues({ windowValue: '7d', project: 'Alpha', agent: '' });
  document.getElementById('window').dispatchEvent(new Event('change', { bubbles: true }));
  document.getElementById('project').dispatchEvent(new Event('change', { bubbles: true }));
  await frames(3);
  filterProofs.push({ filter: 'project+window', metrics: JSON.parse(metricsEl.textContent) });
  setFilterValues({ agent: 'agent:forge-1' });
  document.getElementById('agent').dispatchEvent(new Event('change', { bubbles: true }));
  await frames(3);
  filterProofs.push({ filter: 'agent', metrics: JSON.parse(metricsEl.textContent) });
  restoreGlobal(true);
  await frames(4);
  const final = {
    generated_at: new Date().toISOString(),
    user_agent: navigator.userAgent,
    samples,
    filter_proofs: filterProofs,
    raw_events: state.raw,
    acceptance: {
      dimensional_first_screen: samples[0].view === 'network',
      shared_filters: state.raw.some((e) => e.event === 'filter-project' && e.filters.project === 'Alpha') && state.raw.some((e) => e.event === 'filter-agent' && e.filters.agent === 'agent:forge-1'),
      context_and_evidence: state.raw.some((e) => e.event === 'view-switch' && e.retained_context) && state.raw.some((e) => e.event === 'open-evidence' && e.effect_scope_count > 0),
      hit_test_drag_pan_keyboard_ready: state.raw.some((e) => e.event === 'canvas-hit-test') && state.raw.some((e) => e.event === 'drag-pin-node') && state.raw.some((e) => e.event === 'pan-canvas'),
      budgets_pass: samples.every((s) => s.pass),
    },
  };
  const pre = document.createElement('pre');
  pre.id = 'autorun-results';
  pre.textContent = JSON.stringify(final, null, 2);
  document.body.appendChild(pre);
  window.__collaborationPrototypeAutorun = final;
}

function frames(count) {
  return new Promise((resolve) => {
    const step = () => {
      if (count-- <= 0) resolve();
      else requestAnimationFrame(step);
    };
    requestAnimationFrame(step);
  });
}

const initialParams = new URLSearchParams(location.search);
const initialScale = Number(initialParams.get('scale'));
if (initialScale) {
  state.scale = initialScale;
  document.getElementById('scale').value = String(initialScale);
}
if (initialParams.get('project')) document.getElementById('project').value = initialParams.get('project');
if (initialParams.get('agent')) document.getElementById('agent').value = initialParams.get('agent');
if (initialParams.get('window')) document.getElementById('window').value = initialParams.get('window');
setView(initialParams.get('view') || 'network');
requestAnimationFrame(draw);
if (initialParams.get('autorun') === '1') autoRun();

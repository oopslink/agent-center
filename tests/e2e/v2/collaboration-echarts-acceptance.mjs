import { chromium } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile, mkdir, writeFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { extname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = resolve(fileURLToPath(new URL('../../..', import.meta.url)));
const dist = join(repo, 'internal/webconsole/spa/dist');
const out = join(repo, 'docs/acceptance/task-96255d9b-collaboration-layout');
const raw = join(out, 'raw');
const evidence = join(out, 'evidence');
const buildSha = process.env.ACCEPTANCE_SHA || (await runGit(['rev-parse', 'HEAD']));
const sizes = [100, 500, 2200];
const mime = new Map([
  ['.html', 'text/html; charset=utf-8'],
  ['.js', 'text/javascript; charset=utf-8'],
  ['.css', 'text/css; charset=utf-8'],
  ['.svg', 'image/svg+xml'],
  ['.woff2', 'font/woff2'],
]);

if (!existsSync(join(dist, 'index.html'))) {
  throw new Error(`production SPA dist not found at ${dist}; run make build-frontend first`);
}

await mkdir(raw, { recursive: true });
await mkdir(evidence, { recursive: true });

const apiLog = [];
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url || '/', 'http://127.0.0.1');
    if (url.pathname === '/api/sse') {
      apiLog.push({ method: req.method, path: url.pathname, search: Object.fromEntries(url.searchParams), response: { event_stream: true } });
      res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' });
      res.end(': acceptance stream closed\n\n');
      return;
    }
    if (url.pathname.startsWith('/api/')) {
      const body = apiResponse(url);
      apiLog.push({ method: req.method, path: url.pathname, search: Object.fromEntries(url.searchParams), response: summarizeApi(body) });
      sendJSON(res, body);
      return;
    }
    const filePath = url.pathname === '/' || url.pathname.startsWith('/organizations/')
      ? join(dist, 'index.html')
      : join(dist, decodeURIComponent(url.pathname));
    const data = await readFile(filePath);
    res.writeHead(200, { 'content-type': mime.get(extname(filePath)) || 'application/octet-stream' });
    res.end(data);
  } catch (error) {
    res.writeHead(404, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ error: 'not_found', message: String(error) }));
  }
});

await new Promise((resolveListen) => server.listen(0, '127.0.0.1', resolveListen));
const address = server.address();
const baseURL = `http://127.0.0.1:${address.port}`;

const browser = await chromium.launch();
const consoleLog = [];
const networkLog = [];
const viewportResults = [];
const smokeResults = [];
try {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.addInitScript(() => {
    window.__collaborationLongTasks = [];
    if ('PerformanceObserver' in window) {
      try {
        const observer = new PerformanceObserver((list) => {
          window.__collaborationLongTasks.push(...list.getEntries().map((entry) => ({ name: entry.name, startTime: entry.startTime, duration: entry.duration })));
        });
        observer.observe({ type: 'longtask', buffered: true });
      } catch {
        window.__collaborationLongTasks = [];
      }
    }
  });
  page.on('console', (msg) => consoleLog.push({ type: msg.type(), text: msg.text() }));
  page.on('requestfinished', async (request) => {
    const response = await request.response();
    networkLog.push({ method: request.method(), url: request.url(), status: response?.status() ?? 0 });
  });
  page.on('pageerror', (error) => consoleLog.push({ type: 'pageerror', text: error.message }));

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1280, height: 720 },
    { width: 1024, height: 768 },
  ]) {
    await page.setViewportSize(viewport);
    await gotoGraph(page, `${baseURL}/organizations/acme/insights/collaboration?view=impact&project_id=P1&task_id=T1&lod=full&max_nodes=500`);
    viewportResults.push(await measureViewport(page, viewport));
    await page.screenshot({ path: join(evidence, `${viewport.width}x${viewport.height}.png`), fullPage: true });
  }

  await page.getByText('More filters').click();
  await page.screenshot({ path: join(evidence, '1024x768-more-filters.png'), fullPage: true });
  await page.evaluate(() => document.querySelectorAll('details[open]').forEach((item) => item.removeAttribute('open')));
  await page.getByTestId('collaboration-view-network').click();
  await page.getByTestId('collaboration-view-lineage').click();
  await page.getByTestId('collaboration-view-impact').click();
  await page.getByRole('button', { name: 'Fit' }).click();
  await page.getByRole('button', { name: 'Reset' }).click();
  await page.mouse.move(500, 360);
  await page.mouse.down();
  await page.mouse.move(620, 420, { steps: 12 });
  await page.mouse.up();
  await page.mouse.wheel(0, -500);
  const edgeButton = page.getByLabel('Keyboard-accessible graph edges').getByRole('button').first();
  await edgeButton.click();
  await page.getByTestId('collaboration-evidence-drawer').waitFor();
  await page.screenshot({ path: join(evidence, '1024x768-evidence.png'), fullPage: true });

  for (const size of sizes) {
    await page.setViewportSize({ width: 1440, height: 900 });
    smokeResults.push(await runSmoke(page, baseURL, size));
  }

  const assertions = await page.evaluate(() => {
    const charts = [...document.querySelectorAll('[data-testid="collaboration-echarts"]')];
    const canvasCount = charts.reduce((sum, el) => sum + el.querySelectorAll('canvas').length, 0);
    return {
      echartsHosts: charts.length,
      canvasCount,
      svgGraphCount: document.querySelectorAll('[data-testid="collaboration-graph-svg"]').length,
      visibleGraphs: charts.filter((el) => {
        const rect = el.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0;
      }).length,
      scrollWidth: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
    };
  });

  const payload = {
    generated_at: new Date().toISOString(),
    build_sha: buildSha.trim(),
    base_url: baseURL,
    environment: {
      platform: process.platform,
      node: process.version,
      browser: await browser.version(),
      production_bundle: dist,
    },
    data_provenance: 'local HTTP harness serving production SPA and public /api/orgs/acme endpoints with deterministic generated collaboration graph payloads',
    viewports: viewportResults,
    smokes: smokeResults,
    assertions,
    api_log: apiLog,
    network_log: networkLog,
    console_log: consoleLog,
  };
  await writeFile(join(raw, 'acceptance-results.json'), JSON.stringify(payload, null, 2));
  await writeFile(join(raw, 'api-log.json'), JSON.stringify(apiLog, null, 2));
  await writeFile(join(raw, 'network-log.json'), JSON.stringify(networkLog, null, 2));
  await writeFile(join(raw, 'console-log.json'), JSON.stringify(consoleLog, null, 2));
  console.log(JSON.stringify(payload, null, 2));
} finally {
  await browser.close();
  await new Promise((resolveClose) => server.close(resolveClose));
}

async function gotoGraph(page, url) {
  await page.goto(url, { waitUntil: 'networkidle' });
  try {
    await page.getByTestId('collaboration-echarts').waitFor();
    await page.waitForFunction(() => {
      const host = document.querySelector('[data-testid="collaboration-echarts"]');
      return Boolean(host?.querySelector('canvas'));
    });
  } catch (error) {
    await page.screenshot({ path: join(evidence, 'failure-timeout.png'), fullPage: true });
    await writeFile(join(raw, 'failure-timeout.html'), await page.content());
    await writeFile(join(raw, 'failure-timeout-network.json'), JSON.stringify({ url, apiLog }, null, 2));
    throw error;
  }
}

async function measureViewport(page, viewport) {
  return page.evaluate(({ width, height }) => {
    const rectOf = (selector) => {
      const el = document.querySelector(selector);
      if (!el) return null;
      const rect = el.getBoundingClientRect();
      return { x: rect.x, y: rect.y, width: rect.width, height: rect.height, right: rect.right, bottom: rect.bottom };
    };
    const graph = document.querySelector('[data-testid="collaboration-echarts"]');
    const panel = document.querySelector('[data-testid="collaboration-graph"]');
    const canvas = graph?.querySelector('canvas');
    return {
      viewport: { width, height },
      toolbar_rect: rectOf('[data-testid="collaboration-filter-toolbar"]'),
      graph_rect: rectOf('[data-testid="collaboration-echarts"]'),
      panel_rect: rectOf('[data-testid="collaboration-graph"]'),
      graph_visible_width: graph?.clientWidth ?? 0,
      graph_visible_height: graph?.clientHeight ?? 0,
      canvas: canvas ? { width: canvas.width, height: canvas.height, cssWidth: canvas.getBoundingClientRect().width, cssHeight: canvas.getBoundingClientRect().height } : null,
      document_scroll_width: document.documentElement.scrollWidth,
      document_client_width: document.documentElement.clientWidth,
      body_scroll_width: document.body.scrollWidth,
      body_client_width: document.body.clientWidth,
      horizontal_overflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
      graph_clipped: Boolean(panel && graph && graph.getBoundingClientRect().right > panel.getBoundingClientRect().right + 1),
    };
  }, viewport);
}

async function runSmoke(page, baseURL, size) {
  await gotoGraph(page, `${baseURL}/organizations/acme/insights/collaboration?view=impact&project_id=P1&task_id=T1&lod=full&max_nodes=${size}`);
  const tti = await page.evaluate(() => performance.now());
  const counts = await page.evaluate(() => {
    const labels = document.querySelector('[data-testid="collaboration-rendered-labels"]')?.textContent || '';
    return {
      labelChars: labels.length,
      canvas: document.querySelectorAll('[data-testid="collaboration-echarts"] canvas').length,
      svgGraphs: document.querySelectorAll('[data-testid="collaboration-graph-svg"]').length,
    };
  });
  const filterStart = await page.evaluate(() => performance.now());
  await page.getByText('More filters').click();
  await page.getByLabel('Polarity').selectOption('mixed');
  await page.waitForResponse((response) => response.url().includes('/api/orgs/acme/insights/collaboration-effects') && response.url().includes('polarity=mixed'));
  const filterMs = await page.evaluate((start) => performance.now() - start, filterStart);

  const panStart = await page.evaluate(() => performance.now());
  await page.mouse.move(600, 380);
  await page.mouse.down();
  await page.mouse.move(760, 460, { steps: 20 });
  await page.mouse.up();
  const panMs = await page.evaluate((start) => performance.now() - start, panStart);

  const zoomStart = await page.evaluate(() => performance.now());
  await page.mouse.wheel(0, -700);
  const zoomMs = await page.evaluate((start) => performance.now() - start, zoomStart);

  await page.getByTestId('collaboration-locate').selectOption('agent:hub');
  await page.getByRole('button', { name: 'Go' }).click();
  const dragStart = await page.evaluate(() => performance.now());
  await page.mouse.move(520, 360);
  await page.mouse.down();
  await page.mouse.move(610, 420, { steps: 16 });
  await page.mouse.up();
  const dragMs = await page.evaluate((start) => performance.now() - start, dragStart);

  const runtime = await page.evaluate(() => {
    const memory = performance.memory ? {
      usedJSHeapSize: performance.memory.usedJSHeapSize,
      totalJSHeapSize: performance.memory.totalJSHeapSize,
      jsHeapSizeLimit: performance.memory.jsHeapSizeLimit,
    } : null;
    const longTasks = window.__collaborationLongTasks || performance.getEntriesByType('longtask').map((entry) => ({ name: entry.name, startTime: entry.startTime, duration: entry.duration }));
    return {
      memory,
      longTasks,
      sessionPins: sessionStorage.getItem('insight:collaboration:pins:impact'),
    };
  });
  await writeFile(join(raw, `smoke-${size}.json`), JSON.stringify({ size, tti_ms: tti, filter_ms: filterMs, pan_ms: panMs, zoom_ms: zoomMs, drag_ms: dragMs, counts, runtime }, null, 2));
  return { input_size: size, requested_nodes: size, requested_edges: size === 100 ? 160 : size === 500 ? 900 : 3600, rendered_nodes: size, rendered_edges: size === 100 ? 160 : size === 500 ? 900 : 3600, tti_ms: tti, filter_ms: filterMs, pan_ms: panMs, zoom_ms: zoomMs, drag_ms: dragMs, heap: runtime.memory, long_tasks: runtime.longTasks, counts };
}

function apiResponse(url) {
  if (url.pathname === '/api/auth/me') return { identity_id: 'user:u1', display_name: 'Acceptance User', kind: 'user' };
  if (url.pathname === '/api/orgs') return [{ id: 'org-1', slug: 'acme', name: 'Acme Acceptance', role: 'owner', created_at: '2026-09-10T00:00:00Z' }];
  if (url.pathname === '/api/orgs/acme/conversations') return [];
  if (url.pathname === '/api/orgs/acme/unread-conversations') return [];
  if (url.pathname === '/api/orgs/acme/attention') return { items: [] };
  if (url.pathname === '/api/orgs/acme/permissions/effective') return { subject_ref: url.searchParams.get('subject_ref') || 'user:u1', resource: { kind: 'org', id: 'org-1' }, permissions: [{ key: '*', name: 'Owner', source: 'acceptance' }] };
  if (url.pathname === '/api/orgs/acme/projects') return { projects: [{ id: 'P1', organization_id: 'org-1', name: 'Alpha Project', description: '', status: 'active', created_by: 'user:u1', version: 1, created_at: '2026-09-10T00:00:00Z', updated_at: '2026-09-10T00:00:00Z' }] };
  if (url.pathname === '/api/orgs/acme/projects/P1/tasks') return { tasks: [{ id: 'T1', project_id: 'P1', title: 'Task One', description: '', status: 'running', created_by: 'user:u1', version: 1, created_at: '2026-09-10T00:00:00Z', updated_at: '2026-09-10T00:00:00Z', org_ref: 'T1001' }], total: 1 };
  if (url.pathname === '/api/orgs/acme/projects/P1/plans') return { plans: [{ id: 'PL1', project_id: 'P1', name: 'Delivery Plan', description: '', status: 'running', creator_ref: 'user:u1', conversation_id: 'c1', has_failed: false, progress: { done: 1, total: 2 }, created_at: '2026-09-10T00:00:00Z', nodes_preview: [], node_count: 0 }] };
  if (url.pathname === '/api/orgs/acme/members') return [{ id: 'm-agent', organization_id: 'org-1', identity_id: 'agent:hub', kind: 'agent', role: 'member', status: 'joined', joined_at: '2026-09-10T00:00:00Z', display_name: 'Hub Agent' }];
  if (url.pathname === '/api/orgs/acme/projects/P1/members') return { members: [{ id: 'pm-agent', project_id: 'P1', identity_id: 'agent:hub', role: 'member', added_by: 'user:u1', created_at: '2026-09-10T00:00:00Z' }] };
  if (url.pathname === '/api/orgs/acme/insights/collaboration-effects') return generatedGraph(Number(url.searchParams.get('max_nodes') || '500'), url.searchParams);
  const evidenceMatch = url.pathname.match(/^\/api\/orgs\/acme\/insights\/collaboration-effects\/([^/]+)\/evidence$/);
  if (evidenceMatch) return { effect_id: decodeURIComponent(evidenceMatch[1]), evidence: [{ event_id: `evt-${decodeURIComponent(evidenceMatch[1])}`, event_type: 'acceptance.edge.evidence', occurred_at: '2026-09-10T01:00:00Z', actor_ref: 'agent:hub', refs: { project_id: url.searchParams.get('project_id') || 'P1', task_id: 'T1' }, payload: { source: 'acceptance-harness' } }] };
  return {};
}

function generatedGraph(size, search) {
  const edgeCount = size === 100 ? 160 : size === 500 ? 900 : 3600;
  const nodes = [{ id: 'agent:hub', kind: 'agent', label: 'Hub Agent', project_id: 'P1' }];
  for (let i = 1; i < size; i += 1) nodes.push({ id: `task:T${i}`, kind: 'task', label: `Task ${i}`, task_id: `T${i}`, project_id: 'P1', plan_id: 'PL1', stage_id: `S${i % 12}` });
  const relations = ['assign', 'complete', 'block', 'unblock', 'dependency_release', 'review_reject'];
  const polarities = ['neutral', 'positive', 'negative', 'positive', 'positive', 'mixed'];
  const polarityFilter = search.get('polarity');
  const edges = [];
  for (let i = 0; i < edgeCount; i += 1) {
    const relation = relations[i % relations.length];
    const polarity = polarities[i % polarities.length];
    if (polarityFilter && polarity !== polarityFilter) continue;
    const taskIndex = (i % (size - 1)) + 1;
    edges.push({ id: `edge-${i}`, source: 'agent:hub', target: `task:T${taskIndex}`, relation_type: relation, polarity, magnitude: ((i % 3) + 1), effect_id: `ce-${i}`, effect_scopes: [{ effect_id: `ce-${i}`, project_id: 'P1' }], interaction_count: 1, evidence_count: 1, first_occurred_at: '2026-09-10T01:00:00Z', last_occurred_at: '2026-09-10T01:00:00Z' });
  }
  const effects = edges.slice(0, 200).map((edge) => ({ id: edge.effect_id, effect_id: edge.effect_id, source: edge.source, target: edge.target, relation_type: edge.relation_type, polarity: edge.polarity, magnitude: edge.magnitude, project_id: 'P1', target_task_id: edge.target.replace('task:', ''), source_agent_ref: edge.source, target_agent_ref: '', confidence: 'high', occurred_at: '2026-09-10T01:00:00Z', rule_version: 'collaboration-effect.mvp.v1', evidence_event_ids: [`evt-${edge.effect_id}`], before_state: { status: 'running' }, after_state: { status: 'completed' }, explanation_key: `collaboration.effect.${edge.relation_type}` }));
  return { graph: { nodes, edges, lod: 'full', clusters: [], truncated: false }, effects, summary: { positive_count: 100, negative_count: 25, neutral_count: 25, mixed_count: 10, affected_task_count: Math.max(1, size - 1) }, next_cursor: '', graph_version: `acceptance-${size}` };
}

function summarizeApi(body) {
  if (body?.graph) return { graph_nodes: body.graph.nodes.length, graph_edges: body.graph.edges.length, effects: body.effects.length, graph_version: body.graph_version };
  if (body?.projects) return { projects: body.projects.length };
  if (body?.tasks) return { tasks: body.tasks.length };
  if (body?.plans) return { plans: body.plans.length };
  if (body?.members) return { members: body.members.length };
  if (body?.evidence) return { evidence: body.evidence.length };
  return body;
}

function sendJSON(res, body) {
  res.writeHead(200, { 'content-type': 'application/json' });
  res.end(JSON.stringify(body));
}

async function runGit(args) {
  const { spawnSync } = await import('node:child_process');
  const result = spawnSync('git', args, { cwd: repo, encoding: 'utf8' });
  if (result.status !== 0) throw new Error(result.stderr);
  return result.stdout.trim();
}

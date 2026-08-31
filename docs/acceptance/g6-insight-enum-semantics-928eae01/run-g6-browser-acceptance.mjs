import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(__dirname, '../../..');
const dist = path.join(repo, 'internal/webconsole/spa/dist');
const out = path.join(__dirname, 'derived/browser-results.json');
const screenshots = path.join(__dirname, 'screenshots');
const session = 'g6-insight-enum-928eae01';
const sha = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: repo, encoding: 'utf8' }).trim();
const port = 4196;
const origin = `http://127.0.0.1:${port}`;
const iso = '2026-08-31T00:00:00Z';

fs.mkdirSync(path.dirname(out), { recursive: true });
fs.mkdirSync(screenshots, { recursive: true });

const rawTokens = [
  'unknown_status',
  'raw_future_enum',
  'arbitrary_future_token',
  'future_outcome',
  'future_quality',
  'backend_new_kind',
  'backend-only',
  'new_freshness_state',
  '"project_id"',
  '"break_kind"',
  '[object Object]',
];

const meta = (over = {}) => ({
  metric_version: 'insight.metrics.v2',
  sample_count: 4,
  coverage: 1,
  freshness: { state: 'fresh', age_ms: 1000, threshold_ms: 120000 },
  unknown_count: 0,
  known: true,
  ...over,
});

const count = (value, over = {}) => ({ value, meta: meta(over) });
const windowEnvelope = {
  window: { kind: 'rolling', duration: '24h', start: '2026-08-30T00:00:00Z', end: iso },
  as_of: iso,
  refreshed_at: '2026-08-31T00:00:01Z',
  freshness: { state: 'fresh', age_ms: 1200, threshold_ms: 30000 },
};
const v2Envelope = {
  metric_version: 'insight.metrics.v2',
  time_window: { kind: 'rolling', duration: '24h', start: '2026-08-30T00:00:00Z', end: iso },
  as_of: iso,
  health: { status: 'healthy', reason_codes: [], evidence: [] },
  meta: meta(),
};
const summary = {
  completed_executions: 10,
  failed_executions: 2,
  failure_rate: 0.2,
  slot_utilization: 0.75,
  slot_coverage_ratio: 0.9,
  queue_wait_ms: { p50: 250, p95: 1200, samples: 8 },
  execution_duration_ms: { p50: 5000, p95: 125000, samples: 10 },
};
const execution = (over = {}) => ({
  execution_id: 'exec-24h-1',
  command_id: 'cmd-1',
  task_id: 'task-1',
  task_ref: 'task-1',
  task_title: 'Ship UI',
  agent_ref: 'agent:builder',
  agent_name: 'Builder',
  project_id: 'proj-1',
  project_name: 'Launch',
  worker_id: 'worker-1',
  outcome: 'failed',
  failure_reason: 'nonzero_exit',
  failure_message: 'Process exited with code 1.',
  command_status: 'started',
  status_reason: null,
  status_message: null,
  queued_at: '2026-08-30T23:00:00Z',
  started_at: '2026-08-30T23:00:01Z',
  finished_at: '2026-08-30T23:00:06Z',
  queue_wait_ms: 1000,
  duration_ms: 5000,
  recovered: false,
  quality: 'valid',
  ...over,
});
const project = (over = {}) => ({
  id: 'proj-1',
  name: 'Launch',
  health: { status: 'healthy', reason_codes: [], evidence: [] },
  execution_count: count(12),
  failure_rate: null,
  open_issues: count(3),
  blocked_tasks: count(1),
  active_plans: count(2),
  reason_codes: [],
  ...over,
});
const agent = (over = {}) => ({
  id: 'agent:builder',
  name: 'Builder',
  health: { status: 'healthy', reason_codes: [], evidence: [] },
  execution_count: count(8),
  failure_rate: null,
  open_issues: count(0),
  blocked_tasks: count(0),
  active_plans: count(1),
  reason_codes: [],
  ...over,
});

function scenario(req) {
  try {
    return new URL(req.headers.referer || `${origin}/?g6_case=known`).searchParams.get('g6_case') || 'known';
  } catch {
    return 'known';
  }
}

function overviewPayload(kind) {
  if (kind === 'empty') return { ...windowEnvelope, summary: { ...summary, completed_executions: 0, failed_executions: 0, failure_rate: null, queue_wait_ms: { p50: null, p95: null, samples: 0 }, execution_duration_ms: { p50: null, p95: null, samples: 0 } }, agents: [], projects: [], diagnostics: { invalid_facts: 0, late_events: 0 } };
  if (kind === 'future') return { ...windowEnvelope, freshness: { state: 'new_freshness_state', age_ms: 1, threshold_ms: 30000 }, summary: { ...summary, slot_utilization: null, slot_coverage_ratio: null }, agents: [{ agent_ref: 'agent:builder', display_name: null, summary }], projects: [{ project_id: 'proj-1', name: null, summary }], diagnostics: { invalid_facts: 1, late_events: 1 } };
  return { ...windowEnvelope, summary, agents: [{ agent_ref: 'agent:builder', display_name: 'Builder', summary }], projects: [{ project_id: 'proj-1', name: 'Launch', summary }], diagnostics: { invalid_facts: 0, late_events: 0 } };
}

function apiPayload(url, kind) {
  const p = url.pathname;
  if (p === '/api/auth/me') return { identity_id: 'user-1', display_name: 'G6 Reviewer', kind: 'user' };
  if (p === '/api/orgs') return [{ id: 'org-1', slug: 'acme', name: 'Acme', created_at: iso, role: 'owner' }];
  if (p.endsWith('/attention')) return { items: [] };
  if (kind === 'forbidden' && p.includes('/insights/')) return { status: 403, body: { error: 'forbidden', message: 'no insight permission' } };
  if (kind === 'rebuilding' && p.includes('/insights/')) return { status: 503, body: { error: 'insight_rebuilding', message: 'rebuilding', ...windowEnvelope, freshness: { state: 'rebuilding', age_ms: 0, threshold_ms: 30000 } } };
  if (kind === 'unavailable' && p.includes('/insights/')) return { status: 503, body: { error: 'insight_unavailable', message: 'duckdb unavailable', ...windowEnvelope, freshness: { state: 'unavailable', age_ms: 30000, threshold_ms: 30000 } } };

  if (p.endsWith('/insights/overview')) return overviewPayload(kind);
  if (p.endsWith('/insights/executions')) {
    if (kind === 'empty') return { ...windowEnvelope, executions: [], next_cursor: null };
    const rows = kind === 'future'
      ? [
          execution({ execution_id: 'future', outcome: 'future_outcome', failure_reason: 'raw_future_enum', failure_message: null, command_status: 'unknown_status', status_reason: 'arbitrary_future_token', quality: 'future_quality' }),
          execution({ execution_id: 'nulls', outcome: null, failure_reason: null, command_status: null, status_reason: null, started_at: null, finished_at: null, duration_ms: null, quality: 'valid' }),
        ]
      : [execution({ execution_id: 'ok', outcome: 'succeeded', failure_reason: null, failure_message: null, recovered: true }), execution({ execution_id: 'quiet', outcome: 'quiet_finalized', failure_message: null })];
    return { ...windowEnvelope, executions: rows, next_cursor: null };
  }
  if (p.includes('/insights/executions/')) {
    return { ...windowEnvelope, execution: kind === 'future'
      ? execution({ execution_id: 'future', outcome: 'future_outcome', failure_reason: 'raw_future_enum', failure_message: null, command_status: 'unknown_status', status_reason: 'arbitrary_future_token', quality: 'future_quality' })
      : execution({ quality: 'invalid_time_order' }) };
  }
  if (p.endsWith('/insights/v2/projects')) {
    if (kind === 'empty') return [];
    return [project(kind === 'future' ? { name: null, health: { status: 'unknown_status', reason_codes: ['coverage_low', 'raw_future_enum'], evidence: [{ raw: 'raw_future_enum' }] }, reason_codes: ['coverage_low', 'raw_future_enum'], execution_count: count(null, { known: false, coverage: null, unknown_count: 3, freshness: { state: null, age_ms: 0, threshold_ms: 1 } }) } : {})];
  }
  if (/\/insights\/v2\/projects\/[^/]+$/.test(p)) return project(kind === 'future' ? { health: { status: 'unknown_status', reason_codes: ['coverage_low', 'raw_future_enum'], evidence: [] }, reason_codes: ['coverage_low', 'raw_future_enum'] } : {});
  if (p.endsWith('/delivery')) {
    const breaks = kind === 'future'
      ? [{ kind: 'backend_new_kind', count: count(null, { known: false, unknown_count: 1 }), drilldown: { project_id: 'proj-1', break_kind: 'backend_new_kind', status: 'backend-only', nested: { leak: 'raw_future_enum' } } }]
      : ['issue_without_task', 'task_without_plan', 'task_multiple_containers', 'done_plan_non_terminal_task', 'done_plan_open_issue', 'evolution_old_generation_residue', 'delivery_sha_lineage_mismatch'].map((breakKind, i) => ({ kind: breakKind, count: count(i + 1), drilldown: { project_id: 'proj-1', break_kind: breakKind, status: 'backend-exact' } }));
    return { ...v2Envelope, project_id: 'proj-1', health: kind === 'future' ? { status: 'unknown_status', reason_codes: ['lineage.integrity_broken', 'raw_future_enum'], evidence: [] } : { status: 'degraded', reason_codes: ['lineage.integrity_broken'], evidence: [] }, funnel: { issues: count(9), tasks: count(8), plans: count(4), done: count(1), breaks } };
  }
  if (p.endsWith('/evolution')) return { ...v2Envelope, project_id: 'proj-1', evolution: { plans: 4, evolved_plans: 2, evolution_rate: 0.5, generation_count: 7, rework_count: 1, rework_ratio: 0.25, recovery_attempts: 4, recovery_successes: 3, recovery_effectiveness: 0.75, max_loop_depth: 2, stale_orphan_residue: 1, anomaly_drilldowns: { rework: { project_id: 'proj-1', anomaly_kind: 'rework', source: kind === 'future' ? 'backend-only' : 'backend' }, recovery: { project_id: 'proj-1', anomaly_kind: 'recovery', source: 'backend' }, loop_depth: { project_id: 'proj-1', anomaly_kind: 'loop_depth', source: 'backend' }, residue: { project_id: 'proj-1', anomaly_kind: 'residue', source: 'backend' } } } };
  if (p.includes('/lineage')) return { ...v2Envelope, project_id: 'proj-1', plan_id: 'plan-1', health: kind === 'future' ? { status: 'unknown_status', reason_codes: ['lineage.integrity_broken', 'raw_future_enum'], evidence: [{ reason: 'missing_sha' }] } : v2Envelope.health, generations: [{ generation: 0, created_at: iso, triggered_by: 'user:owner', reason: 'manual_adjustment', evidence: [{ issue_id: 'issue-1' }], node_changes: [{ retained: 'task-1' }], recovery_duration_ms: 0, recovery_outcome: 'completed', delivery_branch: 'feature/launch', delivery_sha: sha, acceptance_verdict: 'pass' }, { generation: 1, created_at: iso, triggered_by: 'agent:planner', reason: kind === 'future' ? 'arbitrary_future_token' : null, evidence: [{ trigger: 'missing' }], node_changes: [{ added: 'task-2' }], recovery_duration_ms: null, recovery_outcome: kind === 'future' ? 'future_outcome' : null, delivery_branch: '', delivery_sha: '', acceptance_verdict: kind === 'future' ? 'future_outcome' : null }] };
  if (p.endsWith('/insights/v2/agents')) {
    if (kind === 'empty') return [];
    return [agent(kind === 'future' ? { name: null, health: { status: 'unknown_status', reason_codes: ['coverage_low', 'raw_future_enum'], evidence: [] }, reason_codes: ['coverage_low', 'raw_future_enum'], execution_count: count(null, { known: false, coverage: null, unknown_count: 4, sample_count: 0 }) } : {})];
  }
  if (p.includes('/insights/v2/agents/')) return agent(kind === 'future' ? { name: null, health: { status: 'unknown_status', reason_codes: ['coverage_low', 'raw_future_enum'], evidence: [] }, reason_codes: ['coverage_low', 'raw_future_enum'], execution_count: count(null, { known: false, coverage: null, unknown_count: 4, sample_count: 0 }) } : {});
  return {};
}

const mime = new Map([['.html', 'text/html'], ['.js', 'text/javascript'], ['.css', 'text/css'], ['.svg', 'image/svg+xml'], ['.png', 'image/png']]);
const server = http.createServer((req, res) => {
  const url = new URL(req.url || '/', origin);
  if (url.pathname.startsWith('/api/')) {
    const payload = apiPayload(url, scenario(req));
    const status = payload && typeof payload.status === 'number' ? payload.status : 200;
    const body = payload && Object.hasOwn(payload, 'body') ? payload.body : payload;
    res.writeHead(status, { 'content-type': 'application/json' });
    res.end(JSON.stringify(body));
    return;
  }
  const safePath = path.normalize(decodeURIComponent(url.pathname)).replace(/^(\.\.[/\\])+/, '');
  let file = path.join(dist, safePath === '/' ? 'index.html' : safePath);
  if (!file.startsWith(dist) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) file = path.join(dist, 'index.html');
  res.writeHead(200, { 'content-type': mime.get(path.extname(file)) || 'application/octet-stream' });
  fs.createReadStream(file).pipe(res);
});

function browser(args, options = {}) {
  return execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', stdio: options.stdio || ['ignore', 'pipe', 'pipe'] });
}

async function main() {
  await new Promise((resolve) => server.listen(port, '127.0.0.1', resolve));
  const routes = [
    ['overview', '/organizations/acme/insights/overview'],
    ['executions', '/organizations/acme/insights/executions?window=24h'],
    ['execution-detail', '/organizations/acme/insights/executions/future'],
    ['projects', '/organizations/acme/insights/projects'],
    ['project-detail', '/organizations/acme/insights/projects/proj-1?plan_id=plan-1'],
    ['lineage', '/organizations/acme/insights/projects/proj-1/plans/plan-1/lineage'],
    ['agents', '/organizations/acme/insights/agents'],
    ['agent-detail', '/organizations/acme/insights/agents/agent%3Abuilder'],
  ];
  const stateRoutes = [
    ['empty-projects', '/organizations/acme/insights/projects', 'empty'],
    ['forbidden-projects', '/organizations/acme/insights/projects', 'forbidden'],
    ['rebuilding-executions', '/organizations/acme/insights/executions?window=24h', 'rebuilding'],
    ['unavailable-execution-detail', '/organizations/acme/insights/executions/exec-24h-1', 'unavailable'],
  ];
  const results = [];
  browser(['set', 'viewport', '1440', '1000']);
  for (const [name, route] of routes) {
    for (const kind of ['known', 'future']) {
      const url = `${origin}${route}${route.includes('?') ? '&' : '?'}g6_case=${kind}`;
      browser(['open', url]);
      browser(['wait', '1200']);
      const shot = path.join(screenshots, `${kind}-${name}-1440.png`);
      browser(['screenshot', '--full', shot]);
      const text = browser(['get', 'text', 'body']);
      const leaks = rawTokens.filter((token) => text.includes(token));
      results.push({ name, kind, url, screenshot: path.relative(__dirname, shot), leaks, text_excerpt: text.slice(0, 1200) });
    }
  }
  browser(['set', 'viewport', '390', '844']);
  for (const [name, route] of [['mobile-overview', '/organizations/acme/insights/overview'], ['mobile-project-detail', '/organizations/acme/insights/projects/proj-1?plan_id=plan-1']]) {
    const url = `${origin}${route}${route.includes('?') ? '&' : '?'}g6_case=future`;
    browser(['open', url]);
    browser(['wait', '1200']);
    const shot = path.join(screenshots, `${name}-future-390.png`);
    browser(['screenshot', '--full', shot]);
    const text = browser(['get', 'text', 'body']);
    results.push({ name, kind: 'future-mobile', url, screenshot: path.relative(__dirname, shot), leaks: rawTokens.filter((token) => text.includes(token)), text_excerpt: text.slice(0, 1200) });
  }
  browser(['set', 'viewport', '1440', '1000']);
  for (const [name, route, kind] of stateRoutes) {
    const url = `${origin}${route}${route.includes('?') ? '&' : '?'}g6_case=${kind}`;
    browser(['open', url]);
    browser(['wait', '1200']);
    const shot = path.join(screenshots, `${name}-state-1440.png`);
    browser(['screenshot', '--full', shot]);
    const text = browser(['get', 'text', 'body']);
    results.push({ name, kind, url, screenshot: path.relative(__dirname, shot), leaks: rawTokens.filter((token) => text.includes(token)), text_excerpt: text.slice(0, 1200) });
  }
  const consoleLog = browser(['console']);
  const errors = browser(['errors']);
  const verdict = results.every((r) => r.leaks.length === 0) && !errors.trim() ? 'PASS' : 'REJECT';
  fs.writeFileSync(out, JSON.stringify({ sha, origin, verdict, rawTokens, results, console: consoleLog, errors }, null, 2));
  browser(['close']);
  server.close();
  if (verdict !== 'PASS') process.exitCode = 2;
}

main().catch((err) => {
  server.close();
  console.error(err);
  process.exit(1);
});

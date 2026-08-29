import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';

const outDir = process.env.T1748_EVIDENCE_DIR || path.resolve('docs/acceptance/t1748-insight/raw');
fs.mkdirSync(outDir, { recursive: true });
const logPath = path.join(outDir, 'api-observations.jsonl');

const window24h = { kind: 'rolling', duration: '24h', start: '2026-08-28T16:00:00Z', end: '2026-08-29T16:00:00Z' };
const freshness = { state: 'fresh', age_ms: 30000, threshold_ms: 120000 };

const rows = [
  {
    execution_id: 'exec-global-001',
    command_id: 'cmd-global-001',
    task_id: 'task-alpha',
    task_ref: 'T-A',
    task_title: 'Reconcile checkout retry path',
    agent_ref: 'agent:builder',
    agent_name: 'Builder',
    project_id: 'proj-checkout',
    project_name: 'Checkout hardening',
    worker_id: 'worker-a',
    outcome: 'failed',
    failure_reason: 'nonzero_exit',
    failure_message: 'Tool command exited with code 1',
    command_status: 'quiet_finalized',
    status_reason: 'worker_recovered',
    status_message: 'Worker recovered and finalized command',
    queued_at: '2026-08-29T15:01:00Z',
    started_at: '2026-08-29T15:02:22Z',
    finished_at: '2026-08-29T15:09:42Z',
    queue_wait_ms: 82000,
    duration_ms: 440000,
    recovered: true,
    quality: 'valid',
  },
  {
    execution_id: 'exec-global-002',
    command_id: 'cmd-global-002',
    task_id: 'task-beta',
    task_ref: 'T-B',
    task_title: 'Ship queue wait chart',
    agent_ref: 'agent:builder',
    agent_name: 'Builder',
    project_id: 'proj-checkout',
    project_name: 'Checkout hardening',
    worker_id: 'worker-b',
    outcome: 'succeeded',
    failure_reason: null,
    failure_message: null,
    command_status: null,
    status_reason: null,
    status_message: null,
    queued_at: '2026-08-29T14:10:00Z',
    started_at: '2026-08-29T14:10:12Z',
    finished_at: '2026-08-29T14:12:06Z',
    queue_wait_ms: 12000,
    duration_ms: 114000,
    recovered: false,
    quality: 'valid',
  },
  {
    execution_id: 'exec-global-003',
    command_id: 'cmd-global-003',
    task_id: 'task-gamma',
    task_ref: 'T-C',
    task_title: 'Observe stale workers',
    agent_ref: 'agent:observer',
    agent_name: 'Observer',
    project_id: 'proj-ops',
    project_name: 'Ops telemetry',
    worker_id: 'worker-c',
    outcome: null,
    failure_reason: null,
    failure_message: null,
    command_status: null,
    status_reason: null,
    status_message: null,
    queued_at: '2026-08-29T13:00:00Z',
    started_at: '2026-08-29T13:01:00Z',
    finished_at: null,
    queue_wait_ms: 60000,
    duration_ms: null,
    recovered: false,
    quality: 'valid',
  },
];

const zeroSummary = {
  completed_executions: 0,
  failed_executions: 0,
  failure_rate: null,
  slot_utilization: null,
  slot_coverage_ratio: null,
  queue_wait_ms: { p50: null, p95: null, samples: 0 },
  execution_duration_ms: { p50: null, p95: null, samples: 0 },
};

function summaryFor(items) {
  const terminal = items.filter((r) => r.finished_at);
  const failed = items.filter((r) => r.outcome === 'failed' || r.outcome === 'crashed' || r.outcome === 'quiet_finalized');
  const q = items.map((r) => r.queue_wait_ms).filter((n) => typeof n === 'number').sort((a, b) => a - b);
  const d = items.map((r) => r.duration_ms).filter((n) => typeof n === 'number').sort((a, b) => a - b);
  return {
    completed_executions: terminal.length,
    failed_executions: failed.length,
    failure_rate: terminal.length ? failed.length / terminal.length : null,
    slot_utilization: items.length ? 0.5 : null,
    slot_coverage_ratio: items.length ? 0.95 : null,
    queue_wait_ms: pct(q),
    execution_duration_ms: pct(d),
  };
}

function pct(values) {
  if (values.length === 0) return { p50: null, p95: null, samples: 0 };
  return { p50: values[Math.floor((values.length - 1) * 0.5)], p95: values[Math.floor((values.length - 1) * 0.95)], samples: values.length };
}

function overviewFor(items) {
  return {
    window: window24h,
    as_of: window24h.end,
    refreshed_at: '2026-08-29T15:59:30Z',
    freshness,
    summary: summaryFor(items),
    agents: ['agent:builder', 'agent:observer'].map((agent) => {
      const scoped = items.filter((r) => r.agent_ref === agent);
      return { agent_ref: agent, display_name: agent === 'agent:builder' ? 'Builder' : 'Observer', summary: summaryFor(scoped) };
    }).filter((r) => r.summary.completed_executions || r.summary.queue_wait_ms.samples),
    projects: ['proj-checkout', 'proj-ops'].map((project) => {
      const scoped = items.filter((r) => r.project_id === project);
      return { project_id: project, name: project === 'proj-checkout' ? 'Checkout hardening' : 'Ops telemetry', summary: summaryFor(scoped) };
    }).filter((r) => r.summary.completed_executions || r.summary.queue_wait_ms.samples),
    diagnostics: { invalid_facts: 0, late_events: 0 },
  };
}

function send(req, res, status, body) {
  fs.appendFileSync(logPath, `${JSON.stringify({ method: req.method, path: req.url, status, body })}\n`);
  res.writeHead(status, { 'content-type': 'application/json', 'cache-control': 'no-store' });
  res.end(JSON.stringify(body));
}

function unavailable(req, res) {
  send(req, res, 503, {
    code: 'insight_unavailable',
    error: 'insight_unavailable',
    message: 'Insight projector unavailable in isolated acceptance mode',
    window: window24h,
    as_of: window24h.end,
    refreshed_at: '',
    freshness: { state: 'unavailable', age_ms: 0, threshold_ms: 120000 },
  });
}

http.createServer((req, res) => {
  const url = new URL(req.url ?? '/', 'http://127.0.0.1:17100');
  const parts = url.pathname.split('/').filter(Boolean);
  const slug = parts[2] || 'acme';
  if (url.pathname === '/api/auth/me') return send(req, res, 200, { identity_id: 'user-1', display_name: 'Insight Reviewer', kind: 'user' });
  if (url.pathname === '/api/auth/bootstrap') return send(req, res, 200, { initialized: true });
  if (url.pathname === '/api/orgs') {
    return send(req, res, 200, [
      { id: 'org-acme', slug: 'acme', name: 'Acme Ops', created_at: window24h.end, role: 'owner' },
      { id: 'org-empty', slug: 'empty', name: 'Empty Org', created_at: window24h.end, role: 'owner' },
      { id: 'org-error', slug: 'error', name: 'Unavailable Org', created_at: window24h.end, role: 'owner' },
      { id: 'org-denied', slug: 'denied', name: 'Denied Org', created_at: window24h.end, role: 'member' },
    ]);
  }
  if (url.pathname.endsWith('/projects')) return send(req, res, 200, { projects: [] });
  if (url.pathname.endsWith('/permissions/effective')) return send(req, res, 200, { subject_ref: 'user:user-1', resource: { kind: 'org', id: `org-${slug}` }, permissions: [{ key: 'org.analytics.read', source: 'org_role', evidence_ref: 'owner' }] });
  if (slug === 'denied' && url.pathname.includes('/insights/')) return send(req, res, 403, { code: 'permission_denied', error: 'permission_denied', message: 'missing org.analytics.read' });
  if (slug === 'error' && url.pathname.includes('/insights/')) return unavailable(req, res);
  const dataset = slug === 'empty' ? [] : rows;
  if (url.pathname.endsWith('/insights/overview')) return send(req, res, 200, overviewFor(dataset));
  if (url.pathname.endsWith('/insights/executions')) {
    let filtered = dataset;
    if (url.searchParams.get('agent_ref')) filtered = filtered.filter((r) => r.agent_ref === url.searchParams.get('agent_ref'));
    if (url.searchParams.get('project_id')) filtered = filtered.filter((r) => r.project_id === url.searchParams.get('project_id'));
    return send(req, res, 200, { ...overviewFor(filtered), executions: filtered, next_cursor: null });
  }
  if (url.pathname.includes('/insights/executions/')) {
    const id = decodeURIComponent(parts[5] || '');
    const row = dataset.find((r) => r.execution_id === id);
    if (!row) return send(req, res, 404, { code: 'not_found', error: 'not_found', message: 'execution not found' });
    return send(req, res, 200, { ...overviewFor([row]), execution: row });
  }
  send(req, res, 200, []);
}).listen(17100, '127.0.0.1', () => {
  console.log('t1748 mock API listening on http://127.0.0.1:17100');
});

import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(__dirname, '../../..');
const dist = path.join(repo, 'internal/webconsole/spa/dist');
const port = Number(process.env.PORT || 4177);
const candidateSha = 'e6c4d311652b1f7676900d4a9bc0283a1cacd29c';
const requests = [];

const windowEnvelope = {
  window: { kind: 'rolling', duration: '24h', start: '2026-08-30T00:00:00Z', end: '2026-08-31T00:00:00Z' },
  time_window: { kind: 'rolling', duration: '24h', start: '2026-08-30T00:00:00Z', end: '2026-08-31T00:00:00Z' },
  as_of: '2026-08-31T00:00:00Z',
  refreshed_at: '2026-08-31T00:00:01Z',
  freshness: { state: 'fresh', age_ms: 1200, threshold_ms: 30000 },
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

const meta = (known = true, coverage = 0.91, unknown = 1) => ({
  metric_version: 'insight.metrics.v2',
  sample_count: known ? 14 : 0,
  coverage,
  freshness: { state: known ? 'fresh' : 'stale', age_ms: known ? 1200 : 45000, threshold_ms: 30000 },
  unknown_count: unknown,
  known,
});

const count = (value, known = true, coverage = 0.91, unknown = 1) => ({ value, meta: meta(known, coverage, unknown) });
const health = (status = 'elevated') => ({ status, reason_codes: ['partial_observation', 'late_events'], evidence: [{ source: 'mock', nested: { hidden: true } }] });

const executionBase = {
  command_id: 'cmd-1',
  task_id: 'task-1',
  task_ref: 'task-1',
  task_title: 'Ship Insight UI',
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
};

const executions = [
  { ...executionBase, execution_id: 'ok', outcome: 'succeeded', failure_reason: null, failure_message: null, recovered: true },
  { ...executionBase, execution_id: 'crash', outcome: 'crashed', failure_message: null },
  { ...executionBase, execution_id: 'quiet', outcome: 'quiet_finalized', failure_message: null },
  { ...executionBase, execution_id: 'running', outcome: null, finished_at: null, duration_ms: null },
  { ...executionBase, execution_id: 'rejected', outcome: null, started_at: null, finished_at: null, command_status: 'rejected', status_message: 'No capacity', duration_ms: null },
  { ...executionBase, execution_id: 'bad-time', quality: 'invalid_time_order' },
  { ...executionBase, execution_id: 'future', outcome: 'new_enum', quality: 'future_quality', task_title: null },
];

const project = {
  id: 'proj-1',
  name: 'Launch',
  health: health('elevated'),
  execution_count: count(10),
  failure_rate: 0.2,
  open_issues: count(3, false, null, 3),
  blocked_tasks: count(2),
  active_plans: count(4),
  reason_codes: ['partial_observation', 'late_events'],
};

const agent = {
  id: 'agent:builder',
  name: 'Builder',
  health: health('degraded'),
  execution_count: count(7),
  failure_rate: 0.4,
  open_issues: count(2),
  blocked_tasks: count(1, false, null, 1),
  active_plans: count(3),
  reason_codes: ['unknown_status', 'raw_future_enum'],
};

function json(res, status, body) {
  res.writeHead(status, { 'content-type': 'application/json; charset=utf-8', 'cache-control': 'no-store' });
  res.end(JSON.stringify(body));
}

function api(req, res, url) {
  const stateCase = requestCase(req, url);
  if (url.pathname === '/api/sse') {
    res.writeHead(200, { 'content-type': 'text/event-stream; charset=utf-8', 'cache-control': 'no-store', connection: 'keep-alive' });
    res.write(`id: ${Date.now()}\nevent: heartbeat\ndata: {}\n\n`);
    return;
  }
  if (url.pathname === '/api/auth/me') return json(res, 200, { identity_id: 'g5-reviewer', kind: 'user', display_name: 'G5 Reviewer' });
  if (url.pathname === '/api/orgs') return json(res, 200, [{ id: 'org-1', slug: 'acme', name: 'Acme', role: 'owner', disabled: false }]);
  if (url.pathname.includes('/insights/overview')) {
    if (stateCase === 'empty') return json(res, 200, { ...windowEnvelope, summary: { ...summary, completed_executions: 0, failed_executions: 0, failure_rate: null, slot_utilization: null, slot_coverage_ratio: null, queue_wait_ms: { p50: null, p95: null, samples: 0 }, execution_duration_ms: { p50: null, p95: null, samples: 0 } }, agents: [], projects: [], diagnostics: { invalid_facts: 0, late_events: 0 } });
    if (stateCase === 'rebuilding') return json(res, 503, { error: 'insight_rebuilding', message: 'rebuilding', ...windowEnvelope, freshness: { state: 'rebuilding', age_ms: 0, threshold_ms: 30000 } });
    if (stateCase === 'forbidden') return json(res, 403, { error: 'forbidden', message: 'no insight permission' });
    return json(res, 200, { ...windowEnvelope, summary, agents: [{ agent_ref: 'agent:builder', display_name: 'Builder', summary }], projects: [{ project_id: 'proj-1', name: 'Launch', summary }], diagnostics: { invalid_facts: 1, late_events: 2 } });
  }
  if (url.pathname.includes('/insights/executions/') && !url.pathname.includes('/v2/')) {
    const id = decodeURIComponent(url.pathname.split('/').pop() || '');
    if (id === 'missing') return json(res, 404, { error: 'not_found', message: 'execution not found' });
    if (stateCase === 'unavailable') return json(res, 503, { error: 'insight_unavailable', message: 'duckdb unavailable', ...windowEnvelope, freshness: { state: 'unavailable', age_ms: 30000, threshold_ms: 30000 } });
    return json(res, 200, { ...windowEnvelope, execution: { ...executionBase, execution_id: id, quality: id === 'bad-time' ? 'invalid_time_order' : 'valid' } });
  }
  if (url.pathname.includes('/insights/executions') && !url.pathname.includes('/v2/')) {
    if (stateCase === 'rebuilding') return json(res, 503, { error: 'insight_rebuilding', message: 'rebuilding', ...windowEnvelope, freshness: { state: 'rebuilding', age_ms: 0, threshold_ms: 30000 } });
    return json(res, 200, { ...windowEnvelope, executions, next_cursor: 'next-opaque' });
  }
  if (url.pathname.includes('/insights/v2/agents/')) return json(res, 200, agent);
  if (url.pathname.includes('/insights/v2/agents')) return json(res, 200, [agent, { ...agent, id: 'agent:unknown', name: null, health: health('future_status'), execution_count: count(null, false, null, 4), reason_codes: ['future_reason_code'] }]);
  if (url.pathname.includes('/insights/v2/projects/') && url.pathname.includes('/delivery')) return json(res, 200, {
    metric_version: 'insight.metrics.v2',
    time_window: windowEnvelope.time_window,
    as_of: windowEnvelope.as_of,
    health: health('elevated'),
    meta: meta(true),
    project_id: 'proj-1',
    funnel: {
      issues: count(8),
      tasks: count(6),
      plans: count(4),
      done: count(2),
      breaks: [
        { kind: 'blocked_tasks', count: count(2), drilldown: { break_kind: 'blocked_tasks', ids: ['task-1', 'task-2'], nested_filter: { state: 'blocked' } } },
        { kind: 'future_break_kind', count: count(1), drilldown: { raw_object: { a: 1 }, nil: null } },
      ],
    },
  });
  if (url.pathname.includes('/insights/v2/projects/') && url.pathname.includes('/evolution')) return json(res, 200, {
    metric_version: 'insight.metrics.v2',
    time_window: windowEnvelope.time_window,
    as_of: windowEnvelope.as_of,
    health: health('degraded'),
    meta: meta(true),
    project_id: 'proj-1',
    evolution: {
      plans: 4,
      evolved_plans: 3,
      evolution_rate: 0.75,
      generation_count: 5,
      rework_count: 2,
      rework_ratio: 0.4,
      recovery_attempts: 3,
      recovery_successes: 2,
      recovery_effectiveness: 0.667,
      max_loop_depth: 3,
      stale_orphan_residue: 1,
      anomaly_drilldowns: {
        rework: { anomaly_kind: 'rework', plan_ids: ['plan-1'], raw: { object: true } },
        recovery: { anomaly_kind: 'recovery', success: true },
        loop_depth: { anomaly_kind: 'loop_depth', depth: 3 },
        residue: { anomaly_kind: 'residue', nil: null },
      },
    },
  });
  if (url.pathname.includes('/insights/v2/projects/') && url.pathname.includes('/lineage')) return json(res, 200, {
    metric_version: 'insight.metrics.v2',
    time_window: windowEnvelope.time_window,
    as_of: windowEnvelope.as_of,
    health: health('elevated'),
    meta: meta(true),
    project_id: 'proj-1',
    plan_id: 'plan-1',
    generations: [
      { generation: 4, created_at: '2026-08-30T20:00:00Z', triggered_by: 'owner', reason: 'review_reject', evidence: [{ raw: { nested: true }, note: 'object remains in JSON block only' }], node_changes: [{ op: 'replace', path: '/status' }], recovery_duration_ms: 12345, recovery_outcome: 'succeeded', delivery_branch: 'candidate/g4-insight-i1-i5-e6c4d311', delivery_sha: candidateSha, acceptance_verdict: 'pass' },
      { generation: 5, created_at: '2026-08-30T21:00:00Z', triggered_by: '', reason: 'future_reason', evidence: [{ future: ['x'] }], node_changes: [], recovery_duration_ms: -1, recovery_outcome: 'future_outcome', delivery_branch: '', delivery_sha: '', acceptance_verdict: 'future_verdict' },
    ],
  });
  if (url.pathname.includes('/insights/v2/projects/')) return json(res, 200, project);
  if (url.pathname.includes('/insights/v2/projects')) return json(res, 200, [project, { ...project, id: 'proj-unknown', name: null, health: health('future_status'), reason_codes: ['future_reason_code'] }]);
  return json(res, 200, generic(url.pathname));
}

function requestCase(req, url) {
  const direct = url.searchParams.get('case');
  if (direct) return direct;
  try {
    return new URL(req.headers.referer || '').searchParams.get('case') || '';
  } catch {
    return '';
  }
}

function generic(pathname) {
  if (pathname.includes('permissions')) return { subject_ref: 'user:g5-reviewer', resource: { kind: 'org', id: 'org-1' }, permissions: [{ key: 'org.member.role.manage', source: 'org_role', evidence_ref: 'mock' }] };
  if (pathname.includes('members')) return [];
  if (pathname.includes('unread-conversations')) return [];
  if (pathname.includes('conversations')) return [];
  if (pathname.includes('projects')) return { projects: [] };
  if (pathname.includes('attention')) return { items: [] };
  if (pathname.includes('sse')) return {};
  return {};
}

function staticFile(req, res, url) {
  let file = path.join(dist, decodeURIComponent(url.pathname));
  if (url.pathname === '/' || !fs.existsSync(file) || fs.statSync(file).isDirectory()) file = path.join(dist, 'index.html');
  const ext = path.extname(file);
  const type = ext === '.js' ? 'text/javascript' : ext === '.css' ? 'text/css' : ext === '.html' ? 'text/html' : 'application/octet-stream';
  res.writeHead(200, { 'content-type': `${type}; charset=utf-8`, 'cache-control': 'no-store', 'x-candidate-sha': candidateSha });
  fs.createReadStream(file).pipe(res);
}

http.createServer((req, res) => {
  const url = new URL(req.url || '/', `http://${req.headers.host}`);
  requests.push({ method: req.method, path: url.pathname, search: url.search, at: new Date().toISOString() });
  if (url.pathname === '/__head') return json(res, 200, { candidate_sha: candidateSha, served_from: dist, request_count: requests.length });
  if (url.pathname === '/__requests') return json(res, 200, requests);
  if (url.pathname.startsWith('/api/')) return api(req, res, url);
  return staticFile(req, res, url);
}).listen(port, '127.0.0.1', () => {
  console.log(`g5 mock server serving ${candidateSha} at http://127.0.0.1:${port}`);
});

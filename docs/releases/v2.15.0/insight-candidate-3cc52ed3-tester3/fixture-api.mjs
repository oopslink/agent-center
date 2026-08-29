import http from 'node:http';
import { URL } from 'node:url';

const PORT = Number(process.env.PORT || 7110);
const reviewedSha = '3cc52ed300bfe00444d6487aded97e96280d2b16';

const windowEnvelope = {
  window: {
    kind: 'rolling',
    duration: '24h',
    start: '2026-08-28T01:00:00Z',
    end: '2026-08-29T01:00:00Z',
  },
  as_of: '2026-08-29T01:00:00Z',
  refreshed_at: '2026-08-29T01:00:15Z',
  freshness: { state: 'fresh', age_ms: 1200, threshold_ms: 30000 },
};

const staleEnvelope = {
  ...windowEnvelope,
  refreshed_at: '2026-08-28T23:45:00Z',
  freshness: { state: 'stale', age_ms: 5400000, threshold_ms: 30000 },
};

const baseSummary = {
  completed_executions: 12,
  failed_executions: 3,
  failure_rate: 0.25,
  slot_utilization: 0.75,
  slot_coverage_ratio: 0.94,
  queue_wait_ms: { p50: 850, p95: 4200, samples: 9 },
  execution_duration_ms: { p50: 48000, p95: 185000, samples: 12 },
};

const zeroSummary = {
  completed_executions: 2,
  failed_executions: 0,
  failure_rate: 0,
  slot_utilization: 0,
  slot_coverage_ratio: 0.95,
  queue_wait_ms: { p50: 0, p95: 0, samples: 1 },
  execution_duration_ms: { p50: 0, p95: 0, samples: 1 },
};

const unknownSummary = {
  completed_executions: 1,
  failed_executions: 0,
  failure_rate: 0,
  slot_utilization: null,
  slot_coverage_ratio: null,
  queue_wait_ms: { p50: null, p95: null, samples: 0 },
  execution_duration_ms: { p50: null, p95: null, samples: 0 },
};

const partialSummary = {
  ...baseSummary,
  slot_utilization: 0.4,
  slot_coverage_ratio: 0.55,
};

const rows = [
  {
    execution_id: 'exec-ok-single',
    command_id: 'cmd-ok',
    task_id: 'task-ship',
    task_ref: 'T-101',
    task_title: 'Ship Insight drilldown',
    agent_ref: 'agent:builder',
    agent_name: 'Builder Agent',
    project_id: 'proj-alpha',
    project_name: 'Alpha Project',
    worker_id: 'worker-mac-1',
    outcome: 'succeeded',
    failure_reason: null,
    failure_message: null,
    command_status: 'completed',
    status_reason: null,
    status_message: null,
    queued_at: '2026-08-29T00:10:00Z',
    started_at: '2026-08-29T00:10:01Z',
    finished_at: '2026-08-29T00:10:49Z',
    queue_wait_ms: 1000,
    duration_ms: 48000,
    recovered: false,
    quality: 'valid',
  },
  {
    execution_id: 'exec-failed-multi',
    command_id: 'cmd-fail',
    task_id: 'task-fail',
    task_ref: 'T-102',
    task_title: 'Persist execution evidence',
    agent_ref: 'agent:tester',
    agent_name: 'QA Tester',
    project_id: 'proj-beta',
    project_name: 'Beta Project',
    worker_id: 'worker-linux-2',
    outcome: 'failed',
    failure_reason: 'nonzero_exit',
    failure_message: 'Process exited with code 1.',
    command_status: 'started',
    status_reason: null,
    status_message: null,
    queued_at: '2026-08-29T00:15:00Z',
    started_at: '2026-08-29T00:15:03Z',
    finished_at: '2026-08-29T00:16:13Z',
    queue_wait_ms: 3000,
    duration_ms: 70000,
    recovered: false,
    quality: 'valid',
  },
  {
    execution_id: 'exec-running-readable',
    command_id: 'cmd-run',
    task_id: 'task-run',
    task_ref: 'T-103',
    task_title: 'Long running repair',
    agent_ref: 'agent:builder',
    agent_name: 'Builder Agent',
    project_id: 'proj-alpha',
    project_name: 'Alpha Project',
    worker_id: 'worker-mac-1',
    outcome: null,
    failure_reason: null,
    failure_message: null,
    command_status: 'started',
    status_reason: null,
    status_message: null,
    queued_at: '2026-08-29T00:20:00Z',
    started_at: '2026-08-29T00:20:05Z',
    finished_at: null,
    queue_wait_ms: 5000,
    duration_ms: null,
    recovered: false,
    quality: 'valid',
  },
  {
    execution_id: 'exec-rejected-readable',
    command_id: 'cmd-reject',
    task_id: 'task-reject',
    task_ref: 'T-104',
    task_title: 'Capacity rejected job',
    agent_ref: 'agent:tester',
    agent_name: 'QA Tester',
    project_id: 'proj-beta',
    project_name: 'Beta Project',
    worker_id: null,
    outcome: null,
    failure_reason: null,
    failure_message: null,
    command_status: 'rejected',
    status_reason: 'repo_source_unavailable',
    status_message: 'No worker capacity for requested runtime.',
    queued_at: '2026-08-29T00:30:00Z',
    started_at: null,
    finished_at: null,
    queue_wait_ms: null,
    duration_ms: null,
    recovered: false,
    quality: 'valid',
  },
  {
    execution_id: 'exec-invalid-time',
    command_id: 'cmd-invalid',
    task_id: 'task-invalid',
    task_ref: 'T-105',
    task_title: 'Clock skew sample',
    agent_ref: 'agent:future',
    agent_name: null,
    project_id: 'proj-gamma',
    project_name: null,
    worker_id: 'worker-clock',
    outcome: 'new_enum',
    failure_reason: null,
    failure_message: null,
    command_status: 'completed',
    status_reason: null,
    status_message: null,
    queued_at: '2026-08-29T00:40:00Z',
    started_at: '2026-08-29T00:42:00Z',
    finished_at: '2026-08-29T00:41:00Z',
    queue_wait_ms: -1,
    duration_ms: -1,
    recovered: true,
    quality: 'invalid_time_order',
  },
];

function overviewFor(scenario) {
  if (scenario === 'empty') {
    return { ...windowEnvelope, summary: { ...zeroSummary, completed_executions: 0, queue_wait_ms: { p50: null, p95: null, samples: 0 }, execution_duration_ms: { p50: null, p95: null, samples: 0 } }, agents: [], projects: [], diagnostics: { invalid_facts: 0, late_events: 0 } };
  }
  if (scenario === 'single') {
    return { ...windowEnvelope, summary: zeroSummary, agents: [{ agent_ref: 'agent:solo', display_name: 'Solo Agent', summary: zeroSummary }], projects: [{ project_id: 'proj-solo', name: 'Solo Project', summary: zeroSummary }], diagnostics: { invalid_facts: 0, late_events: 0 } };
  }
  if (scenario === 'unknown') {
    return { ...windowEnvelope, summary: unknownSummary, agents: [{ agent_ref: 'agent:unknown', display_name: 'Unknown Capacity Agent', summary: unknownSummary }], projects: [{ project_id: 'proj-unknown', name: 'Unknown Capacity Project', summary: unknownSummary }], diagnostics: { invalid_facts: 0, late_events: 0 } };
  }
  if (scenario === 'stale') {
    return { ...staleEnvelope, summary: partialSummary, agents: [{ agent_ref: 'agent:builder', display_name: 'Builder Agent', summary: partialSummary }], projects: [{ project_id: 'proj-alpha', name: 'Alpha Project', summary: partialSummary }], diagnostics: { invalid_facts: 2, late_events: 1 } };
  }
  return {
    ...windowEnvelope,
    summary: baseSummary,
    agents: [
      { agent_ref: 'agent:builder', display_name: 'Builder Agent', summary: baseSummary },
      { agent_ref: 'agent:tester', display_name: 'QA Tester', summary: { ...baseSummary, completed_executions: 4, failed_executions: 1, failure_rate: 0.25 } },
    ],
    projects: [
      { project_id: 'proj-alpha', name: 'Alpha Project', summary: baseSummary },
      { project_id: 'proj-beta', name: 'Beta Project', summary: { ...baseSummary, completed_executions: 5, failed_executions: 2, failure_rate: 0.4 } },
    ],
    diagnostics: { invalid_facts: 1, late_events: 1 },
  };
}

function executionsFor(searchParams) {
  const scenario = searchParams.get('scenario') || 'multi';
  let filtered = scenario === 'empty' ? [] : scenario === 'single' ? rows.slice(0, 1) : rows;
  const agentRef = searchParams.get('agent_ref');
  const projectId = searchParams.get('project_id');
  if (agentRef) filtered = filtered.filter((row) => row.agent_ref === agentRef);
  if (projectId) filtered = filtered.filter((row) => row.project_id === projectId);
  const envelope = scenario === 'stale' ? staleEnvelope : windowEnvelope;
  return { ...envelope, executions: filtered, next_cursor: filtered.length > 1 && !searchParams.get('cursor') ? 'cursor-next-page' : null };
}

function scenarioFrom(req, url) {
  const explicit = url.searchParams.get('scenario');
  if (explicit) return explicit;
  const referer = req.headers.referer;
  if (!referer) return 'multi';
  try {
    return new URL(referer).searchParams.get('scenario') || 'multi';
  } catch {
    return 'multi';
  }
}

function json(res, status, body) {
  res.writeHead(status, {
    'content-type': 'application/json; charset=utf-8',
    'cache-control': 'no-store',
    'access-control-allow-origin': '*',
  });
  res.end(JSON.stringify(body));
}

function notFound(res) {
  json(res, 404, { error: 'not_found', message: 'fixture route not found' });
}

const server = http.createServer((req, res) => {
  if (req.method === 'OPTIONS') {
    res.writeHead(204, { 'access-control-allow-origin': '*', 'access-control-allow-methods': 'GET,POST,OPTIONS', 'access-control-allow-headers': 'content-type' });
    res.end();
    return;
  }
  const url = new URL(req.url || '/', `http://127.0.0.1:${PORT}`);
  if (url.pathname === '/api/auth/me') return json(res, 200, { identity_id: 'tester3', display_name: 'Tester Three', kind: 'user' });
  if (url.pathname === '/api/orgs') return json(res, 200, [{ id: 'org-acme', slug: 'acme', name: 'Acme Review Org', created_at: '2026-08-29T00:00:00Z', role: 'owner' }]);
  if (url.pathname === '/api/system/version') return json(res, 200, { version: `review-${reviewedSha.slice(0, 8)}`, branch: 'detached', commit: reviewedSha, built_at: '2026-08-29T01:00:00Z' });
  if (url.pathname === '/api/orgs/acme/permissions/effective') return json(res, 200, {
    subject_ref: 'user:tester3',
    resource: { kind: 'org', id: 'org-acme' },
    permissions: [{ key: 'org.member.role.manage', source: 'org_role', evidence_ref: 'fixture' }],
  });
  if (url.pathname === '/api/orgs/acme/attention') return json(res, 200, { items: [] });
  if (url.pathname === '/api/orgs/acme/conversations') return json(res, 200, []);
  if (url.pathname === '/api/orgs/acme/unread-conversations') return json(res, 200, []);
  if (url.pathname === '/api/orgs/acme/projects') return json(res, 200, { projects: [] });
  if (url.pathname === '/api/sse/subscribe' || url.pathname === '/api/sse/unsubscribe') return json(res, 200, { ok: true });
  if (url.pathname === '/api/sse') {
    res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-store', connection: 'keep-alive' });
    res.write('event: sse.heartbeat\ndata: {}\n\n');
    return;
  }
  if (url.pathname === '/api/orgs/acme/insights/overview') return json(res, 200, overviewFor(scenarioFrom(req, url)));
  if (url.pathname === '/api/orgs/acme/insights/executions') {
    const params = new URLSearchParams(url.searchParams);
    params.set('scenario', scenarioFrom(req, url));
    return json(res, 200, executionsFor(params));
  }
  const detail = url.pathname.match(/^\/api\/orgs\/acme\/insights\/executions\/([^/]+)$/);
  if (detail) {
    const row = rows.find((item) => item.execution_id === decodeURIComponent(detail[1]));
    if (!row) return json(res, 404, { error: 'not_found', message: 'execution not found' });
    return json(res, 200, { ...windowEnvelope, execution: row });
  }
  notFound(res);
});

server.listen(PORT, '127.0.0.1', () => {
  console.log(`fixture API listening on http://127.0.0.1:${PORT} for ${reviewedSha}`);
});

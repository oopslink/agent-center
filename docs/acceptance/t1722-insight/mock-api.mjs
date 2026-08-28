import http from 'node:http';

const overview = {
  window: { kind: 'rolling', duration: '24h', start: '2026-08-28T00:00:00Z', end: '2026-08-29T00:00:00Z' },
  as_of: '2026-08-29T00:00:00Z',
  refreshed_at: '2026-08-28T23:57:00Z',
  freshness: { state: 'fresh', age_ms: 180000, threshold_ms: 300000 },
  summary: {
    completed_executions: 128,
    failed_executions: 9,
    failure_rate: 0.07,
    slot_utilization: 0.64,
    slot_coverage_ratio: 0.96,
    queue_wait_ms: { p50: 4800, p95: 42000, samples: 128 },
    execution_duration_ms: { p50: 96000, p95: 620000, samples: 128 },
  },
  agents: [
    {
      agent_ref: 'agent:builder',
      display_name: 'Builder',
      summary: {
        completed_executions: 46,
        failed_executions: 2,
        failure_rate: 0.041,
        slot_utilization: 0.71,
        slot_coverage_ratio: 0.94,
        queue_wait_ms: { p50: 3600, p95: 18000, samples: 46 },
        execution_duration_ms: { p50: 82000, p95: 410000, samples: 46 },
      },
    },
    {
      agent_ref: 'agent:observer',
      display_name: 'Observer',
      summary: {
        completed_executions: 1,
        failed_executions: 0,
        failure_rate: null,
        slot_utilization: 0,
        slot_coverage_ratio: 0.001,
        queue_wait_ms: { p50: null, p95: null, samples: 1 },
        execution_duration_ms: { p50: 7000, p95: 7000, samples: 1 },
      },
    },
  ],
  projects: [
    {
      project_id: 'proj-1',
      name: 'Checkout hardening',
      summary: {
        completed_executions: 64,
        failed_executions: 6,
        failure_rate: 0.094,
        slot_utilization: 0.52,
        slot_coverage_ratio: 0.61,
        queue_wait_ms: { p50: 9300, p95: 67000, samples: 64 },
        execution_duration_ms: { p50: 122000, p95: 780000, samples: 64 },
      },
    },
    {
      project_id: 'proj-empty',
      name: 'No samples',
      summary: {
        completed_executions: 0,
        failed_executions: 0,
        failure_rate: null,
        slot_utilization: null,
        slot_coverage_ratio: null,
        queue_wait_ms: { p50: null, p95: null, samples: 0 },
        execution_duration_ms: { p50: null, p95: null, samples: 0 },
      },
    },
  ],
  diagnostics: { invalid_facts: 2, late_events: 3 },
};

const execution = {
  execution_id: 'exec-24h-1',
  command_id: 'cmd-1',
  task_id: 'task-1',
  task_ref: 'T1722',
  task_title: 'Implement Insight IA',
  agent_ref: 'agent:builder',
  agent_name: 'Builder',
  project_id: 'proj-1',
  project_name: 'Checkout hardening',
  worker_id: 'worker-1',
  outcome: 'failed',
  failure_reason: 'agent_error',
  failure_message: 'Tool command exited with code 1',
  command_status: 'quiet_finalized',
  status_reason: 'worker_recovered',
  status_message: 'Worker recovered and finalized command',
  queued_at: '2026-08-28T23:40:00Z',
  started_at: '2026-08-28T23:41:22Z',
  finished_at: '2026-08-28T23:48:42Z',
  queue_wait_ms: 82000,
  duration_ms: 440000,
  recovered: true,
  quality: 'valid',
};

const executions = {
  window: overview.window,
  as_of: overview.as_of,
  refreshed_at: overview.refreshed_at,
  freshness: overview.freshness,
  executions: [
    execution,
    {
      execution_id: 'exec-24h-2',
      command_id: 'cmd-2',
      task_id: 'task-2',
      task_ref: 'T1723',
      task_title: 'Review rollout',
      agent_ref: 'agent:reviewer',
      agent_name: 'Reviewer',
      project_id: 'proj-1',
      project_name: 'Checkout hardening',
      worker_id: 'worker-2',
      outcome: 'succeeded',
      failure_reason: null,
      failure_message: null,
      command_status: null,
      status_reason: null,
      status_message: null,
      queued_at: '2026-08-28T22:12:00Z',
      started_at: '2026-08-28T22:12:12Z',
      finished_at: '2026-08-28T22:13:06Z',
      queue_wait_ms: 12000,
      duration_ms: 54000,
      recovered: false,
      quality: 'valid',
    },
  ],
  next_cursor: '2026-08-28T22:13:06Z|exec-24h-2',
};

const orgs = [{ id: 'org-1', slug: 'acme', name: 'Acme Ops', created_at: '2026-08-29T00:00:00Z', role: 'owner' }];
const me = { identity_id: 'user-1', display_name: 'Insight Reviewer', kind: 'user' };
const projects = {
  projects: [
    {
      id: 'proj-1',
      name: 'Checkout hardening',
      description: '',
      status: 'active',
      created_at: '2026-08-28T00:00:00Z',
      updated_at: '2026-08-28T00:00:00Z',
    },
  ],
};
const effective = {
  subject_ref: 'user:user-1',
  resource: { kind: 'org', id: 'org-1' },
  permissions: [{ key: 'org.admin', source: 'org_role', evidence_ref: 'owner' }],
};

function send(res, status, body) {
  res.writeHead(status, { 'content-type': 'application/json' });
  res.end(JSON.stringify(body));
}

http
  .createServer((req, res) => {
    const url = new URL(req.url ?? '/', 'http://127.0.0.1:17100');
    if (url.pathname === '/api/auth/me') return send(res, 200, me);
    if (url.pathname === '/api/orgs') return send(res, 200, orgs);
    if (url.pathname === '/api/orgs/acme/projects') return send(res, 200, projects);
    if (url.pathname === '/api/orgs/acme/permissions/effective') return send(res, 200, effective);
    if (url.pathname === '/api/orgs/acme/insights/overview') return send(res, 200, overview);
    if (url.pathname === '/api/orgs/acme/insights/executions') return send(res, 200, executions);
    if (url.pathname === '/api/orgs/acme/insights/executions/exec-24h-1') {
      return send(res, 200, { window: overview.window, as_of: overview.as_of, refreshed_at: overview.refreshed_at, freshness: overview.freshness, execution });
    }
    send(res, 200, []);
  })
  .listen(17100, '127.0.0.1', () => {
    console.log('t1722 mock API listening on http://127.0.0.1:17100');
  });

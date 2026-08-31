import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(__dirname, '../../..');
const screenshots = path.join(__dirname, 'screenshots');
const out = path.join(__dirname, 'derived/browser-results.json');
const session = 'g6-insight-enum-vite-928eae01';
const origin = 'http://127.0.0.1:4173';
const sha = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: repo, encoding: 'utf8' }).trim();
const iso = '2026-08-31T00:00:00Z';
const rawTokens = ['unknown_status', 'raw_future_enum', 'arbitrary_future_token', 'future_outcome', 'future_quality', 'backend_new_kind', 'backend-only', 'new_freshness_state', '"project_id"', '"break_kind"', '[object Object]'];

fs.mkdirSync(screenshots, { recursive: true });
fs.mkdirSync(path.dirname(out), { recursive: true });

function ab(args) {
  return execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8' });
}
function route(pattern, body, status = 200) {
  const payload = status === 200 ? body : { error: body.error || 'error', message: body.message || 'error', ...body };
  ab(['network', 'route', `${origin}${pattern}`, '--body', JSON.stringify(payload)]);
}
function metric(value, over = {}) {
  return { value, meta: { metric_version: 'insight.metrics.v2', sample_count: value ?? 0, coverage: 1, freshness: { state: 'fresh', age_ms: 1000, threshold_ms: 120000 }, unknown_count: 0, known: value !== null, ...over } };
}
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
  meta: metric(1).meta,
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
  execution_count: metric(12),
  failure_rate: null,
  open_issues: metric(3),
  blocked_tasks: metric(1),
  active_plans: metric(2),
  reason_codes: [],
  ...over,
});
const agent = (over = {}) => ({
  id: 'agent:builder',
  name: 'Builder',
  health: { status: 'healthy', reason_codes: [], evidence: [] },
  execution_count: metric(8),
  failure_rate: null,
  open_issues: metric(0),
  blocked_tasks: metric(0),
  active_plans: metric(1),
  reason_codes: [],
  ...over,
});

function installBaseRoutes() {
  ab(['network', 'unroute']).trim();
  route('/api/auth/me', { identity_id: 'user-1', display_name: 'G6 Reviewer', kind: 'user' });
  route('/api/orgs', [{ id: 'org-1', slug: 'acme', name: 'Acme', created_at: iso, role: 'owner' }]);
  route('/api/orgs/acme/attention', { items: [] });
}

function installInsightRoutes(kind) {
  const futureHealth = { status: 'unknown_status', reason_codes: ['coverage_low', 'raw_future_enum'], evidence: [{ raw: 'raw_future_enum' }] };
  const overview = kind === 'empty'
    ? { ...windowEnvelope, summary: { ...summary, completed_executions: 0, failed_executions: 0, failure_rate: null, queue_wait_ms: { p50: null, p95: null, samples: 0 }, execution_duration_ms: { p50: null, p95: null, samples: 0 } }, agents: [], projects: [], diagnostics: { invalid_facts: 0, late_events: 0 } }
    : { ...windowEnvelope, freshness: kind === 'future' ? { state: 'new_freshness_state', age_ms: 1, threshold_ms: 30000 } : windowEnvelope.freshness, summary: kind === 'future' ? { ...summary, slot_utilization: null, slot_coverage_ratio: null } : summary, agents: [{ agent_ref: 'agent:builder', display_name: kind === 'future' ? null : 'Builder', summary }], projects: [{ project_id: 'proj-1', name: kind === 'future' ? null : 'Launch', summary }], diagnostics: { invalid_facts: kind === 'future' ? 1 : 0, late_events: kind === 'future' ? 1 : 0 } };
  route('/api/orgs/acme/insights/overview*', overview);
  const executions = kind === 'empty' ? [] : kind === 'future'
    ? [execution({ execution_id: 'future', outcome: 'future_outcome', failure_reason: 'raw_future_enum', failure_message: null, command_status: 'unknown_status', status_reason: 'arbitrary_future_token', quality: 'future_quality' }), execution({ execution_id: 'nulls', outcome: null, failure_reason: null, command_status: null, status_reason: null, started_at: null, finished_at: null, duration_ms: null })]
    : [execution({ execution_id: 'ok', outcome: 'succeeded', failure_reason: null, failure_message: null, recovered: true }), execution({ execution_id: 'quiet', outcome: 'quiet_finalized', failure_message: null })];
  route('/api/orgs/acme/insights/executions*', { ...windowEnvelope, executions, next_cursor: null });
  route('/api/orgs/acme/insights/executions/*', { ...windowEnvelope, execution: kind === 'future' ? executions[0] : execution({ quality: 'invalid_time_order' }) });
  const projectFixture = kind === 'future' ? project({ name: null, health: futureHealth, reason_codes: ['coverage_low', 'raw_future_enum'], execution_count: metric(null, { known: false, coverage: null, unknown_count: 3 }) }) : project();
  route('/api/orgs/acme/insights/v2/projects*', kind === 'empty' ? [] : [projectFixture]);
  route('/api/orgs/acme/insights/v2/projects/proj-1*', projectFixture);
  const breaks = kind === 'future'
    ? [{ kind: 'backend_new_kind', count: metric(null, { known: false, unknown_count: 1 }), drilldown: { project_id: 'proj-1', break_kind: 'backend_new_kind', status: 'backend-only' } }]
    : ['issue_without_task', 'task_without_plan', 'task_multiple_containers', 'done_plan_non_terminal_task', 'done_plan_open_issue', 'evolution_old_generation_residue', 'delivery_sha_lineage_mismatch'].map((b, i) => ({ kind: b, count: metric(i + 1), drilldown: { project_id: 'proj-1', break_kind: b, status: 'backend-exact' } }));
  route('/api/orgs/acme/insights/v2/projects/proj-1/delivery*', { ...v2Envelope, project_id: 'proj-1', health: kind === 'future' ? futureHealth : { status: 'degraded', reason_codes: ['lineage.integrity_broken'], evidence: [] }, funnel: { issues: metric(9), tasks: metric(8), plans: metric(4), done: metric(1), breaks } });
  route('/api/orgs/acme/insights/v2/projects/proj-1/evolution*', { ...v2Envelope, project_id: 'proj-1', evolution: { plans: 4, evolved_plans: 2, evolution_rate: 0.5, generation_count: 7, rework_count: 1, rework_ratio: 0.25, recovery_attempts: 4, recovery_successes: 3, recovery_effectiveness: 0.75, max_loop_depth: 2, stale_orphan_residue: 1, anomaly_drilldowns: { rework: { project_id: 'proj-1', anomaly_kind: 'rework', source: kind === 'future' ? 'backend-only' : 'backend' }, recovery: { project_id: 'proj-1', anomaly_kind: 'recovery', source: 'backend' }, loop_depth: { project_id: 'proj-1', anomaly_kind: 'loop_depth', source: 'backend' }, residue: { project_id: 'proj-1', anomaly_kind: 'residue', source: 'backend' } } } });
  route('/api/orgs/acme/insights/v2/projects/proj-1/plans/plan-1/lineage*', { ...v2Envelope, project_id: 'proj-1', plan_id: 'plan-1', health: kind === 'future' ? futureHealth : v2Envelope.health, generations: [{ generation: 0, created_at: iso, triggered_by: 'user:owner', reason: 'manual_adjustment', evidence: [{ issue_id: 'issue-1' }], node_changes: [{ retained: 'task-1' }], recovery_duration_ms: 0, recovery_outcome: 'completed', delivery_branch: 'feature/launch', delivery_sha: sha, acceptance_verdict: 'pass' }, { generation: 1, created_at: iso, triggered_by: 'agent:planner', reason: kind === 'future' ? 'arbitrary_future_token' : null, evidence: [{ trigger: 'missing' }], node_changes: [{ added: 'task-2' }], recovery_duration_ms: null, recovery_outcome: kind === 'future' ? 'future_outcome' : null, delivery_branch: '', delivery_sha: '', acceptance_verdict: kind === 'future' ? 'future_outcome' : null }] });
  const agentFixture = kind === 'future' ? agent({ name: null, health: futureHealth, reason_codes: ['coverage_low', 'raw_future_enum'], execution_count: metric(null, { known: false, coverage: null, unknown_count: 4, sample_count: 0 }) }) : agent();
  route('/api/orgs/acme/insights/v2/agents*', kind === 'empty' ? [] : [agentFixture]);
  route('/api/orgs/acme/insights/v2/agents/agent%3Abuilder*', agentFixture);
}

function installErrorRoutes(kind) {
  const body = kind === 'forbidden'
    ? { error: 'forbidden', message: 'no insight permission' }
    : kind === 'rebuilding'
      ? { error: 'insight_rebuilding', message: 'rebuilding', ...windowEnvelope, freshness: { state: 'rebuilding', age_ms: 0, threshold_ms: 30000 } }
      : { error: 'insight_unavailable', message: 'duckdb unavailable', ...windowEnvelope, freshness: { state: 'unavailable', age_ms: 30000, threshold_ms: 30000 } };
  for (const p of [
    '/api/orgs/acme/insights/overview*',
    '/api/orgs/acme/insights/executions*',
    '/api/orgs/acme/insights/v2/projects*',
    '/api/orgs/acme/insights/v2/agents*',
  ]) route(p, body);
}

async function main() {
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
  const results = [];
  ab(['close']);
  ab(['set', 'viewport', '1440', '1000']);
  for (const kind of ['known', 'future']) {
    installBaseRoutes();
    installInsightRoutes(kind);
    for (const [name, routePath] of routes) {
      const url = `${origin}${routePath}`;
      ab(['open', url]);
      ab(['wait', '1600']);
      const shot = path.join(screenshots, `${kind}-${name}-1440.png`);
      ab(['screenshot', '--full', shot]);
      const text = ab(['get', 'text', 'body']);
      results.push({ name, kind, url, screenshot: path.relative(__dirname, shot), leaks: rawTokens.filter((t) => text.includes(t)), text_excerpt: text.slice(0, 1400) });
    }
  }
  for (const [name, routePath, kind] of [
    ['empty-projects', '/organizations/acme/insights/projects', 'empty'],
    ['forbidden-projects', '/organizations/acme/insights/projects', 'forbidden'],
    ['rebuilding-executions', '/organizations/acme/insights/executions?window=24h', 'rebuilding'],
    ['unavailable-execution-detail', '/organizations/acme/insights/executions/exec-24h-1', 'unavailable'],
  ]) {
    installBaseRoutes();
    if (kind === 'empty') installInsightRoutes('empty'); else installErrorRoutes(kind);
    ab(['open', `${origin}${routePath}`]);
    ab(['wait', '1600']);
    const shot = path.join(screenshots, `${name}-state-1440.png`);
    ab(['screenshot', '--full', shot]);
    const text = ab(['get', 'text', 'body']);
    results.push({ name, kind, url: `${origin}${routePath}`, screenshot: path.relative(__dirname, shot), leaks: rawTokens.filter((t) => text.includes(t)), text_excerpt: text.slice(0, 1400) });
  }
  installBaseRoutes();
  installInsightRoutes('future');
  ab(['set', 'viewport', '390', '844']);
  for (const [name, routePath] of [['mobile-overview', '/organizations/acme/insights/overview'], ['mobile-project-detail', '/organizations/acme/insights/projects/proj-1?plan_id=plan-1']]) {
    ab(['open', `${origin}${routePath}`]);
    ab(['wait', '1600']);
    const shot = path.join(screenshots, `${name}-future-390.png`);
    ab(['screenshot', '--full', shot]);
    const text = ab(['get', 'text', 'body']);
    results.push({ name, kind: 'future-mobile', url: `${origin}${routePath}`, screenshot: path.relative(__dirname, shot), leaks: rawTokens.filter((t) => text.includes(t)), text_excerpt: text.slice(0, 1400) });
  }
  const consoleLog = ab(['console']);
  const errors = ab(['errors']);
  const verdict = results.every((r) => r.leaks.length === 0) && errors.trim().length === 0 ? 'PASS' : 'REJECT';
  fs.writeFileSync(out, JSON.stringify({ sha, served_origin: origin, verdict, rawTokens, results, console: consoleLog, errors }, null, 2));
  ab(['close']);
  if (verdict !== 'PASS') process.exitCode = 2;
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});

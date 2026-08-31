import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import i18n from '@/i18n';
import InsightProjectsPage, { InsightPlanLineagePage, InsightProjectDetailPage } from './InsightProjects';

const meta = {
  metric_version: 'insight.metrics.v2',
  sample_count: 4,
  coverage: 1,
  freshness: { state: 'fresh', age_ms: 1000, threshold_ms: 120000 },
  unknown_count: 0,
  known: true,
};

const windowEnvelope = {
  metric_version: 'insight.metrics.v2',
  time_window: {
    kind: 'rolling',
    duration: '24h',
    start: '2026-08-30T00:00:00Z',
    end: '2026-08-31T00:00:00Z',
  },
  as_of: '2026-08-31T00:00:00Z',
  health: { status: 'healthy', reason_codes: [], evidence: [] },
  meta,
};

const project = {
  id: 'proj-1',
  name: 'Launch',
  health: { status: 'unknown', reason_codes: ['coverage_low', 'unknown_source_state'], evidence: [] },
  execution_count: { value: 12, meta: { ...meta, unknown_count: 1 } },
  failure_rate: null,
  open_issues: { value: 3, meta },
  blocked_tasks: { value: 1, meta },
  active_plans: { value: 2, meta },
  reason_codes: ['coverage_low', 'unknown_source_state'],
};

const breakKinds = [
  'delivery_sha_lineage_mismatch',
  'done_plan_non_terminal_task',
  'done_plan_open_issue',
  'evolution_old_generation_residue',
  'issue_without_task',
  'task_multiple_containers',
  'task_without_plan',
];

function renderAt(path: string) {
  window.history.pushState({}, '', path);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/organizations/:slug/insights/projects" element={<InsightProjectsPage />} />
          <Route path="/organizations/:slug/insights/projects/:projectId" element={<InsightProjectDetailPage />} />
          <Route path="/organizations/:slug/insights/projects/:projectId/plans/:planId/lineage" element={<InsightPlanLineagePage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(async () => {
  cleanup();
  await i18n.changeLanguage('en');
});

describe('Insight v2 project pages', () => {
  it('renders the Projects decision list with backend health and reason codes', async () => {
    server.use(http.get('/api/orgs/:slug/insights/v2/projects', ({ request }) => {
      expect(new URL(request.url).searchParams.get('window')).toBe('24h');
      return HttpResponse.json([project]);
    }));

    renderAt('/organizations/acme/insights/projects');

    const row = await screen.findByTestId('insight-project-row');
    expect(row).toHaveAttribute('data-project-id', 'proj-1');
    expect(row).toHaveTextContent('Launch');
    expect(row).toHaveTextContent('Unknown');
    expect(row).toHaveTextContent('Low observation coverage');
    expect(row).toHaveTextContent('Source state is not yet known');
    expect(row).not.toHaveTextContent('coverage_low');
    expect(row).not.toHaveTextContent('unknown_source_state');
    expect(within(row).getByRole('link', { name: 'Launch' })).toHaveAttribute('href', '/organizations/acme/insights/projects/proj-1');
  });

  it('renders an explicit empty state for an empty Projects response', async () => {
    server.use(http.get('/api/orgs/:slug/insights/v2/projects', () => HttpResponse.json([])));
    renderAt('/organizations/acme/insights/projects');
    expect(await screen.findByTestId('insight-projects-empty')).toHaveTextContent('No projects are available');
  });

  it('renders Project detail funnel with exactly seven backend break kinds and stable drilldowns', async () => {
    server.use(
      http.get('/api/orgs/:slug/insights/v2/projects/:projectId', () => HttpResponse.json(project)),
      http.get('/api/orgs/:slug/insights/v2/projects/:projectId/delivery', () => HttpResponse.json({
        ...windowEnvelope,
        project_id: 'proj-1',
        health: { status: 'degraded', reason_codes: ['lineage.integrity_broken'], evidence: [] },
        meta: { ...meta, unknown_count: 7 },
        funnel: {
          issues: { value: 9, meta },
          tasks: { value: 8, meta },
          plans: { value: 4, meta },
          done: { value: 1, meta },
          breaks: breakKinds.map((kind, index) => ({
            kind,
            count: { value: index + 1, meta },
            drilldown: { project_id: 'proj-1', break_kind: kind, status: 'backend-exact' },
          })),
        },
      })),
      http.get('/api/orgs/:slug/insights/v2/projects/:projectId/evolution', () => HttpResponse.json({
        ...windowEnvelope,
        project_id: 'proj-1',
        evolution: {
          plans: 4,
          evolved_plans: 2,
          evolution_rate: 0.5,
          generation_count: 7,
          rework_count: 1,
          rework_ratio: 0.25,
          recovery_attempts: 4,
          recovery_successes: 3,
          recovery_effectiveness: 0.75,
          max_loop_depth: 2,
          stale_orphan_residue: 1,
          anomaly_drilldowns: {
            rework: { project_id: 'proj-1', anomaly_kind: 'rework', source: 'backend' },
            recovery: { project_id: 'proj-1', anomaly_kind: 'recovery', source: 'backend' },
            loop_depth: { project_id: 'proj-1', anomaly_kind: 'loop_depth', source: 'backend' },
            residue: { project_id: 'proj-1', anomaly_kind: 'residue', source: 'backend' },
          },
        },
      })),
    );

    renderAt('/organizations/acme/insights/projects/proj-1?plan_id=plan-1');

    expect(await screen.findByTestId('insight-project-summary')).toHaveTextContent('Runtime health');
    expect(screen.getByTestId('insight-health-panel')).toHaveTextContent('Lineage integrity issue');
    expect(screen.getByTestId('insight-health-panel')).not.toHaveTextContent('lineage.integrity_broken');
    const rows = await screen.findAllByTestId('insight-funnel-break');
    expect(rows).toHaveLength(7);
    expect(rows.map((row) => row.getAttribute('data-break-kind'))).toEqual(breakKinds);
    for (const row of rows) {
      const drilldown = within(row).getByTestId('insight-break-drilldown');
      expect(drilldown).toHaveTextContent('Project: Proj 1');
      expect(drilldown).toHaveTextContent('Break:');
      expect(drilldown).toHaveTextContent('Status: Backend Exact');
      expect(drilldown).not.toHaveTextContent('"project_id"');
      expect(drilldown).not.toHaveTextContent('"break_kind"');
    }
    expect(screen.getByTestId('insight-evolution-panel')).toHaveTextContent('50%');
    expect(screen.getByTestId('insight-evolution-panel')).toHaveTextContent('Rework ratio');
    expect(screen.getByTestId('insight-evolution-panel')).toHaveTextContent('Stale/orphan residue');
    expect(screen.getByTestId('insight-evolution-drilldowns')).toHaveTextContent('Source: Backend');
    expect(screen.getByTestId('insight-evolution-drilldowns')).not.toHaveTextContent('"source"');
    expect(screen.getByRole('link', { name: 'Open lineage' })).toHaveAttribute('href', '/organizations/acme/insights/projects/proj-1/plans/plan-1/lineage');
  });

  it('does not synthesize missing break data or coerce unknown backend break kinds', async () => {
    server.use(
      http.get('/api/orgs/:slug/insights/v2/projects/:projectId', () => HttpResponse.json(project)),
      http.get('/api/orgs/:slug/insights/v2/projects/:projectId/delivery', () => HttpResponse.json({
        ...windowEnvelope,
        project_id: 'proj-1',
        funnel: {
          issues: { value: 9, meta },
          tasks: { value: 8, meta },
          plans: { value: 4, meta },
          done: { value: 1, meta },
          breaks: [{
            kind: 'backend_new_kind',
            count: { value: null, meta: { ...meta, known: false, unknown_count: 1 } },
            drilldown: { project_id: 'proj-1', anomaly: 'backend-only' },
          }],
        },
      })),
      http.get('/api/orgs/:slug/insights/v2/projects/:projectId/evolution', () => HttpResponse.json({
        ...windowEnvelope,
        project_id: 'proj-1',
        evolution: {
          plans: 0,
          evolved_plans: 0,
          evolution_rate: null,
          generation_count: 0,
          rework_count: 0,
          rework_ratio: null,
          recovery_attempts: 0,
          recovery_successes: 0,
          recovery_effectiveness: null,
          max_loop_depth: 0,
          stale_orphan_residue: 0,
          anomaly_drilldowns: { rework: {}, recovery: {}, loop_depth: {}, residue: {} },
        },
      })),
    );

    renderAt('/organizations/acme/insights/projects/proj-1');

    const rows = await screen.findAllByTestId('insight-funnel-break');
    expect(rows).toHaveLength(1);
    expect(rows[0]).toHaveAttribute('data-break-kind', 'backend_new_kind');
    expect(rows[0]).toHaveTextContent('Backend-defined break');
    expect(rows[0]).not.toHaveTextContent('backend_new_kind');
    expect(within(rows[0]).getByTestId('insight-break-drilldown')).toHaveTextContent('Anomaly: Backend Only');
    expect(within(rows[0]).getByTestId('insight-break-drilldown')).not.toHaveTextContent('"anomaly"');
    expect(within(rows[0]).getByTestId('insight-break-drilldown')).not.toHaveTextContent('break_kind');
    expect(within(rows[0]).getByText('—')).toBeInTheDocument();
  });

  it('renders stale and low-coverage backend confidence without inferring health', async () => {
    server.use(
      http.get('/api/orgs/:slug/insights/v2/projects/:projectId', () => HttpResponse.json({
        ...project,
        health: { status: 'unknown', reason_codes: ['coverage_low', 'freshness_stale'], evidence: [] },
      })),
      http.get('/api/orgs/:slug/insights/v2/projects/:projectId/delivery', () => HttpResponse.json({
        ...windowEnvelope,
        health: { status: 'unknown', reason_codes: ['coverage_low', 'freshness_stale'], evidence: [] },
        meta: {
          ...meta,
          coverage: 0.4,
          freshness: { state: 'stale', age_ms: 300000, threshold_ms: 120000 },
          known: false,
        },
        project_id: 'proj-1',
        funnel: {
          issues: { value: null, meta: { ...meta, known: false, coverage: 0.4 } },
          tasks: { value: null, meta: { ...meta, known: false, coverage: 0.4 } },
          plans: { value: null, meta: { ...meta, known: false, coverage: 0.4 } },
          done: { value: null, meta: { ...meta, known: false, coverage: 0.4 } },
          breaks: [],
        },
      })),
      http.get('/api/orgs/:slug/insights/v2/projects/:projectId/evolution', () => HttpResponse.json({
        ...windowEnvelope,
        project_id: 'proj-1',
        evolution: {
          plans: 0,
          evolved_plans: 0,
          evolution_rate: null,
          generation_count: 0,
          rework_count: 0,
          rework_ratio: null,
          recovery_attempts: 0,
          recovery_successes: 0,
          recovery_effectiveness: null,
          max_loop_depth: 0,
          stale_orphan_residue: 0,
          anomaly_drilldowns: { rework: {}, recovery: {}, loop_depth: {}, residue: {} },
        },
      })),
    );

    renderAt('/organizations/acme/insights/projects/proj-1');

    expect(await screen.findByTestId('insight-v2-window')).toHaveTextContent('Data delayed');
    expect(screen.getByTestId('insight-health-panel')).toHaveTextContent('Unknown');
    expect(screen.getByTestId('insight-health-panel')).toHaveTextContent('coverage 40%');
    expect(screen.getByTestId('insight-health-panel')).toHaveTextContent('Low observation coverage');
    expect(screen.getByTestId('insight-health-panel')).not.toHaveTextContent('coverage_low');
    expect(screen.getByTestId('insight-delivery-funnel')).toHaveTextContent('—');
  });

  it('renders lineage generations and flags anomalies without hiding unknown backend values', async () => {
    server.use(http.get('/api/orgs/:slug/insights/v2/projects/:projectId/plans/:planId/lineage', () => HttpResponse.json({
      ...windowEnvelope,
      project_id: 'proj-1',
      plan_id: 'plan-1',
      health: { status: 'unknown', reason_codes: ['lineage.integrity_broken'], evidence: [{ reason: 'missing_sha' }] },
      generations: [
        {
          generation: 0,
          created_at: '2026-08-30T01:00:00Z',
          triggered_by: 'user:owner',
          reason: 'manual_adjustment',
          evidence: [{ issue_id: 'issue-1' }],
          node_changes: [{ retained: 'task-1' }],
          recovery_duration_ms: 0,
          recovery_outcome: 'completed',
          delivery_branch: 'feature/launch',
          delivery_sha: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
          acceptance_verdict: 'pass',
        },
        {
          generation: 1,
          created_at: '2026-08-30T02:00:00Z',
          triggered_by: 'agent:planner',
          reason: 'unknown',
          evidence: [{ trigger: 'missing' }],
          node_changes: [{ added: 'task-2' }],
          recovery_duration_ms: null,
          recovery_outcome: 'unknown',
          delivery_branch: '',
          delivery_sha: '',
          acceptance_verdict: 'reject',
        },
      ],
    })));

    renderAt('/organizations/acme/insights/projects/proj-1/plans/plan-1/lineage');

    expect(await screen.findByTestId('insight-v2-window')).toHaveTextContent('Past 24 hours');
    const generations = screen.getAllByTestId('insight-lineage-generation');
    expect(generations).toHaveLength(2);
    expect(generations[0]).toHaveAttribute('data-generation', 'G0');
    expect(generations[0]).toHaveTextContent('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa');
    expect(generations[1]).toHaveAttribute('data-generation', 'G1');
    expect(generations[1]).toHaveTextContent('Lineage anomaly');
    expect(generations[1]).toHaveTextContent('Unknown');
    expect(generations[1]).toHaveTextContent('Reject');
  });

  it('renders auth and Chinese states independently', async () => {
    server.use(http.get('/api/orgs/:slug/insights/v2/projects', () => HttpResponse.json({ error: 'forbidden', message: 'no insight permission' }, { status: 403 })));
    renderAt('/organizations/acme/insights/projects');
    expect(await screen.findByTestId('insight-projects-error')).toHaveTextContent('permission');
    cleanup();

    await i18n.changeLanguage('zh');
    server.use(http.get('/api/orgs/:slug/insights/v2/projects', () => HttpResponse.json([project])));
    renderAt('/organizations/acme/insights/projects');
    expect(await screen.findByRole('heading', { name: 'Insight 项目' })).toBeInTheDocument();
    expect(await screen.findByTestId('insight-projects-table')).toHaveTextContent('健康');
  });
});

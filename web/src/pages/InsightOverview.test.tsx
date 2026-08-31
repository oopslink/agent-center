import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import i18n from '@/i18n';
import { server } from '@/test/mswServer';
import InsightOverview, { InsightAgentsPage, InsightExecutionDetailPage, InsightExecutionsPage, InsightProjectsPage } from './InsightOverview';

const windowEnvelope = {
  window: {
    kind: 'rolling',
    duration: '24h',
    start: '2026-08-26T00:00:00Z',
    end: '2026-08-27T00:00:00Z',
  },
  as_of: '2026-08-27T00:00:00Z',
  refreshed_at: '2026-08-27T00:00:01Z',
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

const execution = {
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
  queued_at: '2026-08-26T23:00:00Z',
  started_at: '2026-08-26T23:00:01Z',
  finished_at: '2026-08-26T23:00:06Z',
  queue_wait_ms: 1000,
  duration_ms: 5000,
  recovered: false,
  quality: 'valid',
};

function overview(overrides: Record<string, unknown> = {}) {
  return {
    ...windowEnvelope,
    summary,
    agents: [{ agent_ref: 'agent:builder', display_name: 'Builder', summary }],
    projects: [{ project_id: 'proj-1', name: 'Launch', summary }],
    diagnostics: { invalid_facts: 0, late_events: 0 },
    ...overrides,
  };
}

function renderAt(path: string) {
  window.history.pushState({}, '', path);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/organizations/:slug/insights/overview" element={<InsightOverview />} />
          <Route path="/organizations/:slug/insights/agents" element={<InsightAgentsPage />} />
          <Route path="/organizations/:slug/insights/projects" element={<InsightProjectsPage />} />
          <Route path="/organizations/:slug/insights/executions" element={<InsightExecutionsPage />} />
          <Route path="/organizations/:slug/insights/executions/:executionId" element={<InsightExecutionDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(async () => {
  cleanup();
  await i18n.changeLanguage('en');
});

describe('Insight pages', () => {
  it('renders overview metrics and links agent/project drilldowns to the routed execution list', async () => {
    server.use(http.get('/api/orgs/:slug/insights/overview', ({ request }) => {
      expect(new URL(request.url).searchParams.get('window')).toBe('24h');
      return HttpResponse.json(overview());
    }));

    renderAt('/organizations/acme/insights/overview');

    expect(await screen.findByTestId('insight-window')).toHaveTextContent('Past 24 hours (rolling)');
    expect(screen.getByTestId('insight-summary')).toHaveTextContent('Completed executions');
    expect(screen.getByTestId('insight-summary')).toHaveTextContent('75%');
    expect(screen.queryByTestId('insight-drilldown')).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'View all executions' })).toHaveAttribute('href', '/organizations/acme/insights/executions?window=24h');
    expect(within(screen.getByTestId('insight-agent-table')).getByRole('link', { name: 'View executions' })).toHaveAttribute('href', '/organizations/acme/insights/executions?window=24h&agent_ref=agent%3Abuilder');
    expect(within(screen.getByTestId('insight-project-table')).getByRole('link', { name: 'View executions' })).toHaveAttribute('href', '/organizations/acme/insights/executions?window=24h&project_id=proj-1');
  });

  it('renders agent and project Insight nav pages with drilldowns into task executions', async () => {
    server.use(http.get('/api/orgs/:slug/insights/overview', () => HttpResponse.json(overview())));

    renderAt('/organizations/acme/insights/agents');
    expect(screen.getByTestId('page-InsightAgents')).toHaveTextContent('Insight agents');
    const agentTable = await screen.findByTestId('insight-agent-table');
    expect(agentTable).toHaveTextContent('Builder');
    expect(within(agentTable).getByRole('link', { name: 'View executions' })).toHaveAttribute('href', '/organizations/acme/insights/executions?window=24h&agent_ref=agent%3Abuilder');
    cleanup();

    renderAt('/organizations/acme/insights/projects');
    expect(screen.getByTestId('page-InsightProjects')).toHaveTextContent('Insight projects');
    const projectTable = await screen.findByTestId('insight-project-table');
    expect(projectTable).toHaveTextContent('Launch');
    expect(within(projectTable).getByRole('link', { name: 'View executions' })).toHaveAttribute('href', '/organizations/acme/insights/executions?window=24h&project_id=proj-1');
  });

  it('treats null, zero, low, partial, and representative coverage without guessing missing data as zero', async () => {
    const cases = [
      { slot_coverage_ratio: null, slot_utilization: null, want: 'Cannot determine' },
      { slot_coverage_ratio: 0, slot_utilization: 0, want: 'Cannot determine' },
      { slot_coverage_ratio: 0.001, slot_utilization: 0, want: 'Insufficient data' },
      { slot_coverage_ratio: 0.499, slot_utilization: 0.3, want: 'Insufficient data' },
      { slot_coverage_ratio: 0.5, slot_utilization: 0, want: '0% (partial observation)' },
      { slot_coverage_ratio: 0.899, slot_utilization: 0.4, want: '40% (partial observation)' },
      { slot_coverage_ratio: 0.9, slot_utilization: 0, want: '0%' },
    ];
    for (const item of cases) {
      server.use(http.get('/api/orgs/:slug/insights/overview', () => HttpResponse.json(overview({ summary: { ...summary, ...item } }))));
      renderAt('/organizations/acme/insights/overview');
      expect(await screen.findByTestId('insight-utilization-card')).toHaveTextContent(item.want);
      if (item.slot_coverage_ratio === 0.001) {
        expect(screen.getByTestId('insight-utilization-card')).toHaveTextContent('Observation coverage 0.1%');
      }
      cleanup();
    }
  });

  it('loads execution list from URL filters, preserves cursor context, and removes chips', async () => {
    const seen: string[] = [];
    server.use(http.get('/api/orgs/:slug/insights/executions', ({ request }) => {
      seen.push(request.url);
      return HttpResponse.json({ ...windowEnvelope, executions: [execution], next_cursor: 'next-opaque' });
    }));

    renderAt('/organizations/acme/insights/executions?window=24h&agent_ref=agent%3Abuilder&project_id=proj-1&cursor=old');

    expect(await screen.findByTestId('insight-execution-row')).toHaveTextContent('Ship UI');
    let params = new URL(seen[0]).searchParams;
    expect(params.get('window')).toBe('24h');
    expect(params.get('agent_ref')).toBe('agent:builder');
    expect(params.get('project_id')).toBe('proj-1');
    expect(params.get('cursor')).toBe('old');

    fireEvent.click(screen.getByRole('button', { name: /Agent: agent:builder/ }));
    await waitFor(() => expect(seen.length).toBe(2));
    params = new URL(seen[1]).searchParams;
    expect(params.get('agent_ref')).toBeNull();
    expect(params.get('project_id')).toBe('proj-1');
    expect(params.get('cursor')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    await waitFor(() => expect(seen.length).toBe(3));
    expect(new URL(seen[2]).searchParams.get('cursor')).toBe('next-opaque');
  });

  it('maps execution status, recovery, quality, and reasons without exposing raw enums in the main row', async () => {
    const rows = [
      { ...execution, execution_id: 'ok', outcome: 'succeeded', failure_reason: null, failure_message: null, recovered: true },
      { ...execution, execution_id: 'crash', outcome: 'crashed', failure_message: null },
      { ...execution, execution_id: 'quiet', outcome: 'quiet_finalized', failure_message: null },
      { ...execution, execution_id: 'running', outcome: null, finished_at: null, duration_ms: null },
      { ...execution, execution_id: 'rejected', outcome: null, started_at: null, finished_at: null, command_status: 'rejected', status_message: 'No capacity', duration_ms: null },
      { ...execution, execution_id: 'bad-time', quality: 'invalid_time_order' },
      { ...execution, execution_id: 'future', outcome: 'new_enum', quality: 'future_quality' },
    ];
    server.use(http.get('/api/orgs/:slug/insights/executions', () => HttpResponse.json({ ...windowEnvelope, executions: rows, next_cursor: null })));

    renderAt('/organizations/acme/insights/executions?window=24h');

    const table = await screen.findByTestId('insight-execution-table');
    expect(table).toHaveTextContent('Completed');
    expect(table).toHaveTextContent('Interrupted');
    expect(table).toHaveTextContent('Ended during recovery');
    expect(table).toHaveTextContent('Running');
    expect(table).toHaveTextContent('Did not start');
    expect(table).toHaveTextContent('Invalid time data');
    expect(table).toHaveTextContent('Data needs review');
    expect(table).not.toHaveTextContent('quiet_finalized');
    expect(table).not.toHaveTextContent('invalid_time_order');
  });

  it('renders object detail, failure_message, fallback reason, invalid quality, and not-found state', async () => {
    server.use(http.get('/api/orgs/:slug/insights/executions/:executionId', ({ params }) => {
      if (params.executionId === 'missing') return HttpResponse.json({ error: 'not_found', message: 'execution not found' }, { status: 404 });
      return HttpResponse.json({ ...windowEnvelope, execution: { ...execution, quality: 'invalid_time_order' } });
    }));

    renderAt('/organizations/acme/insights/executions/exec-24h-1');

    const detail = await screen.findByTestId('insight-execution-detail');
    expect(detail).toHaveTextContent('Execution timeline');
    expect(detail).toHaveTextContent('Process exited with code 1.');
    expect(detail).toHaveTextContent('Invalid time data');
    expect(detail).toHaveTextContent('Execution ID');
    cleanup();

    renderAt('/organizations/acme/insights/executions/missing');
    expect(await screen.findByTestId('insight-execution-not-found')).toHaveTextContent('This TaskExecution was not found');
  });

  it('renders rebuilding/auth errors explicitly and supports Chinese copy', async () => {
    server.use(http.get('/api/orgs/:slug/insights/overview', () =>
      HttpResponse.json({ error: 'insight_rebuilding', message: 'rebuilding', ...windowEnvelope, freshness: { state: 'rebuilding', age_ms: 0, threshold_ms: 30000 } }, { status: 503 }),
    ));
    renderAt('/organizations/acme/insights/overview');
    expect(await screen.findByTestId('insight-rebuilding')).toHaveTextContent('Insight is rebuilding');
    cleanup();

    server.use(http.get('/api/orgs/:slug/insights/overview', () => HttpResponse.json({ error: 'forbidden', message: 'no insight permission' }, { status: 403 })));
    renderAt('/organizations/acme/insights/overview');
    expect(await screen.findByTestId('insight-auth-error')).toHaveTextContent('permission');
    cleanup();

    await i18n.changeLanguage('zh');
    server.use(http.get('/api/orgs/:slug/insights/overview', () => HttpResponse.json(overview())));
    renderAt('/organizations/acme/insights/overview');
    expect(await screen.findByTestId('insight-window')).toHaveTextContent('过去 24 小时');
    expect(screen.getByRole('link', { name: '查看全部执行记录' })).toBeInTheDocument();
  });
});

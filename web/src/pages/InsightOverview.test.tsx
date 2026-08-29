import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import InsightOverview, { InsightExecutionDetailPage } from './InsightOverview';

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
  execution_duration_ms: { p50: 5000, p95: 12000, samples: 10 },
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
  failure_reason: 'exit 1',
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

function renderOverview() {
  window.history.pushState({}, '', '/organizations/acme/insights/overview');
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/organizations/acme/insights/overview']}>
        <Routes>
          <Route path="/organizations/:slug/insights/overview" element={<InsightOverview />} />
          <Route path="/organizations/:slug/insights/executions/:executionId" element={<InsightExecutionDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function renderExecutionDetail({
  slug = 'acme',
  executionId = 'exec-24h-1',
  qc = new QueryClient({ defaultOptions: { queries: { retry: false } } }),
}: {
  slug?: string;
  executionId?: string;
  qc?: QueryClient;
} = {}) {
  window.history.pushState({}, '', `/organizations/${slug}/insights/executions/${executionId}`);
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/organizations/${slug}/insights/executions/${executionId}`]}>
        <Routes>
          <Route path="/organizations/:slug/insights/executions/:executionId" element={<InsightExecutionDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('InsightOverview page', () => {
  it('renders authoritative 24h metrics and opens agent drilldown with exact agent_ref', async () => {
    let executionUrl = '';
    server.use(
      http.get('/api/orgs/:slug/insights/overview', ({ request }) => {
        expect(new URL(request.url).searchParams.get('window')).toBe('24h');
        return HttpResponse.json(overview());
      }),
      http.get('/api/orgs/:slug/insights/executions', ({ request }) => {
        executionUrl = request.url;
        return HttpResponse.json({ ...windowEnvelope, executions: [execution], next_cursor: null });
      }),
    );

    renderOverview();

    expect(await screen.findByTestId('insight-window')).toHaveTextContent('Past 24 hours');
    expect(screen.getByTestId('insight-freshness')).toHaveTextContent('fresh');
    expect(screen.getByTestId('insight-summary')).toHaveTextContent('10');
    expect(screen.getByTestId('insight-summary')).toHaveTextContent('20%');
    expect(screen.getByTestId('insight-summary')).toHaveTextContent('Coverage 90%');

    fireEvent.click(screen.getByRole('button', { name: 'Builder' }));

    const row = await screen.findByTestId('insight-execution-row');
    expect(row).toHaveTextContent('exec-24h-1');
    expect(row).toHaveTextContent('Ship UI');
    expect(within(row).getByRole('link', { name: 'exec-24h-1' })).toHaveAttribute('href', '/organizations/acme/insights/executions/exec-24h-1');
    expect(new URL(executionUrl).searchParams.get('agent_ref')).toBe('agent:builder');
    expect(new URL(executionUrl).searchParams.get('window')).toBe('24h');
  });

  it('opens project drilldown with exact project_id and no provenance filters', async () => {
    let executionUrl = '';
    server.use(
      http.get('/api/orgs/:slug/insights/overview', () => HttpResponse.json(overview())),
      http.get('/api/orgs/:slug/insights/executions', ({ request }) => {
        executionUrl = request.url;
        return HttpResponse.json({ ...windowEnvelope, executions: [], next_cursor: null });
      }),
    );

    renderOverview();
    fireEvent.click(await screen.findByRole('button', { name: 'Launch' }));

    await waitFor(() => expect(executionUrl).toContain('/insights/executions'));
    const params = new URL(executionUrl).searchParams;
    expect(params.get('project_id')).toBe('proj-1');
    expect(params.get('agent_ref')).toBeNull();
    expect(params.get('provenance')).toBeNull();
  });

  it('renders empty denominators without inventing zero rates', async () => {
    const emptySummary = {
      completed_executions: 0,
      failed_executions: 0,
      failure_rate: null,
      slot_utilization: null,
      slot_coverage_ratio: null,
      queue_wait_ms: { p50: null, p95: null, samples: 0 },
      execution_duration_ms: { p50: null, p95: null, samples: 0 },
    };
    server.use(
      http.get('/api/orgs/:slug/insights/overview', () =>
        HttpResponse.json(overview({ summary: emptySummary, agents: [], projects: [] })),
      ),
    );

    renderOverview();

    expect(await screen.findByTestId('insight-empty')).toHaveTextContent('No executions');
    expect(screen.getByTestId('insight-summary')).toHaveTextContent('Failure rate');
    expect(screen.getByTestId('insight-summary')).toHaveTextContent('-');
  });

  it('shows stale overview and stale empty drilldown states', async () => {
    const staleEnvelope = {
      ...windowEnvelope,
      freshness: { state: 'stale', age_ms: 61000, threshold_ms: 30000 },
    };
    server.use(
      http.get('/api/orgs/:slug/insights/overview', () => HttpResponse.json(overview(staleEnvelope))),
      http.get('/api/orgs/:slug/insights/executions', () => HttpResponse.json({ ...staleEnvelope, executions: [], next_cursor: null })),
    );

    renderOverview();

    expect(await screen.findByTestId('insight-stale')).toHaveTextContent('Stale data');
    fireEvent.click(screen.getByRole('button', { name: 'Launch' }));
    expect(await screen.findByTestId('insight-drilldown-stale')).toBeInTheDocument();
    expect(screen.getByTestId('insight-drilldown-empty')).toBeInTheDocument();
  });

  it('opens a pre-start command row through an encoded TaskExecution detail request', async () => {
    const preStart = {
      ...execution,
      execution_id: 'command:cmd-prestart',
      command_id: 'cmd-prestart',
      task_title: 'Queued prestart',
      outcome: null,
      started_at: null,
      finished_at: null,
      queue_wait_ms: null,
      duration_ms: null,
    };
    const detailUrls: string[] = [];
    server.use(
      http.get('/api/orgs/:slug/insights/overview', () => HttpResponse.json(overview())),
      http.get('/api/orgs/:slug/insights/executions', ({ request }) => {
        expect(new URL(request.url).searchParams.get('agent_ref')).toBe('agent:builder');
        return HttpResponse.json({ ...windowEnvelope, executions: [preStart], next_cursor: null });
      }),
      http.get('/api/orgs/:slug/insights/executions/:executionId', ({ params, request }) => {
        detailUrls.push(request.url);
        expect(params.executionId).toBe('command:cmd-prestart');
        return HttpResponse.json({ ...windowEnvelope, execution: preStart });
      }),
    );

    renderOverview();
    fireEvent.click(await screen.findByRole('button', { name: 'Builder' }));
    fireEvent.click(await screen.findByRole('link', { name: 'command:cmd-prestart' }));

    expect(await screen.findByTestId('insight-execution-detail')).toHaveTextContent('Queued prestart');
    expect(detailUrls).toHaveLength(1);
    expect(new URL(detailUrls[0]).pathname).toBe('/api/orgs/acme/insights/executions/command%3Acmd-prestart');
  });

  it('shows auth, loading, and unavailable states', async () => {
    server.use(
      http.get('/api/orgs/:slug/insights/overview', async () => {
        await new Promise((resolve) => setTimeout(resolve, 50));
        return HttpResponse.json({ error: 'forbidden', message: 'no insight permission' }, { status: 403 });
      }),
    );

    renderOverview();

    expect(screen.getByTestId('insight-loading')).toHaveTextContent('Loading');
    expect(await screen.findByTestId('insight-auth-error')).toHaveTextContent('not authorized');
  });

  it('shows rebuilding envelopes from the backend distinctly', async () => {
    server.use(
      http.get('/api/orgs/:slug/insights/overview', () =>
        HttpResponse.json(
          {
            error: 'insight_rebuilding',
            message: 'rebuilding',
            ...windowEnvelope,
            freshness: { state: 'rebuilding', age_ms: 0, threshold_ms: 30000 },
          },
          { status: 503 },
        ),
      ),
    );

    renderOverview();

    expect(await screen.findByTestId('insight-rebuilding')).toHaveTextContent('rebuilding');
  });

  it('loads single execution detail from the real detail route', async () => {
    let detailUrl = '';
    server.use(
      http.get('/api/orgs/:slug/insights/executions/:executionId', ({ request, params }) => {
        detailUrl = request.url;
        expect(params.executionId).toBe('exec-24h-1');
        return HttpResponse.json({ ...windowEnvelope, execution });
      }),
    );

    renderExecutionDetail();

    expect(screen.getByTestId('insight-execution-loading')).toHaveTextContent('Loading execution detail');
    expect(await screen.findByTestId('insight-execution-detail')).toHaveTextContent('exec-24h-1');
    expect(screen.getByTestId('insight-execution-detail')).toHaveTextContent('Ship UI');
    expect(new URL(detailUrl).searchParams.get('window')).toBe('24h');
  });

  it('refreshes single execution detail explicitly', async () => {
    let calls = 0;
    server.use(
      http.get('/api/orgs/:slug/insights/executions/:executionId', () => {
        calls += 1;
        return HttpResponse.json({
          ...windowEnvelope,
          execution: { ...execution, outcome: calls === 1 ? 'running' : 'succeeded' },
        });
      }),
    );

    renderExecutionDetail();

    expect(await screen.findByTestId('insight-execution-detail')).toHaveTextContent('running');
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    expect(await screen.findByTestId('insight-execution-detail')).toHaveTextContent('succeeded');
    expect(calls).toBe(2);
  });

  it('renders 404 as a dedicated not-found state', async () => {
    server.use(
      http.get('/api/orgs/:slug/insights/executions/:executionId', () =>
        HttpResponse.json({ error: 'not_found', message: 'execution not found' }, { status: 404 }),
      ),
    );

    renderExecutionDetail({ executionId: 'missing-exec' });

    expect(await screen.findByTestId('insight-execution-not-found')).toHaveTextContent('Execution not found');
    expect(screen.queryByText('Execution detail failed')).not.toBeInTheDocument();
    expect(screen.queryByTestId('insight-execution-detail')).not.toBeInTheDocument();
  });

  it('keeps non-404 detail failures on the generic error state', async () => {
    server.use(
      http.get('/api/orgs/:slug/insights/executions/:executionId', () =>
        HttpResponse.json({ error: 'insight_unavailable', message: 'duckdb offline' }, { status: 503 }),
      ),
    );

    renderExecutionDetail();

    expect(await screen.findByTestId('insight-execution-error')).toHaveTextContent('Execution detail failed');
    expect(screen.getByTestId('insight-execution-error')).toHaveTextContent('duckdb offline');
  });

  it('does not reuse single execution detail across organizations', async () => {
    const seen: string[] = [];
    server.use(
      http.get('/api/orgs/:slug/insights/executions/:executionId', ({ params }) => {
        seen.push(String(params.slug));
        return HttpResponse.json({
          ...windowEnvelope,
          execution: {
            ...execution,
            execution_id: String(params.executionId),
            project_name: params.slug === 'acme' ? 'Acme Launch' : 'Beta Launch',
          },
        });
      }),
    );
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    const first = renderExecutionDetail({ slug: 'acme', qc });
    expect(await screen.findByTestId('insight-execution-detail')).toHaveTextContent('Acme Launch');
    first.unmount();
    cleanup();

    renderExecutionDetail({ slug: 'beta', qc });

    expect(await screen.findByTestId('insight-execution-detail')).toHaveTextContent('Beta Launch');
    expect(seen).toEqual(['acme', 'beta']);
  });
});

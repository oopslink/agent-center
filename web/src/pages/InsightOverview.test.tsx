import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import InsightOverview from './InsightOverview';

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

function overview(overrides: Record<string, unknown> = {}) {
  return {
    ...windowEnvelope,
    summary,
    agents: [
      { agent_ref: 'agent:builder', display_name: 'Builder', summary },
    ],
    projects: [
      { project_id: 'proj-1', name: 'Launch', summary },
    ],
    diagnostics: { invalid_facts: 0, late_events: 0 },
    ...overrides,
  };
}

function renderPage() {
  window.history.pushState({}, '', '/organizations/acme/insights/overview');
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/organizations/acme/insights/overview']}>
        <InsightOverview />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('InsightOverview page', () => {
  it('renders authoritative 24h overview metrics and opens agent drilldown by execution_id', async () => {
    let executionUrl = '';
    server.use(
      http.get('/api/orgs/:slug/insights/overview', ({ request }) => {
        expect(new URL(request.url).searchParams.get('window')).toBe('24h');
        return HttpResponse.json(overview());
      }),
      http.get('/api/orgs/:slug/insights/executions', ({ request }) => {
        executionUrl = request.url;
        return HttpResponse.json({
          ...windowEnvelope,
          executions: [
            {
              execution_id: 'exec-24h-1',
              command_id: 'cmd-1',
              task_id: 'task-1',
              task_ref: 'T1',
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
            },
          ],
          next_cursor: null,
        });
      }),
    );

    renderPage();

    expect(await screen.findByTestId('insight-window')).toHaveTextContent('Past 24 hours');
    expect(screen.getByTestId('insight-freshness')).toHaveTextContent('fresh');
    expect(screen.getByTestId('insight-summary')).toHaveTextContent('10');
    expect(screen.getByTestId('insight-summary')).toHaveTextContent('20%');
    expect(screen.getByTestId('insight-summary')).toHaveTextContent('Coverage 90%');

    fireEvent.click(screen.getByRole('button', { name: 'Builder' }));

    const row = await screen.findByTestId('insight-execution-row');
    expect(row).toHaveTextContent('exec-24h-1');
    expect(row).toHaveTextContent('Ship UI');
    expect(new URL(executionUrl).searchParams.get('agent_ref')).toBe('agent:builder');
    expect(new URL(executionUrl).searchParams.get('window')).toBe('24h');
  });

  it('renders empty denominators distinctly without inventing zero rates', async () => {
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

    renderPage();

    expect(await screen.findByTestId('insight-empty')).toHaveTextContent('No executions');
    expect(screen.getByTestId('insight-summary')).toHaveTextContent('Failure rate');
    expect(screen.getByTestId('insight-summary')).toHaveTextContent('—');
  });

  it('shows stale overview and stale drilldown as visible states', async () => {
    const staleEnvelope = {
      ...windowEnvelope,
      freshness: { state: 'stale', age_ms: 61000, threshold_ms: 30000 },
    };
    server.use(
      http.get('/api/orgs/:slug/insights/overview', () =>
        HttpResponse.json(overview(staleEnvelope)),
      ),
      http.get('/api/orgs/:slug/insights/executions', () =>
        HttpResponse.json({ ...staleEnvelope, executions: [], next_cursor: null }),
      ),
    );

    renderPage();

    expect(await screen.findByTestId('insight-stale')).toHaveTextContent('Stale data');
    fireEvent.click(screen.getByRole('button', { name: 'Launch' }));
    expect(await screen.findByTestId('insight-drilldown-stale')).toBeInTheDocument();
    expect(screen.getByTestId('insight-drilldown-empty')).toBeInTheDocument();
  });

  it('shows authorization errors separately from unavailable errors', async () => {
    server.use(
      http.get('/api/orgs/:slug/insights/overview', () =>
        HttpResponse.json({ error: 'forbidden', message: 'no insight permission' }, { status: 403 }),
      ),
    );

    renderPage();

    expect(await screen.findByTestId('insight-auth-error')).toHaveTextContent('not authorized');
  });

  it('shows loading state before the overview resolves', async () => {
    server.use(
      http.get('/api/orgs/:slug/insights/overview', async () => {
        await new Promise((resolve) => setTimeout(resolve, 50));
        return HttpResponse.json(overview());
      }),
    );

    renderPage();

    expect(screen.getByTestId('insight-loading')).toHaveTextContent('Loading');
    expect(await screen.findByTestId('insight-summary')).toBeInTheDocument();
  });

  it('keeps rebuilding and unavailable 503 states distinct when the backend returns a freshness envelope', async () => {
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

    renderPage();

    expect(await screen.findByTestId('insight-rebuilding')).toHaveTextContent('rebuilding');
  });

  it('shows generic unavailable failures distinctly', async () => {
    server.use(
      http.get('/api/orgs/:slug/insights/overview', () =>
        HttpResponse.json({ error: 'insight_unavailable', message: 'duckdb offline' }, { status: 503 }),
      ),
    );

    renderPage();

    const panel = await screen.findByTestId('insight-error');
    expect(within(panel).getByText('Insight overview failed')).toBeInTheDocument();
  });
});

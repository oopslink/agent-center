import type React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import InsightsOverview from './InsightsOverview';
import InsightExecutionDetail from './InsightExecutionDetail';

function wrap(path = '/organizations/acme/insights', element: React.ReactElement = <InsightsOverview />) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/organizations/:slug/insights" element={element} />
          <Route path="/organizations/:slug/insights/executions/:executionId" element={element} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function bothPaths(path: string) {
  return [path, `/api/orgs/:slug${path.slice('/api'.length)}`];
}

const execution = {
  execution_id: 'exec-freeze-1',
  task_id: 'task-1',
  task_title: 'Frozen attempt row',
  task_org_ref: 'T1',
  project_id: 'proj-a',
  project_name: 'agent-center2',
  agent_id: 'agent-alpha',
  status: 'completed',
  attempt: 3,
  submitted_at: '2026-08-26T10:00:00Z',
  started_at: '2026-08-26T10:01:00Z',
  completed_at: '2026-08-26T10:06:00Z',
  queue_wait_ms: 60000,
  duration_ms: 360000,
  command_id: 'cmd-1',
  worker_id: 'worker-1',
  refreshed_at: '2026-08-26T10:07:00Z',
  freshness: 'fresh',
  total_tool_calls: 0,
  total_tokens_input: 0,
  total_tokens_output: 0,
};

describe('Insights overview and execution drilldown', () => {
  afterEach(() => cleanup());

  it('renders fixed 24h overview metrics, rankings, and raw execution row links', async () => {
    wrap();
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Insight Overview' })).toBeInTheDocument());
    expect(screen.getByTestId('insights-window')).toHaveTextContent('Past 24h');
    expect(screen.getByTestId('insights-refreshed')).toHaveTextContent('Refreshed');
    expect(screen.getByTestId('insights-freshness')).toHaveTextContent('fresh');
    expect(screen.getByText('Executions')).toBeInTheDocument();
    expect(screen.getByText('Failure rate')).toBeInTheDocument();
    expect(screen.getByText('Slot utilization')).toBeInTheDocument();
    expect(screen.getByTestId('insights-agents')).toHaveTextContent('agent-alpha');
    expect(screen.getByTestId('insights-projects')).toHaveTextContent('agent-center2');
    const link = screen.getAllByTestId('insights-execution-link')[0];
    expect(link).toHaveTextContent('exec-24h-001');
    expect(link).toHaveAttribute('href', '/organizations/acme/insights/executions/exec-24h-001');
  });

  it('shows the empty state for a 24h window with no executions', async () => {
    server.use(
      ...bothPaths('/api/insights/overview').map((path) => http.get(path, () =>
        HttpResponse.json({
          window: { from: '2026-08-25T00:00:00Z', to: '2026-08-26T00:00:00Z', label: 'past_24h' },
          refreshed_at: '2026-08-26T00:00:00Z',
          freshness: 'stale',
          metrics: {
            execution_count: 0,
            failure_rate: 0,
            slot_utilization: 0,
            queue_wait_p50_ms: 0,
            queue_wait_p95_ms: 0,
            execution_duration_p50_ms: 0,
            execution_duration_p95_ms: 0,
          },
          agents: [],
          projects: [],
          executions: [],
        }),
      )),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('insights-empty')).toBeInTheDocument());
  });

  it('shows error state and retries overview loads', async () => {
    let calls = 0;
    server.use(
      ...bothPaths('/api/insights/overview').map((path) => http.get(path, () => {
        calls += 1;
        if (calls === 1) return HttpResponse.json({ error: 'boom', message: 'boom' }, { status: 500 });
        return HttpResponse.json({
          window: { from: '2026-08-25T00:00:00Z', to: '2026-08-26T00:00:00Z', label: 'past_24h' },
          refreshed_at: '2026-08-26T00:00:00Z',
          freshness: 'fresh',
          metrics: {
            execution_count: 1,
            failure_rate: 0,
            slot_utilization: 0,
            queue_wait_p50_ms: 0,
            queue_wait_p95_ms: 0,
            execution_duration_p50_ms: 0,
            execution_duration_p95_ms: 0,
          },
          agents: [],
          projects: [],
          executions: [execution],
        });
      })),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('insights-error')).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(screen.getByText('exec-freeze-1')).toBeInTheDocument());
  });

  it('direct-loads execution detail, displays frozen attempt, and refreshes', async () => {
    let calls = 0;
    server.use(
      ...bothPaths('/api/insights/executions/:executionId').map((path) => http.get(path, () => {
        calls += 1;
        return HttpResponse.json({ ...execution, attempt: calls === 1 ? 3 : 4, refreshed_at: `2026-08-26T10:0${6 + calls}:00Z` });
      })),
    );
    wrap('/organizations/acme/insights/executions/exec-freeze-1', <InsightExecutionDetail />);
    await waitFor(() => expect(screen.getByTestId('execution-id')).toHaveTextContent('exec-freeze-1'));
    expect(screen.getByTestId('execution-attempt')).toHaveTextContent('3');
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    await waitFor(() => expect(screen.getByTestId('execution-attempt')).toHaveTextContent('4'));
  });

  it('shows loading, not-found, and non-404 error states for execution detail', async () => {
    server.use(
      ...bothPaths('/api/insights/executions/missing').map((path) => http.get(path, () =>
        HttpResponse.json({ error: 'not_found', message: 'execution not found' }, { status: 404 }),
      )),
      ...bothPaths('/api/insights/executions/errored').map((path) => http.get(path, () =>
        HttpResponse.json({ error: 'failed', message: 'failed' }, { status: 500 }),
      )),
    );
    wrap('/organizations/acme/insights/executions/missing', <InsightExecutionDetail />);
    expect(screen.getByTestId('insight-execution-loading')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId('insight-execution-not-found')).toBeInTheDocument());
    cleanup();
    wrap('/organizations/acme/insights/executions/errored', <InsightExecutionDetail />);
    await waitFor(() => expect(screen.getByTestId('insight-execution-error')).toBeInTheDocument());
  });
});

import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { BrowserRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { renderWithQuery } from '@/test/renderWith';
import InsightsOverviewPage from './InsightsOverview';

function renderPage() {
  window.history.pushState({}, '', '/organizations/testorg/insights/overview');
  return renderWithQuery(
    <BrowserRouter>
      <InsightsOverviewPage />
    </BrowserRouter>,
  );
}

function overview(overrides = {}) {
  return {
    window: {
      label: 'past_24h',
      started_at: '2026-08-26T10:00:00Z',
      ended_at: '2026-08-27T10:00:00Z',
      refreshed_at: '2026-08-27T10:00:00Z',
    },
    refreshed_at: '2026-08-27T10:00:00Z',
    freshness: 'fresh',
    stale: false,
    summary: {
      executions: 3,
      failures: 1,
      failure_rate: 1 / 3,
      slot_utilization: { running: 1, capacity: 4, utilization: 0.25 },
      queue_wait: { p50_seconds: 60, p95_seconds: 180 },
      execution_duration: { p50_seconds: 600, p95_seconds: 900 },
    },
    leaderboards: {
      agents: [{ id: 'agent-1', name: 'Builder', executions: 2, failures: 1, failure_rate: 0.5 }],
      projects: [{ id: 'project-1', name: 'Atlas', executions: 3, failures: 1, failure_rate: 1 / 3 }],
    },
    ...overrides,
  };
}

const detail = {
  window: {
    label: 'past_24h',
    started_at: '2026-08-26T10:00:00Z',
    ended_at: '2026-08-27T10:00:00Z',
    refreshed_at: '2026-08-27T10:00:00Z',
  },
  refreshed_at: '2026-08-27T10:00:00Z',
  freshness: 'fresh',
  stale: false,
  total: 1,
  items: [{
    task_id: 'task-1',
    org_ref: 'T1',
    title: 'Ship overview',
    project_id: 'project-1',
    project_name: 'Atlas',
    agent_id: 'agent-1',
    agent_name: 'Builder',
    status: 'completed',
    created_at: '2026-08-27T09:00:00Z',
    updated_at: '2026-08-27T09:20:00Z',
    status_changed_at: '2026-08-27T09:20:00Z',
    queue_wait_seconds: 60,
    duration_seconds: 600,
    constituent_scopes: ['executions', 'agent:agent-1', 'project:project-1'],
  }],
};

afterEach(() => cleanup());

describe('InsightsOverviewPage', () => {
  it('renders a loading state before the backend overview resolves', () => {
    server.use(http.get('/api/orgs/:slug/insights/overview', () => new Promise(() => undefined)));
    renderPage();
    expect(screen.getByTestId('insights-loading')).toHaveTextContent('Loading Insight overview');
  });

  it('renders backend window, freshness, metrics, leaderboards, and metric drill-down', async () => {
    server.use(
      http.get('/api/orgs/:slug/insights/overview', () => HttpResponse.json(overview())),
      http.get('/api/orgs/:slug/insights/task-executions', ({ request }) => {
        expect(new URL(request.url).searchParams.get('filter')).toBe('metric');
        expect(new URL(request.url).searchParams.get('value')).toBe('queue_wait');
        return HttpResponse.json(detail);
      }),
    );

    renderPage();
    expect(await screen.findByTestId('insights-overview')).toBeInTheDocument();
    expect(screen.getByTestId('insights-window')).toHaveTextContent('2026');
    expect(screen.getByTestId('insights-refreshed')).toHaveTextContent('2026');
    expect(screen.getByTestId('insights-freshness')).toHaveTextContent('fresh');
    expect(screen.getByTestId('insights-card-executions')).toHaveTextContent('3');
    expect(screen.getByTestId('insights-card-failures')).toHaveTextContent('33.3%');
    expect(screen.getByTestId('insights-card-slot_utilization')).toHaveTextContent('25%');

    fireEvent.click(screen.getByTestId('insights-card-queue_wait'));
    const table = await screen.findByTestId('insights-drilldown-table');
    expect(within(table).getByText('Ship overview')).toBeInTheDocument();
    expect(within(table).getByText('Builder')).toBeInTheDocument();
  });

  it('opens leaderboard drill-down against constituent TaskExecution rows', async () => {
    server.use(
      http.get('/api/orgs/:slug/insights/overview', () => HttpResponse.json(overview())),
      http.get('/api/orgs/:slug/insights/task-executions', ({ request }) => {
        const url = new URL(request.url);
        expect(url.searchParams.get('filter')).toBe('agent');
        expect(url.searchParams.get('value')).toBe('agent-1');
        return HttpResponse.json(detail);
      }),
    );

    renderPage();
    fireEvent.click(await screen.findByTestId('insights-leaderboard-row-agent-1'));
    expect(await screen.findByTestId('insights-drilldown-table')).toHaveTextContent('T1');
  });

  it('keeps stale data distinct from empty and does not render it as zero', async () => {
    server.use(http.get('/api/orgs/:slug/insights/overview', () => HttpResponse.json(overview({ stale: true, freshness: 'stale' }))));
    renderPage();
    expect(await screen.findByTestId('insights-stale')).toBeInTheDocument();
    expect(screen.getByTestId('insights-card-executions')).toHaveTextContent('3');
    expect(screen.queryByTestId('insights-empty')).not.toBeInTheDocument();
  });

  it('renders empty and error states separately', async () => {
    server.use(http.get('/api/orgs/:slug/insights/overview', () => HttpResponse.json(overview({
      summary: {
        executions: 0,
        failures: 0,
        failure_rate: null,
        slot_utilization: { running: 0, capacity: 0, utilization: null },
        queue_wait: { p50_seconds: null, p95_seconds: null },
        execution_duration: { p50_seconds: null, p95_seconds: null },
      },
      leaderboards: { agents: [], projects: [] },
    }))));
    const empty = renderPage();
    expect(await screen.findByTestId('insights-empty')).toHaveTextContent('No TaskExecution records');
    empty.unmount();

    server.use(http.get('/api/orgs/:slug/insights/overview', () => HttpResponse.json({ message: 'boom' }, { status: 500 })));
    renderPage();
    await waitFor(() => expect(screen.getByTestId('insights-error')).toHaveTextContent('boom'));
  });
});

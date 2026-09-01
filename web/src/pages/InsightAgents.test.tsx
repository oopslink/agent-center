import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import i18n from '@/i18n';
import { server } from '@/test/mswServer';
import InsightAgentsPage, { InsightAgentDetailPage } from './InsightAgents';

const freshness = { state: 'fresh', age_ms: 1000, threshold_ms: 120000 };

function metric(value: number | null, overrides: Record<string, unknown> = {}) {
  return {
    value,
    meta: {
      metric_version: 'insight.metrics.v2',
      sample_count: value ?? 0,
      coverage: 1,
      freshness,
      unknown_count: 0,
      known: value !== null,
      ...overrides,
    },
  };
}

function agent(overrides: Record<string, unknown> = {}) {
  return {
    id: 'agent:builder',
    name: 'Builder',
    health: { status: 'healthy', reason_codes: [], evidence: [] },
    execution_count: metric(8),
    failure_rate: null,
    open_issues: metric(0),
    blocked_tasks: metric(0),
    active_plans: metric(1),
    reason_codes: [],
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
          <Route path="/organizations/:slug/insights/agents" element={<InsightAgentsPage />} />
          <Route path="/organizations/:slug/insights/agents/:agentRef" element={<InsightAgentDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(async () => {
  cleanup();
  await i18n.changeLanguage('en');
});

describe('Insight agents pages', () => {
  it('renders v2 agents and preserves exact agent_ref in detail and execution drilldown URLs', async () => {
    server.use(http.get('/api/orgs/:slug/insights/v2/agents', ({ request }) => {
      expect(new URL(request.url).searchParams.get('window')).toBe('24h');
      return HttpResponse.json([
        agent({ execution_count: metric(3, { coverage: 0.001, unknown_count: 2, sample_count: 5 }) }),
      ]);
    }));

    renderAt('/organizations/acme/insights/agents');

    expect(await screen.findByTestId('insight-agents-charts')).toHaveTextContent('Health mix');
    expect(screen.getByTestId('insight-agents-charts')).toHaveTextContent('Agent throughput');
    const table = await screen.findByTestId('insight-agents-table');
    expect(table).toHaveTextContent('Builder');
    expect(table).toHaveTextContent('0.1%');
    expect(table).toHaveTextContent('2');
    expect(within(table).getByRole('link', { name: 'Builder' })).toHaveAttribute('href', '/organizations/acme/insights/agents/agent%3Abuilder');
    expect(within(table).getByRole('link', { name: 'View executions' })).toHaveAttribute('href', '/organizations/acme/insights/executions?window=24h&agent_ref=agent%3Abuilder');
  });

  it('renders detail metrics and semantic health reasons without exposing enum tokens', async () => {
    server.use(http.get('/api/orgs/:slug/insights/v2/agents/:agentRef', ({ params, request }) => {
      expect(params.agentRef).toBe('agent:builder');
      expect(new URL(request.url).searchParams.get('window')).toBe('24h');
      return HttpResponse.json(agent({
        health: { status: 'unknown_status', reason_codes: ['coverage_low', 'raw_future_enum'], evidence: [] },
        reason_codes: ['coverage_low', 'raw_future_enum'],
        execution_count: metric(null, { known: false, coverage: null, unknown_count: 4, sample_count: 0 }),
      }));
    }));

    renderAt('/organizations/acme/insights/agents/agent%3Abuilder');

    const detail = await screen.findByTestId('insight-agent-detail');
    expect(detail).toHaveTextContent('Unknown');
    expect(detail).toHaveTextContent('Low observation coverage');
    expect(detail).toHaveTextContent('Backend-defined reason');
    expect(detail).not.toHaveTextContent('unknown_status');
    expect(detail).not.toHaveTextContent('coverage_low');
    expect(detail).not.toHaveTextContent('raw_future_enum');
    expect(detail).toHaveTextContent('Metric confidence');
    expect(screen.getByText('Agent work shape')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'View executions' })).toHaveAttribute('href', '/organizations/acme/insights/executions?window=24h&agent_ref=agent%3Abuilder');
  });

  it('renders empty, auth, and not-found states', async () => {
    server.use(http.get('/api/orgs/:slug/insights/v2/agents', () => HttpResponse.json([])));
    renderAt('/organizations/acme/insights/agents');
    expect(await screen.findByTestId('insight-agents-empty')).toHaveTextContent('No agents');
    cleanup();

    server.use(http.get('/api/orgs/:slug/insights/v2/agents', () => HttpResponse.json({ error: 'forbidden', message: 'no insight permission' }, { status: 403 })));
    renderAt('/organizations/acme/insights/agents');
    expect(await screen.findByTestId('insight-agents-auth-error')).toHaveTextContent('permission');
    cleanup();

    server.use(http.get('/api/orgs/:slug/insights/v2/agents/:agentRef', () => HttpResponse.json({ error: 'not_found', message: 'not found' }, { status: 404 })));
    renderAt('/organizations/acme/insights/agents/agent%3Amissing');
    expect(await screen.findByTestId('insight-agent-not-found')).toHaveTextContent('not found');
  });
});

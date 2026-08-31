import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { FakeEventSource } from '@/sse/fakeEventSource';
import AppLayout from './AppLayout';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function renderShell(initial = '/insights/overview') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/insights/overview" element={<div data-testid="page-InsightOverview">overview</div>} />
            <Route path="/insights/agents" element={<div data-testid="page-InsightAgents">agents</div>} />
            <Route path="/insights/projects" element={<div data-testid="page-InsightProjects">projects</div>} />
            <Route path="/insights/executions" element={<div data-testid="page-InsightExecutions">executions</div>} />
            <Route path="/insights/executions/:executionId" element={<div data-testid="page-InsightExecutionDetail">detail</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AppLayout Insight secondary nav', () => {
  afterEach(() => cleanup());

  it('desktop col2 includes all Insight entries and navigates without leaving the Insight rail module', async () => {
    renderShell('/insights/overview');

    const rail = screen.getByRole('navigation', { name: /^modules$/ });
    expect(within(rail).getByTestId('rail-module-insight')).toHaveAttribute('data-active', 'true');

    const nav = screen.getByRole('navigation', { name: /^primary$/ });
    expect(within(nav).getByRole('link', { name: 'Overview' })).toHaveAttribute('href', '/insights/overview');
    expect(within(nav).getByRole('link', { name: 'Agents' })).toHaveAttribute('href', '/insights/agents');
    expect(within(nav).getByRole('link', { name: 'Projects' })).toHaveAttribute('href', '/insights/projects');
    expect(within(nav).getByRole('link', { name: 'Task executions' })).toHaveAttribute('href', '/insights/executions?window=24h');

    fireEvent.click(within(nav).getByRole('link', { name: 'Projects' }));
    await waitFor(() => expect(screen.getByTestId('page-InsightProjects')).toBeInTheDocument());
    expect(within(rail).getByTestId('rail-module-insight')).toHaveAttribute('data-active', 'true');
    expect(within(nav).getByRole('link', { name: 'Projects' }).className).toContain('bg-brand-hover');
  });

  it('marks Task executions active on detail drilldown routes', () => {
    renderShell('/insights/executions/exec-1');
    const nav = screen.getByRole('navigation', { name: /^primary$/ });
    expect(within(nav).getByRole('link', { name: 'Task executions' }).className).toContain('bg-brand-hover');
  });
});

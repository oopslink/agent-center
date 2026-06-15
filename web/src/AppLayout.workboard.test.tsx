// v2.10.0 [T5] Workspace · Project Work Board — three-column shell integration.
// The Work Board (/projects/:id/plans) renders as col③ inside the Workspace
// module, with the col② project sub-nav (T4's WorkspaceSecondaryNav) showing
// "Work Board" active. Three columns — no col④ (mockup workboard.html). This
// pins that wiring; the board content itself is covered by ProjectPlans.test.
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import AppLayout from './AppLayout';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

beforeEach(() => {
  server.use(
    http.get('/api/projects/:id', ({ params }) =>
      HttpResponse.json({
        id: String(params.id), organization_id: 'org-test', name: 'agent-center2',
        description: '', status: 'active', created_by: 'user:x', version: 1,
        created_at: '2026-06-01T00:00:00Z', updated_at: '2026-06-01T00:00:00Z',
      }),
    ),
  );
});

function renderShell(initial = '/projects/P1/plans') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/projects" element={<div data-testid="page-Projects">list</div>} />
            <Route path="/projects/:id" element={<div data-testid="page-ProjectDetail">detail</div>} />
            <Route path="/projects/:id/plans" element={<div data-testid="page-ProjectPlans">board</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('col②/④ Work Board — three-column shell integration (v2.10.0 [T5])', () => {
  afterEach(() => cleanup());

  it('renders the Work Board page as col③ with the Workspace module active', () => {
    renderShell('/projects/P1/plans');
    expect(screen.getByTestId('rail-module-workspace')).toHaveAttribute('data-active', 'true');
    expect(screen.getByTestId('page-ProjectPlans')).toBeInTheDocument();
  });

  it('col② shows the project sub-nav with Work Board the active entry', async () => {
    renderShell('/projects/P1/plans');
    const nav = screen.getByRole('navigation', { name: /^primary$/ });
    expect(within(nav).getByTestId('workspace-nav-project')).toBeInTheDocument();
    const wb = within(nav).getByTestId('project-subnav-workboard');
    expect(wb).toHaveAttribute('aria-current', 'page');
    expect(wb).toHaveAttribute('href', '/projects/P1/plans');
    // The other project tabs are present but NOT current.
    expect(within(nav).getByTestId('project-subnav-tasks')).not.toHaveAttribute('aria-current', 'page');
    // The current project's name renders in the sub-nav header.
    await waitFor(() => expect(nav.textContent).toContain('agent-center2'));
  });

  it('is three-column: no col④ context panel for the Work Board', () => {
    renderShell('/projects/P1/plans');
    expect(screen.getByTestId('context-panel')).toHaveAttribute('data-open', 'false');
  });
});

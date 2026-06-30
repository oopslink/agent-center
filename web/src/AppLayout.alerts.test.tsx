// Global "stuck" Alerts rail item (col①): a task waiting on the user (running +
// blocked_reason of type input_required/obstacle) must be visible from ANY page —
// a badge with the count, an auto-opening popout listing the tasks (input_required
// first), each deep-linking to the task so the user can unblock it.
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import { OrgContext } from './OrgContext';

vi.mock('@/shell/secondaryNav', () => ({ SECONDARY_NAV_REGISTRY: {} }));

import AppLayout from './AppLayout';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
  const store: Record<string, string> = {};
  Object.defineProperty(globalThis, 'localStorage', {
    value: {
      getItem: (k: string) => (k in store ? store[k] : null),
      setItem: (k: string, v: string) => { store[k] = String(v); },
      removeItem: (k: string) => { delete store[k]; },
      clear: () => { for (const k of Object.keys(store)) delete store[k]; },
    },
    configurable: true,
  });
});

// An org-scoped tasks aggregation with two actionable stuck tasks (input_required
// + obstacle), one healthy running task, and one running task blocked by an
// unrelated/empty reason type — only the two actionable ones become alerts.
function seedTasks() {
  server.use(
    http.get('/api/tasks', () =>
      HttpResponse.json({
        total: 4,
        items: [
          {
            id: 'task-obs', org_ref: 'T20', project: { id: 'p1', name: 'Alpha' },
            title: 'Needs a deploy key', status: 'running', assignee: null,
            created_at: '2026-06-01T00:00:00Z', updated_at: '2026-06-29T00:00:00Z',
            blocked_reason: 'waiting on infra to provision a key', blocked_reason_type: 'obstacle',
          },
          {
            id: 'task-inp', org_ref: 'T21', project: { id: 'p2', name: 'Beta' },
            title: 'Confirm the schema', status: 'running', assignee: null,
            created_at: '2026-06-01T00:00:00Z', updated_at: '2026-06-30T00:00:00Z',
            blocked_reason: 'please confirm column names', blocked_reason_type: 'input_required',
          },
          {
            id: 'task-ok', org_ref: 'T22', project: { id: 'p1', name: 'Alpha' },
            title: 'Healthy running task', status: 'running', assignee: null,
            created_at: '2026-06-01T00:00:00Z', updated_at: '2026-06-28T00:00:00Z',
            blocked_reason: '', blocked_reason_type: '',
          },
        ],
      }),
    ),
  );
}

function renderShell() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OrgContext.Provider value={{ slug: 'acme', orgId: 'org-1', orgName: 'Acme' }}>
        <MemoryRouter initialEntries={['/channels']}>
          <Routes>
            <Route element={<AppLayout />}>
              <Route path="/channels" element={<div data-testid="page-Channels">x</div>} />
            </Route>
          </Routes>
        </MemoryRouter>
      </OrgContext.Provider>
    </QueryClientProvider>,
  );
}

describe('rail Alerts item — global stuck-task surfacing', () => {
  beforeEach(() => { localStorage.clear(); });
  afterEach(() => cleanup());

  it('shows a count badge and auto-opens the panel with input_required first', async () => {
    seedTasks();
    renderShell();

    // Badge reflects the two actionable stuck tasks.
    const badge = await screen.findByTestId('rail-alerts-badge');
    expect(badge).toHaveTextContent('2');

    // Panel auto-opens on first load when there are alerts.
    const panel = await screen.findByTestId('rail-alerts-panel');
    const items = within(panel).getAllByTestId('rail-alert-item');
    expect(items).toHaveLength(2);

    // input_required (most urgent) is listed first…
    expect(items[0]).toHaveAttribute('data-reason-type', 'input_required');
    expect(items[1]).toHaveAttribute('data-reason-type', 'obstacle');
    // …and the healthy running task never appears.
    expect(within(panel).queryByText('Healthy running task')).not.toBeInTheDocument();

    // Each alert deep-links to its task detail under the current org base.
    expect(items[0]).toHaveAttribute('href', '/organizations/acme/projects/p2/tasks/task-inp');
    expect(within(items[0]).getByText('Confirm the schema')).toBeInTheDocument();
    expect(within(items[0]).getByText('等你回复')).toBeInTheDocument();
    expect(within(items[1]).getByText('需介入')).toBeInTheDocument();
  });

  it('renders no badge when there are no stuck tasks', async () => {
    server.use(http.get('/api/tasks', () => HttpResponse.json({ total: 0, items: [] })));
    renderShell();

    // The Alerts rail button is always present…
    const btn = await screen.findByTestId('rail-alerts');
    expect(btn).toHaveAttribute('data-count', '0');
    // …but with no badge and no auto-opened panel.
    await waitFor(() => expect(screen.getByTestId('page-Channels')).toBeInTheDocument());
    expect(screen.queryByTestId('rail-alerts-badge')).not.toBeInTheDocument();
    expect(screen.queryByTestId('rail-alerts-panel')).not.toBeInTheDocument();
  });
});

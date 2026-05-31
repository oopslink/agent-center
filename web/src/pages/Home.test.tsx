import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import Home from './Home';

beforeAll(() => {
  server.use(http.get('/api/input_requests', () => HttpResponse.json([])));
});

function renderHome() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <Home />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Home / Overview (v2.3 P3)', () => {
  afterEach(() => cleanup());

  it('renders the three stat cards', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({
          work_items: [],
          workers: [],
          pending_issues: [],
          generated_at: '2026-05-25T14:00:00Z',
        }),
      ),
      http.get('/api/conversations', () => HttpResponse.json([])),
    );
    renderHome();
    await waitFor(() => {
      expect(screen.getByText('Pending input requests')).toBeInTheDocument();
      expect(screen.getByText('Active work items')).toBeInTheDocument();
      expect(screen.getByText('Workers online')).toBeInTheDocument();
    });
  });

  it('renders active work items from the fleet snapshot', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({
          work_items: [
            { work_item_id: 'WI-1', agent_id: 'AG-1', task_id: 'T1', status: 'active', current_activity: 'edit', total_tool_calls: 2, total_tokens_input: 100, total_tokens_output: 50, working_seconds: 0, last_activity_at: 't' },
          ],
          workers: [
            { worker_id: 'W1', status: 'online', active_count: 1 },
          ],
          pending_issues: [],
        }),
      ),
      http.get('/api/conversations', () => HttpResponse.json([])),
    );
    renderHome();
    // The active work item lands in the "Active work items" panel (agent_id rendered).
    await waitFor(() => {
      expect(screen.getByText(/AG-1/)).toBeInTheDocument();
    });
  });

  it('shows empty-state copy when nothing is running', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({ work_items: [], workers: [], pending_issues: [] }),
      ),
      http.get('/api/conversations', () => HttpResponse.json([])),
    );
    renderHome();
    await waitFor(() => {
      expect(screen.getByText('No active work items')).toBeInTheDocument();
      expect(screen.getByText('No conversations yet')).toBeInTheDocument();
    });
  });
});

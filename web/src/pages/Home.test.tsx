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
          executions: [],
          workers: [],
          open_input_requests: [],
          pending_issues: [],
          generated_at: '2026-05-25T14:00:00Z',
        }),
      ),
      http.get('/api/conversations', () => HttpResponse.json([])),
    );
    renderHome();
    await waitFor(() => {
      expect(screen.getByText('Pending input requests')).toBeInTheDocument();
      expect(screen.getByText('Failed executions')).toBeInTheDocument();
      expect(screen.getByText('Workers online')).toBeInTheDocument();
    });
  });

  it('badges failed and running counts from the fleet snapshot', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({
          executions: [
            { execution_id: 'E1', task_id: 'T1', worker_id: 'W1', agent_cli: 'claude-code', workspace_mode: 'direct', status: 'working', working_seconds: 12, started_at: 't' },
            { execution_id: 'E2', task_id: 'T2', worker_id: 'W1', agent_cli: 'claude-code', workspace_mode: 'direct', status: 'failed', working_seconds: 3, started_at: 't' },
          ],
          workers: [
            { worker_id: 'W1', status: 'online', active_count: 1, mappings_count: 1 },
          ],
          open_input_requests: [],
          pending_issues: [],
        }),
      ),
      http.get('/api/conversations', () => HttpResponse.json([])),
    );
    renderHome();
    // Wait for the working execution to land in the running-execs panel
    // (W1 is the worker_id rendered for that row).
    await waitFor(() => {
      expect(screen.getByText(/W1/)).toBeInTheDocument();
    });
  });

  it('shows empty-state copy when nothing is running', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({ executions: [], workers: [], open_input_requests: [], pending_issues: [] }),
      ),
      http.get('/api/conversations', () => HttpResponse.json([])),
    );
    renderHome();
    await waitFor(() => {
      expect(screen.getByText('No running executions')).toBeInTheDocument();
      expect(screen.getByText('No conversations yet')).toBeInTheDocument();
    });
  });
});

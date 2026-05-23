import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import AgentDetail from './AgentDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/agents/:name" element={<AgentDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AgentDetail page', () => {
  afterEach(() => cleanup());

  it('renders profile + matching active executions from fleet', async () => {
    server.use(
      http.get('/api/agents/:name', () =>
        HttpResponse.json({
          id: 'A1',
          identity_id: 'agent:A1',
          name: 'bot-1',
          agent_cli: 'claudecode',
          state: 'active',
          worker_id: 'w-1',
          is_builtin: false,
          max_concurrent: 4,
        }),
      ),
      http.get('/api/fleet', () =>
        HttpResponse.json({
          executions: [
            {
              execution_id: 'E-1',
              task_id: 'T-1',
              worker_id: 'w-1',
              agent_cli: 'claudecode',
              workspace_mode: 'worktree',
              status: 'working',
              working_seconds: 12,
              started_at: '2026-05-24T01:00:00Z',
            },
            // Doesn't match — wrong worker.
            {
              execution_id: 'E-other',
              task_id: 'T-other',
              worker_id: 'w-2',
              agent_cli: 'claudecode',
              workspace_mode: 'worktree',
              status: 'working',
              working_seconds: 5,
              started_at: '2026-05-24T01:00:00Z',
            },
          ],
          workers: [],
          open_input_requests: [],
          pending_issues: [],
        }),
      ),
    );
    wrap('/agents/bot-1');
    await waitFor(() => expect(screen.getByText('bot-1')).toBeInTheDocument());
    expect(screen.getByText('claudecode')).toBeInTheDocument();
    const rows = await waitFor(() => screen.getAllByTestId('agent-exec-row'));
    expect(rows).toHaveLength(1);
    expect(rows[0]).toHaveAttribute('data-execution-id', 'E-1');
  });

  it('renders empty exec state when nothing matches', async () => {
    server.use(
      http.get('/api/agents/:name', () =>
        HttpResponse.json({
          id: 'A1',
          identity_id: 'agent:A1',
          name: 'idle-bot',
          agent_cli: 'claudecode',
          state: 'idle',
        }),
      ),
      http.get('/api/fleet', () =>
        HttpResponse.json({
          executions: [],
          workers: [],
          open_input_requests: [],
          pending_issues: [],
        }),
      ),
    );
    wrap('/agents/idle-bot');
    await waitFor(() => expect(screen.getByTestId('agent-exec-empty')).toBeInTheDocument());
  });

  it('surfaces lookup error', async () => {
    server.use(
      http.get('/api/agents/:name', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such agent' }, { status: 404 }),
      ),
    );
    wrap('/agents/ghost');
    await waitFor(() => expect(screen.getByTestId('agent-not-found')).toHaveTextContent(/no such agent/));
  });
});

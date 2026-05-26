import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import Fleet from './Fleet';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Fleet page', () => {
  afterEach(() => cleanup());

  it('renders all four segments when populated', async () => {
    server.use(
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
              working_seconds: 10,
              started_at: '2026-05-24T01:00:00Z',
            },
          ],
          workers: [
            {
              worker_id: 'w-1',
              status: 'online',
              active_count: 1,
              mappings_count: 3,
              last_heartbeat_at: '2026-05-24T01:00:01Z',
            },
          ],
          open_input_requests: [
            {
              input_request_id: 'IR-1',
              task_execution_id: 'E-1',
              question: 'proceed?',
              urgency: 'normal',
              requested_at: '2026-05-24T01:00:02Z',
            },
          ],
          pending_issues: [
            {
              issue_id: 'I-1',
              project_id: 'P-1',
              title: 'fix login',
              status: 'active',
              opened_at: '2026-05-24T00:00:00Z',
              opener: 'user:hayang',
            },
          ],
          generated_at: '2026-05-24T01:00:03Z',
        }),
      ),
    );
    wrap(<Fleet />);
    await waitFor(() => expect(screen.getByTestId('fleet-workers-table')).toBeInTheDocument());
    expect(screen.getByTestId('fleet-worker-row')).toHaveAttribute('data-worker-id', 'w-1');
    expect(screen.getByTestId('fleet-exec-row')).toHaveAttribute('data-execution-id', 'E-1');
    expect(screen.getByText('proceed?')).toBeInTheDocument();
    expect(screen.getByText('fix login')).toBeInTheDocument();
    expect(screen.getByTestId('fleet-generated-at')).toHaveTextContent('2026-05-24T01:00:03Z');
  });

  it('empty fleet shows enroll-worker hint', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({
          executions: [],
          workers: [],
          open_input_requests: [],
          pending_issues: [],
        }),
      ),
    );
    wrap(<Fleet />);
    await waitFor(() => expect(screen.getByTestId('fleet-workers-empty')).toHaveTextContent(/No workers connected yet/));
    expect(screen.getByTestId('fleet-workers-empty-cta')).toHaveTextContent(/Add your first worker/);
    expect(screen.getByTestId('fleet-exec-empty')).toBeInTheDocument();
  });

  it('shows warnings banner when backend returns partial snapshot', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({
          executions: [],
          workers: [],
          open_input_requests: [],
          pending_issues: [],
          warnings: ['workers segment errored: db down'],
        }),
      ),
    );
    wrap(<Fleet />);
    await waitFor(() => expect(screen.getByTestId('fleet-warnings')).toBeInTheDocument());
    expect(screen.getByTestId('fleet-warnings')).toHaveTextContent(/db down/);
  });

  it('surfaces API error', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({ error: 'fleet_not_wired', message: 'down' }, { status: 501 }),
      ),
    );
    wrap(<Fleet />);
    await waitFor(() => expect(screen.getByTestId('fleet-error')).toHaveTextContent(/down/));
  });
});

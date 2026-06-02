import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import Environment from './Environment';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

// v2.7 #164: Environment now sources workers from /api/fleet (canonical
// workforce.Worker + active work-item count), with agents grouped per worker +
// work items + pending issues + transfers.
const fleetSnapshot = (workers: unknown[], extra: Record<string, unknown> = {}) => ({
  generated_at: '2026-05-24T02:00:00Z',
  workers,
  work_items: [],
  pending_issues: [],
  warnings: [],
  ...extra,
});

const fleetWorker = (id: string, extra: Record<string, unknown> = {}) => ({
  worker_id: id,
  name: id,
  status: 'online',
  active_count: 0,
  last_heartbeat_at: '2026-05-24T02:00:00Z',
  ...extra,
});

const agent = (id: string, workerID: string, extra: Record<string, unknown> = {}) => ({
  id,
  organization_id: 'O-1',
  name: id,
  description: '',
  model: 'claude-opus',
  cli: 'claudecode',
  env_vars: {},
  skills: [],
  worker_id: workerID,
  lifecycle: 'running',
  availability: 'busy',
  created_by: 'user:hayang',
  version: 1,
  created_at: '2026-05-24T01:00:00Z',
  updated_at: '2026-05-24T02:00:00Z',
  ...extra,
});

describe('Environment page (#164 merged Fleet+Environment)', () => {
  afterEach(() => cleanup());

  it('renders workers with status + active count and agents grouped by worker', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json(
          fleetSnapshot([
            fleetWorker('w-1', { status: 'online', active_count: 3 }),
            fleetWorker('w-2', { status: 'offline', active_count: 0 }),
          ]),
        ),
      ),
      http.get('/api/agents', () =>
        HttpResponse.json({ agents: [agent('bot-a', 'w-1'), agent('bot-b', 'w-1')] }),
      ),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);

    await waitFor(() => expect(screen.getAllByTestId('environment-worker')).toHaveLength(2));
    const rows = screen.getAllByTestId('environment-worker');
    expect(rows[0]).toHaveAttribute('data-worker-id', 'w-1');
    expect(rows[0]).toHaveAttribute('data-status', 'online');
    expect(rows[0]).toHaveTextContent('3 active'); // active_count from /api/fleet

    // w-1 has its two agents grouped under it; w-2 has none.
    expect(screen.getAllByTestId('environment-agent')).toHaveLength(2);
    expect(screen.getByText('bot-a')).toBeInTheDocument();
    expect(screen.getByTestId('environment-worker-noagents')).toBeInTheDocument();
  });

  it('shows the empty state when there are no workers', async () => {
    server.use(
      http.get('/api/fleet', () => HttpResponse.json(fleetSnapshot([]))),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    await waitFor(() => expect(screen.getByTestId('environment-workers-empty')).toBeInTheDocument());
    expect(screen.getByTestId('environment-workers-empty')).toHaveTextContent(/worker/i);
  });

  it('surfaces API error', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({ error: 'fleet_error', message: 'db down' }, { status: 500 }),
      ),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    await waitFor(() =>
      expect(screen.getByTestId('environment-error')).toHaveTextContent(/db down/),
    );
  });

  it('renders work items + pending issues from the fleet snapshot', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json(
          fleetSnapshot([fleetWorker('w-1', { active_count: 1 })], {
            work_items: [
              { work_item_id: 'wi-1', task_id: 'task-1', agent_id: 'bot-a', status: 'active', current_activity: 'coding' },
            ],
            pending_issues: [{ issue_id: 'iss-1', title: 'Fix login' }],
          }),
        ),
      ),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    await waitFor(() => expect(screen.getByTestId('environment-workitem-row')).toBeInTheDocument());
    expect(screen.getByText('task-1')).toBeInTheDocument();
    expect(screen.getByText('Fix login')).toBeInTheDocument();
  });

  it('renders in-flight transfer sessions in the transfers section', async () => {
    server.use(
      http.get('/api/fleet', () => HttpResponse.json(fleetSnapshot([]))),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () =>
        HttpResponse.json({
          transfer_sessions: [
            {
              id: 't-1',
              file_uri: 'ac://files/abc',
              transfer_uri: 'ac://transfers/t-1',
              direction: 'upload',
              status: 'open',
              scope: 'project',
              scope_id: 'p-1',
              content_type: 'application/pdf',
              size: 2048,
              created_by: 'user:hayang',
              created_at: '2026-05-24T01:00:00Z',
              expires_at: '2026-05-24T02:00:00Z',
            },
          ],
        }),
      ),
    );
    wrap(<Environment />);
    await waitFor(() => expect(screen.getByTestId('transfer-row')).toBeInTheDocument());
    const row = screen.getByTestId('transfer-row');
    expect(row).toHaveAttribute('data-scope', 'project');
    expect(row).toHaveTextContent('upload');
    expect(row).toHaveTextContent('project/p-1');
  });
});

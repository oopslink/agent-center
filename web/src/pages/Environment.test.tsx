import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
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

const worker = (id: string, extra: Record<string, unknown> = {}) => ({
  worker_id: id,
  organization_id: 'O-1',
  name: id,
  status: 'online',
  last_acked_offset: 0,
  created_at: '2026-05-24T01:00:00Z',
  updated_at: '2026-05-24T02:00:00Z',
  version: 1,
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

describe('Environment page', () => {
  afterEach(() => cleanup());

  it('renders control-connected workers with status + offset and agents grouped by worker', async () => {
    server.use(
      http.get('/api/workers', () =>
        HttpResponse.json({
          workers: [
            worker('w-1', { status: 'online', last_acked_offset: 7 }),
            worker('w-2', { status: 'offline', last_acked_offset: 0 }),
          ],
        }),
      ),
      http.get('/api/agents', () =>
        HttpResponse.json({ agents: [agent('bot-a', 'w-1'), agent('bot-b', 'w-1')] }),
      ),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);

    await waitFor(() => expect(screen.getAllByTestId('environment-worker')).toHaveLength(2));
    // control-connected labeling present in the header copy
    expect(screen.getByTestId('page-Environment')).toHaveTextContent(/control-connected/i);

    const rows = screen.getAllByTestId('environment-worker');
    expect(rows[0]).toHaveAttribute('data-worker-id', 'w-1');
    expect(rows[0]).toHaveAttribute('data-status', 'online');
    expect(rows[0]).toHaveTextContent('offset 7');

    // w-1 has its two agents grouped under it; w-2 has none.
    expect(screen.getAllByTestId('environment-agent')).toHaveLength(2);
    expect(screen.getByText('bot-a')).toBeInTheDocument();
    expect(screen.getByTestId('environment-worker-noagents')).toBeInTheDocument();
  });

  it('shows the empty state when no worker is control-connected', async () => {
    server.use(
      http.get('/api/workers', () => HttpResponse.json({ workers: [] })),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    await waitFor(() => expect(screen.getByTestId('environment-empty')).toBeInTheDocument());
    expect(screen.getByTestId('environment-empty')).toHaveTextContent(/control channel/i);
  });

  it('surfaces API error', async () => {
    server.use(
      http.get('/api/workers', () =>
        HttpResponse.json({ error: 'env_workers_error', message: 'db down' }, { status: 500 }),
      ),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    await waitFor(() =>
      expect(screen.getByTestId('environment-error')).toHaveTextContent(/db down/),
    );
  });

  it('renders in-flight transfer sessions in the transfers section', async () => {
    server.use(
      http.get('/api/workers', () => HttpResponse.json({ workers: [] })),
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

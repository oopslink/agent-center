import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import Agents from './Agents';

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

const agent = (id: string, extra: Record<string, unknown> = {}) => ({
  id,
  organization_id: 'O-1',
  name: id,
  description: '',
  model: 'claude-opus',
  cli: 'claudecode',
  env_vars: {},
  skills: [],
  worker_id: 'w-1',
  lifecycle: 'stopped',
  availability: 'available',
  created_by: 'user:hayang',
  version: 1,
  created_at: '2026-05-24T01:00:00Z',
  updated_at: '2026-05-24T02:00:00Z',
  ...extra,
});

const seed = [
  agent('bot-1', { name: 'bot-1', lifecycle: 'running', availability: 'busy' }),
  agent('bot-2', { name: 'bot-2', lifecycle: 'stopped', availability: 'available' }),
  agent('bot-3', { name: 'bot-3', lifecycle: 'error', availability: 'unavailable', worker_id: '' }),
];

describe('Agents page', () => {
  beforeEach(() => {
    server.use(http.get('/api/agents', () => HttpResponse.json({ agents: seed })));
  });
  afterEach(() => cleanup());

  it('renders all agents with lifecycle + availability badges and link to /agents/{id}', async () => {
    wrap(<Agents />);
    await waitFor(() => expect(screen.getAllByTestId('agent-row')).toHaveLength(3));
    expect(screen.getByText('bot-1')).toBeInTheDocument();

    const badges = screen.getAllByTestId('agent-availability-badge');
    expect(badges[0]).toHaveAttribute('data-availability', 'busy');
    expect(badges[2]).toHaveAttribute('data-availability', 'unavailable');

    const links = screen.getAllByText(/Open/);
    expect(links[0]).toHaveAttribute('href', '/agents/bot-1');
  });

  it('shows the add-agent empty state when there are no agents', async () => {
    server.use(http.get('/api/agents', () => HttpResponse.json({ agents: [] })));
    wrap(<Agents />);
    await waitFor(() => expect(screen.getByTestId('agents-empty')).toBeInTheDocument());
    expect(screen.getByTestId('agents-empty')).toHaveTextContent(/Add Agent/);
  });

  it('opens the create modal with a worker picker sourced from the fleet', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({
          executions: [],
          workers: [{ worker_id: 'w-7', name: 'box-7', status: 'online' }],
          open_input_requests: [],
          pending_issues: [],
        }),
      ),
    );
    wrap(<Agents />);
    await waitFor(() => expect(screen.getAllByTestId('agent-row')).toHaveLength(3));
    fireEvent.click(screen.getByTestId('agents-add-btn'));
    expect(screen.getByTestId('agent-create-modal')).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByTestId('agent-create-worker')).toHaveTextContent('box-7'),
    );
  });

  it('creates an agent through the modal', async () => {
    let posted: Record<string, unknown> | null = null;
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({
          executions: [],
          workers: [{ worker_id: 'w-7', name: 'box-7', status: 'online' }],
          open_input_requests: [],
          pending_issues: [],
        }),
      ),
      http.post('/api/agents', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(agent('A-NEW', { name: 'newbot' }), { status: 201 });
      }),
    );
    wrap(<Agents />);
    await waitFor(() => expect(screen.getAllByTestId('agent-row')).toHaveLength(3));
    fireEvent.click(screen.getByTestId('agents-add-btn'));

    await userEvent.type(screen.getByTestId('agent-create-name'), 'newbot');
    await waitFor(() =>
      expect(screen.getByTestId('agent-create-worker')).toHaveTextContent('box-7'),
    );
    await userEvent.selectOptions(screen.getByTestId('agent-create-worker'), 'w-7');
    fireEvent.click(screen.getByTestId('agent-create-submit'));

    await waitFor(() => expect(posted).not.toBeNull());
    expect(posted).toMatchObject({ name: 'newbot', worker_id: 'w-7' });
    await waitFor(() =>
      expect(screen.queryByTestId('agent-create-modal')).not.toBeInTheDocument(),
    );
  });

  it('surfaces API error', async () => {
    server.use(
      http.get('/api/agents', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap(<Agents />);
    await waitFor(() => expect(screen.getByTestId('agents-error')).toHaveTextContent(/db down/));
  });
});

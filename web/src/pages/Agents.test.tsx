import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
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
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

const seed = [
  { id: 'A1', identity_id: 'agent:A1', name: 'bot-1', agent_cli: 'claudecode', state: 'active', worker_id: 'w-1' },
  { id: 'A2', identity_id: 'agent:A2', name: 'bot-2', agent_cli: 'codex', state: 'idle', worker_id: 'w-2' },
  { id: 'A3', identity_id: 'agent:A3', name: 'gone', agent_cli: 'claudecode', state: 'archived' },
];

describe('Agents page', () => {
  beforeEach(() => {
    server.use(http.get('/api/agents', () => HttpResponse.json(seed)));
  });
  afterEach(() => cleanup());

  it('renders all agents by default + profile link', async () => {
    wrap(<Agents />);
    await waitFor(() => expect(screen.getAllByTestId('agent-row')).toHaveLength(3));
    expect(screen.getByText('bot-1')).toBeInTheDocument();
    const links = screen.getAllByText(/Open profile/);
    expect(links[0]).toHaveAttribute('href', '/agents/bot-1');
  });

  it('state filter narrows to one row', async () => {
    wrap(<Agents />);
    await waitFor(() => expect(screen.getAllByTestId('agent-row')).toHaveLength(3));
    fireEvent.click(screen.getByRole('tab', { name: /^idle$/i }));
    await waitFor(() => expect(screen.getAllByTestId('agent-row')).toHaveLength(1));
    expect(screen.getByText('bot-2')).toBeInTheDocument();
  });

  it('empty filter shows the CLI hint', async () => {
    server.use(http.get('/api/agents', () => HttpResponse.json([])));
    wrap(<Agents />);
    await waitFor(() => expect(screen.getByTestId('agents-empty')).toBeInTheDocument());
    expect(screen.getByTestId('agents-empty')).toHaveTextContent(/agent-center agent create/);
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

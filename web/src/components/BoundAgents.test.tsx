import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { BoundAgents } from './BoundAgents';

const agent = (extra: Record<string, unknown> = {}) => ({
  id: 'A1',
  name: 'bot-1',
  lifecycle: 'running',
  availability: 'available',
  worker_id: 'w-1',
  ...extra,
});

function wrap(workerId = 'w-1') {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <BoundAgents workerId={workerId} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(cleanup);

describe('BoundAgents', () => {
  it('lists only agents bound to this worker (filtered by worker_id)', async () => {
    server.use(
      http.get('/api/agents', () =>
        HttpResponse.json({
          agents: [
            agent({ id: 'A1', name: 'bot-1', worker_id: 'w-1' }),
            agent({ id: 'A2', name: 'bot-2', worker_id: 'w-2' }),
          ],
        }),
      ),
    );
    wrap('w-1');
    expect(await screen.findByText('bot-1')).toBeInTheDocument();
    expect(screen.queryByText('bot-2')).not.toBeInTheDocument();
    expect(screen.getAllByTestId('bound-agent-row')).toHaveLength(1);
    // in-UI hint: no unbind → archive via AgentDetail (PD lock)
    expect(screen.getByTestId('bound-agents-remove-hint')).toHaveTextContent('Archive');
  });

  it('empty state when no agents are bound', async () => {
    server.use(
      http.get('/api/agents', () =>
        HttpResponse.json({ agents: [agent({ worker_id: 'other' })] }),
      ),
    );
    wrap('w-1');
    expect(await screen.findByTestId('bound-agents-empty')).toBeInTheDocument();
  });

  it('Restart shown only for running agents', async () => {
    server.use(
      http.get('/api/agents', () =>
        HttpResponse.json({
          agents: [
            agent({ id: 'A1', name: 'run', lifecycle: 'running', worker_id: 'w-1' }),
            agent({ id: 'A2', name: 'stop', lifecycle: 'stopped', worker_id: 'w-1' }),
          ],
        }),
      ),
    );
    wrap('w-1');
    await screen.findByText('run');
    expect(screen.getAllByTestId('bound-agent-restart')).toHaveLength(1);
  });

  it('Restart posts to the agent lifecycle endpoint', async () => {
    let called = false;
    server.use(
      http.get('/api/agents', () =>
        HttpResponse.json({
          agents: [agent({ id: 'A1', name: 'run', lifecycle: 'running', worker_id: 'w-1' })],
        }),
      ),
      http.post('/api/agents/A1/restart', () => {
        called = true;
        return HttpResponse.json(agent({ id: 'A1', worker_id: 'w-1' }));
      }),
    );
    wrap('w-1');
    fireEvent.click(await screen.findByTestId('bound-agent-restart'));
    await waitFor(() => expect(called).toBe(true));
  });

  it('agent name links into AgentDetail; no standalone "Open →" link (T133/T143 parity)', async () => {
    server.use(
      http.get('/api/agents', () =>
        HttpResponse.json({
          agents: [agent({ id: 'A1', name: 'bot-1', worker_id: 'w-1' })],
        }),
      ),
    );
    wrap('w-1');
    const link = await screen.findByTestId('bound-agent-name-link');
    expect(link).toHaveTextContent('bot-1');
    expect(link).toHaveAttribute('href', expect.stringContaining('/agents/A1'));
    // the row-end "Open →" affordance is gone — the name itself is the open link
    expect(screen.queryByText('Open →')).not.toBeInTheDocument();
  });
});

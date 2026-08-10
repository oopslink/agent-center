import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { AgentCreateModal } from './AgentCreateModal';

function fleetWithWorker() {
  return http.get('/api/fleet', () =>
    HttpResponse.json({
      tasks: [],
      workers: [{ worker_id: 'w-1', name: 'worker-one', status: 'online', active_count: 0 }],
      pending_issues: [],
      generated_at: '2026-05-24T01:00:00Z',
    }),
  );
}

function wrap(onClose = () => {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AgentCreateModal onClose={onClose} />
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('AgentCreateModal runtime catalog contract', () => {
  it('prefills the organization default runtime profile from AI Runtime', async () => {
    wrap();
    const cli = await screen.findByTestId('agent-create-cli') as HTMLSelectElement;
    const model = screen.getByTestId('agent-create-model') as HTMLSelectElement;

    await waitFor(() => expect(cli.value).toBe('claude-code'));
    expect(model.value).toBe('claude-opus-4-8');
    expect([...cli.options].map((o) => o.value)).toEqual(['claude-code', 'codex']);
    expect([...model.options].map((o) => o.value)).toEqual(['claude-opus-4-8', 'claude-sonnet-4-6']);
    expect(screen.getByTestId('agent-create-model-description')).toHaveTextContent(/context/i);
  });

  it('filters models by CLI and submits the selected catalog pair', async () => {
    let postBody: Record<string, unknown> | undefined;
    server.use(
      fleetWithWorker(),
      http.post('/api/members/agent', async ({ request }) => {
        postBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(
          { id: 'mem-1', identity_id: 'agent-x', agent_id: 'A-new', display_name: 'bot-x' },
          { status: 201 },
        );
      }),
    );
    const onClose = vi.fn();
    wrap(onClose);

    fireEvent.click(await screen.findByTestId('agent-create-worker-trigger'));
    fireEvent.click(await screen.findByTestId('agent-create-worker-option'));
    fireEvent.change(screen.getByTestId('agent-create-name'), { target: { value: 'bot-x' } });

    fireEvent.change(await screen.findByTestId('agent-create-cli'), { target: { value: 'codex' } });
    const model = screen.getByTestId('agent-create-model') as HTMLSelectElement;
    await waitFor(() => expect(model.value).toBe('gpt-5'));
    expect([...model.options].map((o) => o.value)).toEqual(['gpt-5']);

    fireEvent.click(screen.getByTestId('agent-create-submit'));

    await waitFor(() => expect(postBody).toBeDefined());
    expect(postBody).toMatchObject({ display_name: 'bot-x', worker_id: 'w-1', cli: 'codex', model: 'gpt-5' });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });
});

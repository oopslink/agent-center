import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { AgentCreateModal } from './AgentCreateModal';

// A fleet snapshot carrying one worker so the required Worker picker is fillable.
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

describe('AgentCreateModal — model field', () => {
  it('renders model as an AI Runtime combobox, not a datalist free-text field', () => {
    wrap();
    const input = screen.getByTestId('agent-create-model') as HTMLInputElement;
    expect(input).toHaveAttribute('role', 'combobox');
    expect(input.getAttribute('list')).toBeNull();
  });

  it('lists AI Runtime models and does not submit a free-typed non-option value', async () => {
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

    const modelInput = screen.getByTestId('agent-create-model') as HTMLInputElement;
    await waitFor(() => expect(modelInput).not.toBeDisabled());
    fireEvent.focus(modelInput);
    expect(await screen.findByTestId('agent-create-model-options')).toHaveTextContent('Claude Opus 4.8');
    expect(screen.getByTestId('agent-create-model-options')).toHaveTextContent('frontier');
    fireEvent.keyDown(modelInput, { key: 'Escape' });

    // Wait for the fleet worker to load into the picker.
    fireEvent.click(await screen.findByTestId('agent-create-worker-trigger'));
    fireEvent.click(await screen.findByTestId('agent-create-worker-option'));

    fireEvent.change(screen.getByTestId('agent-create-name'), { target: { value: 'bot-x' } });

    const custom = 'my-org/custom-model-2099';
    fireEvent.focus(modelInput);
    fireEvent.change(modelInput, { target: { value: custom } });
    expect(screen.getByTestId('agent-create-model-empty')).toHaveTextContent('No matching runtime models');
    fireEvent.keyDown(modelInput, { key: 'Escape' });
    expect(modelInput).toHaveValue('Claude Opus 4.8');

    fireEvent.click(screen.getByTestId('agent-create-submit'));

    await waitFor(() => expect(postBody).toBeDefined());
    expect(postBody).toMatchObject({ display_name: 'bot-x', worker_id: 'w-1', model: 'claude-opus-4-8' });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });
});

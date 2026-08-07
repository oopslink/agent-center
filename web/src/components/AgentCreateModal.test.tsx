// AgentCreateModal — the "Model" field is an editable dropdown (<input list> +
// <datalist>): preset KNOWN_MODELS are offered as suggestions while any custom
// value can still be typed and submitted (the backend accepts any model string).
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { AgentCreateModal } from './AgentCreateModal';
import { KNOWN_MODELS } from '@/config/agent-defaults';

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
  it('renders the KNOWN_MODELS presets as a datalist bound to the model input', () => {
    wrap();
    const input = screen.getByTestId('agent-create-model') as HTMLInputElement;
    expect(input.getAttribute('list')).toBe('agent-create-model-list');
    const list = screen.getByTestId('agent-create-model-list');
    const values = Array.from(list.querySelectorAll('option')).map((o) => (o as HTMLOptionElement).value);
    expect(values).toEqual(KNOWN_MODELS);
    expect(values).toContain('claude-opus-4-8');
  });

  it('accepts a free-typed non-preset model value and submits it unchanged', async () => {
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

    // Wait for the fleet worker to load into the picker.
    fireEvent.click(await screen.findByTestId('agent-create-worker-trigger'));
    fireEvent.click(await screen.findByTestId('agent-create-worker-option'));

    fireEvent.change(screen.getByTestId('agent-create-name'), { target: { value: 'bot-x' } });

    const custom = 'my-org/custom-model-2099';
    expect(KNOWN_MODELS).not.toContain(custom);
    fireEvent.change(screen.getByTestId('agent-create-model'), { target: { value: custom } });
    expect((screen.getByTestId('agent-create-model') as HTMLInputElement).value).toBe(custom);

    fireEvent.click(screen.getByTestId('agent-create-submit'));

    await waitFor(() => expect(postBody).toBeDefined());
    expect(postBody).toMatchObject({ display_name: 'bot-x', worker_id: 'w-1', model: custom });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });
});

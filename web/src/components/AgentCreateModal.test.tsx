import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { AgentCreateModal } from './AgentCreateModal';

function runtimeCatalog() {
  return http.get('/api/ai-runtime', () =>
    HttpResponse.json({
      org_id: 'org-test',
      revision: 1,
      clis: [
        { id: 'runtime-cli-claude', key: 'claude-code', display_name: 'Claude Code', executable: 'claude', enabled: true },
        { id: 'runtime-cli-codex', key: 'codex', display_name: 'Codex CLI', executable: 'codex', enabled: true },
      ],
      models: [
        { id: 'runtime-model-opus', key: 'opus-4-8', model_key: 'opus-4-8', display_name: 'Opus', compatible_cli_keys: ['claude-code'], enabled: true },
        { id: 'runtime-model-sonnet', key: 'sonnet-disabled', model_key: 'sonnet-disabled', display_name: 'Sonnet Disabled', compatible_cli_keys: ['claude-code'], enabled: false },
        { id: 'runtime-model-gpt', key: 'gpt-5', model_key: 'gpt-5', display_name: 'GPT-5', compatible_cli_keys: ['codex'], enabled: true },
      ],
    }),
  );
}

function fleetWithWorker(capabilities = [
  { agent_cli: 'claude-code', detected: true, enabled: true },
  { agent_cli: 'codex', detected: true, enabled: true },
]) {
  return http.get('/api/fleet', () =>
    HttpResponse.json({
      tasks: [],
      workers: [{ worker_id: 'w-1', name: 'worker-one', status: 'online', active_count: 0, capabilities }],
      pending_issues: [],
      generated_at: '2026-05-24T01:00:00Z',
    }),
  );
}

function wrap(onClose = () => {}) {
  server.use(runtimeCatalog(), fleetWithWorker());
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AgentCreateModal onClose={onClose} />
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('AgentCreateModal — model field', () => {
  it('renders Runtime catalog CLI/model options filtered by the selected worker', async () => {
    wrap();
    fireEvent.click(await screen.findByTestId('agent-create-worker-trigger'));
    fireEvent.click(await screen.findByTestId('agent-create-worker-option'));

    const cli = screen.getByTestId('agent-create-cli') as HTMLSelectElement;
    await waitFor(() => expect(cli.value).toBe('claude-code'));
    expect(Array.from(cli.options).map((o) => o.value)).toEqual(['claude-code', 'codex']);

    const model = screen.getByTestId('agent-create-model') as HTMLSelectElement;
    expect(model.value).toBe('opus-4-8');
    expect(Array.from(model.options).map((o) => o.value)).toEqual(['opus-4-8']);

    fireEvent.change(cli, { target: { value: 'codex' } });
    await waitFor(() => expect((screen.getByTestId('agent-create-model') as HTMLSelectElement).value).toBe('gpt-5'));
    expect(Array.from((screen.getByTestId('agent-create-model') as HTMLSelectElement).options).map((o) => o.value)).toEqual(['gpt-5']);
  });

  it('submits the selected Runtime catalog pair unchanged', async () => {
    let postBody: Record<string, unknown> | undefined;
    server.use(
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
    fireEvent.change(screen.getByTestId('agent-create-cli'), { target: { value: 'codex' } });
    await waitFor(() => expect((screen.getByTestId('agent-create-model') as HTMLSelectElement).value).toBe('gpt-5'));

    fireEvent.click(screen.getByTestId('agent-create-submit'));

    await waitFor(() => expect(postBody).toBeDefined());
    expect(postBody).toMatchObject({ display_name: 'bot-x', worker_id: 'w-1', cli: 'codex', model: 'gpt-5' });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it('blocks creation when the selected worker has no enabled Runtime CLI capability', async () => {
    server.use(runtimeCatalog(), fleetWithWorker([{ agent_cli: 'claude-code', detected: true, enabled: false }]));
    const onClose = vi.fn();
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <AgentCreateModal onClose={onClose} />
      </QueryClientProvider>,
    );
    fireEvent.click(await screen.findByTestId('agent-create-worker-trigger'));
    fireEvent.click(await screen.findByTestId('agent-create-worker-option'));
    fireEvent.change(screen.getByTestId('agent-create-name'), { target: { value: 'bot-x' } });

    expect(await screen.findByTestId('agent-create-validation-error')).toHaveTextContent(/not enabled/i);
    expect(screen.getByTestId('agent-create-submit')).toBeDisabled();
    expect(onClose).not.toHaveBeenCalled();
  });
});

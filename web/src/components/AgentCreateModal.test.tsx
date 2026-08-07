import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { AgentCreateModal } from './AgentCreateModal';
import { qk } from '@/api/queryKeys';

const runtimeCatalog = {
  org_id: 'O-1',
  revision: 3,
  default_runtime_profile_id: 'rp-default',
  clis: [
    { id: 'cli-1', key: 'claude-code', display_name: 'Claude Code', executable: 'claude', required_features: [], enabled: true },
    { id: 'cli-2', key: 'codex', display_name: 'Codex', executable: 'codex', required_features: [], enabled: true },
  ],
  models: [
    { id: 'model-1', key: 'opus-runtime', model_key: 'opus-4-8', display_name: 'Opus 4.8', compatible_cli_keys: ['claude-code'], default_parameters: {}, enabled: true },
    { id: 'model-2', key: 'gpt-runtime', model_key: 'gpt-5.5', display_name: 'GPT-5.5', compatible_cli_keys: ['codex'], default_parameters: {}, enabled: true },
  ],
  profiles: [
    { id: 'rp-default', key: 'default-coding', name: 'Default coding', cli_key: 'claude-code', model_key: 'opus-runtime', parameters: {}, enabled: true },
  ],
};

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
  qc.setQueryData(qk.aiRuntime(), runtimeCatalog);
  return render(
    <QueryClientProvider client={qc}>
      <AgentCreateModal onClose={onClose} />
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('AgentCreateModal — model field', () => {
  it('renders Runtime Catalog-backed CLI and model selectors', async () => {
    wrap();
    const cli = screen.getByTestId('agent-create-cli') as HTMLSelectElement;
    const model = screen.getByTestId('agent-create-model') as HTMLSelectElement;
    await waitFor(() => expect(cli.value).toBe('claude-code'));
    expect(Array.from(cli.options).map((o) => o.value)).toEqual(['claude-code', 'codex']);
    expect(Array.from(model.options).map((o) => o.value)).toEqual(['opus-4-8']);
  });

  it('submits a selected catalog model value', async () => {
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

    fireEvent.change(screen.getByTestId('agent-create-cli'), { target: { value: 'codex' } });
    await waitFor(() => expect((screen.getByTestId('agent-create-model') as HTMLSelectElement).value).toBe('gpt-5.5'));

    fireEvent.click(screen.getByTestId('agent-create-submit'));

    await waitFor(() => expect(postBody).toBeDefined());
    expect(postBody).toMatchObject({ display_name: 'bot-x', worker_id: 'w-1', cli: 'codex', model: 'gpt-5.5' });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });
});

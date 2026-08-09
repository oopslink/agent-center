import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { AgentCreateModal } from './AgentCreateModal';

function runtimeCatalog() {
  return {
    org_id: 'org-test',
    revision: 7,
    default_runtime_profile_id: 'profile-codex',
    clis: [
      { id: 'cli-codex', key: 'codex', display_name: 'Codex CLI', executable: 'codex', enabled: true },
      { id: 'cli-claude', key: 'claude-code', display_name: 'Claude Code', executable: 'claude', enabled: true },
    ],
    models: [
      { id: 'model-gpt', key: 'gpt-5', model_key: 'gpt-5', display_name: 'GPT-5', compatible_cli_keys: ['codex'], enabled: true },
      { id: 'model-sonnet', key: 'claude-sonnet', model_key: 'claude-sonnet-4-6', display_name: 'Claude Sonnet', compatible_cli_keys: ['claude-code'], enabled: true },
    ],
    profiles: [
      { id: 'profile-codex', key: 'default-codex', name: 'Default Codex', cli_key: 'codex', model_key: 'gpt-5', parameters: {}, enabled: true },
    ],
  };
}

function fleetWithWorkers(workers: unknown[]) {
  return http.get('/api/fleet', () =>
    HttpResponse.json({
      tasks: [],
      workers,
      pending_issues: [],
      generated_at: '2026-05-24T01:00:00Z',
    }),
  );
}

function worker(worker_id: string, name: string, cliKeys: string[]) {
  return {
    worker_id,
    name,
    status: 'online',
    active_count: 0,
    capabilities: cliKeys.map((agent_cli) => ({ agent_cli, detected: true, enabled: true })),
  };
}

function runtimeHandler(catalog = runtimeCatalog()) {
  return http.get('/api/ai-runtime', () => HttpResponse.json(catalog));
}

function wrap(onClose = () => {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AgentCreateModal onClose={onClose} />
    </QueryClientProvider>,
  );
}

async function chooseWorker(value: string) {
  fireEvent.click(await screen.findByTestId('agent-create-worker-trigger'));
  const options = await screen.findAllByTestId('agent-create-worker-option');
  const option = options.find((node) => node.getAttribute('data-value') === value);
  expect(option).toBeDefined();
  fireEvent.click(option!);
}

afterEach(() => cleanup());

describe('AgentCreateModal — runtime capability selection', () => {
  it('defaults from AI Runtime and submits the worker-supported CLI/model pair', async () => {
    let postBody: Record<string, unknown> | undefined;
    server.use(
      runtimeHandler(),
      fleetWithWorkers([worker('w-1', 'worker-one', ['codex', 'claude-code'])]),
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

    await chooseWorker('w-1');
    await waitFor(() => expect(screen.getByTestId('agent-create-cli')).toHaveValue('codex'));
    expect(screen.getByTestId('agent-create-model')).toHaveValue('gpt-5');

    fireEvent.change(screen.getByTestId('agent-create-cli'), { target: { value: 'claude-code' } });
    await waitFor(() => expect(screen.getByTestId('agent-create-model')).toHaveValue('claude-sonnet-4-6'));
    fireEvent.change(screen.getByTestId('agent-create-name'), { target: { value: 'bot-x' } });
    fireEvent.click(screen.getByTestId('agent-create-submit'));

    await waitFor(() => expect(postBody).toBeDefined());
    expect(postBody).toMatchObject({
      display_name: 'bot-x',
      worker_id: 'w-1',
      cli: 'claude-code',
      model: 'claude-sonnet-4-6',
    });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it('recomputes CLI/model choices when the selected worker capability changes', async () => {
    server.use(
      runtimeHandler(),
      fleetWithWorkers([
        worker('w-codex', 'codex-box', ['codex']),
        worker('w-claude', 'claude-box', ['claude-code']),
      ]),
    );
    wrap();

    await chooseWorker('w-codex');
    await waitFor(() => expect(screen.getByTestId('agent-create-cli')).toHaveValue('codex'));
    expect(screen.getByTestId('agent-create-model')).toHaveValue('gpt-5');

    await chooseWorker('w-claude');
    await waitFor(() => expect(screen.getByTestId('agent-create-cli')).toHaveValue('claude-code'));
    expect(screen.getByTestId('agent-create-model')).toHaveValue('claude-sonnet-4-6');
  });

  it('blocks submit when the selected worker has no usable runtime pair', async () => {
    let posted = false;
    server.use(
      runtimeHandler(),
      fleetWithWorkers([worker('w-empty', 'empty-box', ['opencode'])]),
      http.post('/api/members/agent', () => {
        posted = true;
        return HttpResponse.json({});
      }),
    );
    wrap();

    await chooseWorker('w-empty');
    fireEvent.change(screen.getByTestId('agent-create-name'), { target: { value: 'blocked-bot' } });

    await waitFor(() => expect(screen.getByTestId('agent-create-submit')).toBeDisabled());
    expect(screen.getByTestId('agent-create-runtime-hint')).toHaveTextContent(/no enabled runtime cli/i);
    expect(posted).toBe(false);
  });
});

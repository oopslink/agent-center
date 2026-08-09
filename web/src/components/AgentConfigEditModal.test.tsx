// T236 — AgentConfigEditModal: edit LLM config, confirm (restart warning), then
// PATCH the config and restart a running agent.
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { AgentConfigEditModal } from './AgentConfigEditModal';
import type { Agent } from '@/api/types';

const base: Agent = {
  id: 'A1', organization_id: 'O-1', name: 'bot-1', description: '',
  model: 'gpt-5', cli: 'codex', reasoning: '', mode: '', provider: '',
  env_vars: {}, worker_id: 'w-1', lifecycle: 'running', availability: 'busy',
  created_by: 'user:hayang', version: 1, created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T02:00:00Z',
};

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
      { id: 'model-gpt-mini', key: 'gpt-5-mini', model_key: 'gpt-5-mini', display_name: 'GPT-5 mini', compatible_cli_keys: ['codex'], enabled: true },
      { id: 'model-sonnet', key: 'claude-sonnet', model_key: 'claude-sonnet-4-6', display_name: 'Claude Sonnet', compatible_cli_keys: ['claude-code'], enabled: true },
    ],
    profiles: [
      { id: 'profile-codex', key: 'default-codex', name: 'Default Codex', cli_key: 'codex', model_key: 'gpt-5', parameters: {}, enabled: true },
    ],
  };
}

function worker(worker_id: string, cliKeys: string[]) {
  return {
    worker_id,
    name: worker_id,
    status: 'online',
    capabilities: cliKeys.map((agent_cli) => ({ agent_cli, detected: true, enabled: true })),
  };
}

function wrap(agent: Agent, onClose = () => {}, options?: { catalog?: unknown; workers?: unknown[] }) {
  server.use(
    http.get('/api/ai-runtime', () => HttpResponse.json(options?.catalog ?? runtimeCatalog())),
    http.get('/api/workers', () => HttpResponse.json({ workers: options?.workers ?? [worker('w-1', ['codex', 'claude-code'])] })),
  );
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AgentConfigEditModal agent={agent} onClose={onClose} />
    </QueryClientProvider>,
  );
}

async function waitRuntimeReady() {
  await waitFor(() => expect(screen.getByTestId('agent-config-executor-add')).toBeEnabled());
}

afterEach(() => cleanup());

describe('AgentConfigEditModal (T236)', () => {
  it('prefills the form from the agent config', async () => {
    wrap({ ...base, model: 'claude-sonnet-4-6', cli: 'claude-code', reasoning: 'high', mode: 'plan', provider: 'anthropic' });
    await waitRuntimeReady();
    expect(screen.getByTestId('agent-config-model')).toHaveValue('claude-sonnet-4-6');
    expect(screen.getByTestId('agent-config-cli')).toHaveValue('claude-code');
    expect(screen.getByTestId('agent-config-reasoning')).toHaveValue('high');
    expect(screen.getByTestId('agent-config-mode')).toHaveValue('plan');
    expect(screen.getByTestId('agent-config-provider')).toHaveValue('anthropic');
  });

  it('runtime: model choices follow the selected worker-supported CLI', async () => {
    wrap(base);
    await waitRuntimeReady();
    expect(screen.getByTestId('agent-config-cli')).toHaveValue('codex');
    expect(screen.getByTestId('agent-config-model')).toHaveValue('gpt-5');
    fireEvent.change(screen.getByTestId('agent-config-cli'), { target: { value: 'claude-code' } });
    await waitFor(() => expect(screen.getByTestId('agent-config-model')).toHaveValue('claude-sonnet-4-6'));
  });

  it('runtime: preserves legacy values but blocks confirmation until changed', async () => {
    let patched = false;
    server.use(
      http.patch('/api/agents/:id/config', () => {
        patched = true;
        return HttpResponse.json({ ...base });
      }),
    );
    wrap(
      { ...base, cli: 'claude-code', model: 'legacy-model' },
      undefined,
      { workers: [worker('w-1', ['codex'])] },
    );

    await waitFor(() => expect(screen.getByTestId('agent-config-cli')).toHaveValue('claude-code'));
    expect(screen.getByTestId('agent-config-model')).toHaveValue('legacy-model');
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));

    expect(screen.queryByTestId('confirm-modal')).toBeNull();
    expect(await screen.findByTestId('agent-config-runtime-error')).toHaveTextContent(/worker-supported cli\/model/i);
    expect(patched).toBe(false);
  });

  it('runtime: blocks saving when the bound worker has no usable runtime pair', async () => {
    let patched = false;
    server.use(
      http.patch('/api/agents/:id/config', () => {
        patched = true;
        return HttpResponse.json({ ...base });
      }),
    );
    wrap(base, undefined, { workers: [worker('w-1', ['opencode'])] });

    fireEvent.click(screen.getByTestId('agent-config-edit-save'));

    expect(screen.queryByTestId('confirm-modal')).toBeNull();
    expect(await screen.findByTestId('agent-config-runtime-error')).toHaveTextContent(/worker-supported cli\/model/i);
    expect(patched).toBe(false);
  });

  it('env vars: prefills KEY=value lines and PATCHes env_vars', async () => {
    let patchBody: Record<string, unknown> | undefined;
    server.use(
      http.patch('/api/agents/:id/config', async ({ request }) => {
        patchBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ ...base });
      }),
      http.post('/api/agents/:id/restart', () => HttpResponse.json({ ...base })),
    );
    wrap({
      ...base,
      env_vars: {
        FOO: 'bar',
        ANTHROPIC_BASE_URL: 'https://anthropic.example',
      },
    });
    await waitRuntimeReady();

    const env = screen.getByTestId('agent-config-env') as HTMLTextAreaElement;
    expect(env.value).toBe('ANTHROPIC_BASE_URL=https://anthropic.example\nFOO=bar');
    fireEvent.change(env, { target: { value: 'FOO=baz\nEMPTY=' } });
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));

    await waitFor(() => expect(patchBody).toBeDefined());
    expect(patchBody).toMatchObject({ env_vars: { FOO: 'baz', EMPTY: '' } });
  });

  it('env vars: invalid lines block confirmation and show an error', () => {
    wrap(base);
    fireEvent.change(screen.getByTestId('agent-config-env'), { target: { value: '1BAD=value' } });
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    expect(screen.queryByTestId('confirm-modal')).toBeNull();
    expect(screen.getByTestId('agent-config-env-error')).toHaveTextContent(/invalid environment variable name/i);
  });

  it('Save shows a restart confirmation for a RUNNING agent, then PATCHes config + restarts', async () => {
    let patchBody: Record<string, unknown> | undefined;
    let restarted = false;
    server.use(
      http.patch('/api/agents/:id/config', async ({ request }) => {
        patchBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ ...base, reasoning: 'high' });
      }),
      http.post('/api/agents/:id/restart', () => {
        restarted = true;
        return HttpResponse.json({ ...base });
      }),
    );
    const onClose = vi.fn();
    wrap(base, onClose);
    await waitRuntimeReady();

    fireEvent.change(screen.getByTestId('agent-config-reasoning'), { target: { value: 'high' } });
    fireEvent.change(screen.getByTestId('agent-config-model'), { target: { value: 'gpt-5-mini' } });
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    const confirm = await screen.findByTestId('confirm-modal');
    expect(confirm).toHaveTextContent(/restart/i);

    fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(patchBody).toMatchObject({ model: 'gpt-5-mini', cli: 'codex', reasoning: 'high' });
    expect(restarted).toBe(true);
  });

  it('does NOT restart a stopped agent (config applies on next start)', async () => {
    let restarted = false;
    server.use(
      http.patch('/api/agents/:id/config', () => HttpResponse.json({ ...base, lifecycle: 'stopped' })),
      http.post('/api/agents/:id/restart', () => {
        restarted = true;
        return HttpResponse.json({ ...base });
      }),
    );
    const onClose = vi.fn();
    wrap({ ...base, lifecycle: 'stopped' }, onClose);
    await waitRuntimeReady();
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    const confirm = await screen.findByTestId('confirm-modal');
    expect(confirm).toHaveTextContent(/next time it starts/i);
    fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(restarted).toBe(false);
  });

  it('concurrency: prefills max + executor chips; status reflects "truly parallel"', async () => {
    wrap({
      ...base,
      max_concurrent_tasks: 3,
      allowed_executors: [
        { cli: 'claude-code', model: 'claude-sonnet-4-6' },
        { cli: 'codex', model: 'gpt-5-mini' },
      ],
    });
    await waitRuntimeReady();
    expect(screen.getByTestId('agent-config-max-concurrent')).toHaveValue(3);
    expect(screen.getAllByTestId('agent-config-executor-chip')).toHaveLength(2);
    expect(screen.getAllByTestId('agent-config-executor-chip')[0]).toHaveAttribute('data-supported', 'true');
    const status = screen.getByTestId('agent-config-concurrency-status');
    expect(status).toHaveAttribute('data-enabled', 'true');
    expect(status).toHaveTextContent(/up to 3/i);
  });

  it('concurrency: a default agent (no executors) shows DISABLED single-active', async () => {
    wrap(base);
    await waitRuntimeReady();
    const status = screen.getByTestId('agent-config-concurrency-status');
    expect(status).toHaveAttribute('data-enabled', 'false');
    expect(status).toHaveTextContent(/single-active/i);
    expect(screen.getByTestId('agent-config-executors-empty')).toBeInTheDocument();
  });

  it('concurrency: add then remove an executor profile updates the chips', async () => {
    wrap(base);
    await waitRuntimeReady();
    expect(screen.queryAllByTestId('agent-config-executor-chip')).toHaveLength(0);
    fireEvent.change(screen.getByTestId('agent-config-executor-cli'), { target: { value: 'claude-code' } });
    await waitFor(() => expect(screen.getByTestId('agent-config-executor-model')).toHaveValue('claude-sonnet-4-6'));
    fireEvent.click(screen.getByTestId('agent-config-executor-add'));
    expect(screen.getAllByTestId('agent-config-executor-chip')).toHaveLength(1);
    expect(screen.getByTestId('agent-config-executor-chip')).toHaveTextContent('claude-sonnet-4-6');
    fireEvent.click(screen.getByTestId('agent-config-executor-remove'));
    expect(screen.queryAllByTestId('agent-config-executor-chip')).toHaveLength(0);
  });

  it('concurrency: PATCH body carries normalized max_concurrent_tasks + allowed_executors', async () => {
    let patchBody: Record<string, unknown> | undefined;
    server.use(
      http.patch('/api/agents/:id/config', async ({ request }) => {
        patchBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ ...base });
      }),
      http.post('/api/agents/:id/restart', () => HttpResponse.json({ ...base })),
    );
    wrap(base);
    await waitRuntimeReady();
    fireEvent.change(screen.getByTestId('agent-config-max-concurrent'), { target: { value: '4' } });
    fireEvent.change(screen.getByTestId('agent-config-executor-model'), { target: { value: 'gpt-5-mini' } });
    fireEvent.click(screen.getByTestId('agent-config-executor-add'));
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(patchBody).toBeDefined());
    expect(patchBody).toMatchObject({
      max_concurrent_tasks: 4,
      allowed_executors: [{ cli: 'codex', model: 'gpt-5-mini' }],
    });
  });

  it('concurrency: unsupported legacy executor profiles block submit until removed', async () => {
    let patched = false;
    server.use(
      http.patch('/api/agents/:id/config', () => {
        patched = true;
        return HttpResponse.json({ ...base });
      }),
    );
    wrap({ ...base, allowed_executors: [{ cli: 'claude-code', model: 'legacy-model' }] });
    await waitRuntimeReady();
    expect(screen.getByTestId('agent-config-executor-chip')).toHaveAttribute('data-supported', 'false');

    fireEvent.click(screen.getByTestId('agent-config-edit-save'));

    expect(screen.queryByTestId('confirm-modal')).toBeNull();
    expect(await screen.findByTestId('agent-config-runtime-error')).toHaveTextContent(/executor profiles/i);
    expect(patched).toBe(false);
  });

  it('T566: auto_assignable toggle defaults ON and PATCHes false when turned off', async () => {
    let patchBody: Record<string, unknown> | undefined;
    server.use(
      http.patch('/api/agents/:id/config', async ({ request }) => {
        patchBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ ...base });
      }),
      http.post('/api/agents/:id/restart', () => HttpResponse.json({ ...base })),
    );
    wrap(base);
    await waitRuntimeReady();
    const toggle = screen.getByTestId('agent-config-auto-assignable');
    expect(toggle).toHaveAttribute('aria-checked', 'true');
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-checked', 'false');
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(patchBody).toBeDefined());
    expect(patchBody).toMatchObject({ auto_assignable: false });
  });

  it('executor git worktree defaults OFF and PATCHes only this agent ON', async () => {
    let patchBody: Record<string, unknown> | undefined;
    server.use(
      http.patch('/api/agents/:id/config', async ({ request }) => {
        patchBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ ...base });
      }),
      http.post('/api/agents/:id/restart', () => HttpResponse.json({ ...base })),
    );
    wrap(base);
    await waitRuntimeReady();
    const toggle = screen.getByTestId('agent-config-executor-git-worktree');
    expect(toggle).toHaveAttribute('aria-checked', 'false');
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-checked', 'true');
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(patchBody).toBeDefined());
    expect(patchBody).toMatchObject({ executor_git_worktree: true });
  });

  it('T728: include-description toggle defaults ON and PATCHes false when turned off', async () => {
    let patchBody: Record<string, unknown> | undefined;
    server.use(
      http.patch('/api/agents/:id/config', async ({ request }) => {
        patchBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ ...base });
      }),
      http.post('/api/agents/:id/restart', () => HttpResponse.json({ ...base })),
    );
    wrap(base);
    await waitRuntimeReady();
    const toggle = screen.getByTestId('agent-config-include-description');
    expect(toggle).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByTestId('agent-config-include-description-restart-hint')).toBeInTheDocument();
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-checked', 'false');
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(patchBody).toBeDefined());
    expect(patchBody).toMatchObject({ include_description_in_system_prompt: false });
  });

  it('T728: include-description toggle echoes the persisted false value', () => {
    wrap({ ...base, include_description_in_system_prompt: false });
    expect(screen.getByTestId('agent-config-include-description')).toHaveAttribute('aria-checked', 'false');
  });

  it('Cancel on the confirm keeps the modal open (no PATCH)', async () => {
    let patched = false;
    server.use(
      http.patch('/api/agents/:id/config', () => {
        patched = true;
        return HttpResponse.json({ ...base });
      }),
    );
    wrap(base);
    await waitRuntimeReady();
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-cancel'));
    expect(screen.queryByTestId('confirm-modal')).toBeNull();
    expect(screen.getByTestId('agent-config-edit-modal')).toBeInTheDocument();
    expect(patched).toBe(false);
  });
});

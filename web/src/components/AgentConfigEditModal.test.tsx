// T236 — AgentConfigEditModal: edit LLM config, confirm (restart warning), then
// PATCH the config and restart a running agent.
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { AgentConfigEditModal } from './AgentConfigEditModal';
import type { Agent } from '@/api/types';
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
    { id: 'rp-codex', key: 'codex-review', name: 'Codex review', cli_key: 'codex', model_key: 'gpt-runtime', parameters: {}, enabled: true },
  ],
};

const base: Agent = {
  id: 'A1', organization_id: 'O-1', name: 'bot-1', description: '',
  model: 'opus-4-8', cli: 'claude-code', reasoning: '', mode: '', provider: '',
  env_vars: {}, worker_id: 'w-1', lifecycle: 'running', availability: 'busy',
  created_by: 'user:hayang', version: 1, created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T02:00:00Z',
};

function wrap(agent: Agent, onClose = () => {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  qc.setQueryData(qk.aiRuntime(), runtimeCatalog);
  return render(
    <QueryClientProvider client={qc}>
      <AgentConfigEditModal agent={agent} onClose={onClose} />
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('AgentConfigEditModal (T236)', () => {
  it('prefills the form from the agent config', () => {
    wrap({ ...base, model: 'gpt-5.5', cli: 'codex', reasoning: 'high', mode: 'plan', provider: 'anthropic' });
    expect((screen.getByTestId('agent-config-model') as HTMLSelectElement).value).toBe('gpt-5.5');
    expect((screen.getByTestId('agent-config-cli') as HTMLSelectElement).value).toBe('codex');
    expect((screen.getByTestId('agent-config-reasoning') as HTMLSelectElement).value).toBe('high');
    expect((screen.getByTestId('agent-config-mode') as HTMLInputElement).value).toBe('plan');
    expect((screen.getByTestId('agent-config-provider') as HTMLInputElement).value).toBe('anthropic');
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

    // edit a couple of fields
    fireEvent.change(screen.getByTestId('agent-config-reasoning'), { target: { value: 'high' } });
    fireEvent.change(screen.getByTestId('agent-config-cli'), { target: { value: 'codex' } });
    await waitFor(() => expect((screen.getByTestId('agent-config-model') as HTMLSelectElement).value).toBe('gpt-5.5'));
    // Save → confirm dialog (running → restart warning)
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    const confirm = await screen.findByTestId('confirm-modal');
    expect(confirm).toHaveTextContent(/restart/i);

    // confirm → PATCH then restart, then close
    fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(patchBody).toMatchObject({ model: 'gpt-5.5', cli: 'codex', reasoning: 'high' });
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
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    const confirm = await screen.findByTestId('confirm-modal');
    // stopped → wording is about next start, not restart
    expect(confirm).toHaveTextContent(/next time it starts/i);
    fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(restarted).toBe(false);
  });

  // v2.18.1 (issue-8746a5b9): executor concurrency config.
  it('concurrency: prefills max + executor chips; status reflects "truly parallel"', () => {
    wrap({
      ...base,
      max_concurrent_tasks: 3,
      allowed_executors: [
        { cli: 'claude-code', model: 'opus-4-8' },
        { cli: 'codex', model: 'gpt-5.5' },
      ],
    });
    expect((screen.getByTestId('agent-config-max-concurrent') as HTMLInputElement).value).toBe('3');
    expect(screen.getAllByTestId('agent-config-executor-chip')).toHaveLength(2);
    const status = screen.getByTestId('agent-config-concurrency-status');
    expect(status).toHaveAttribute('data-enabled', 'true');
    expect(status).toHaveTextContent(/up to 3/i);
  });

  it('concurrency: a default agent (no executors) shows DISABLED single-active', () => {
    wrap(base);
    const status = screen.getByTestId('agent-config-concurrency-status');
    expect(status).toHaveAttribute('data-enabled', 'false');
    expect(status).toHaveTextContent(/single-active/i);
    expect(screen.getByTestId('agent-config-executors-empty')).toBeInTheDocument();
  });

  it('concurrency: add then remove an executor profile updates the chips', async () => {
    wrap(base);
    expect(screen.queryAllByTestId('agent-config-executor-chip')).toHaveLength(0);
    fireEvent.change(screen.getByTestId('agent-config-executor-mode'), { target: { value: 'override' } });
    await waitFor(() => expect((screen.getByTestId('agent-config-executor-cli') as HTMLSelectElement).options.length).toBeGreaterThan(1));
    fireEvent.change(screen.getByTestId('agent-config-executor-cli'), { target: { value: 'codex' } });
    fireEvent.change(screen.getByTestId('agent-config-executor-model'), { target: { value: 'gpt-runtime' } });
    fireEvent.click(screen.getByTestId('agent-config-executor-add'));
    expect(screen.getAllByTestId('agent-config-executor-chip')).toHaveLength(1);
    expect(screen.getByTestId('agent-config-executor-chip')).toHaveTextContent('gpt-5.5');
    fireEvent.click(screen.getByTestId('agent-config-executor-remove'));
    expect(screen.queryAllByTestId('agent-config-executor-chip')).toHaveLength(0);
  });

  it('concurrency: PATCH body carries max_concurrent_tasks + allowed_executors', async () => {
    let patchBody: Record<string, unknown> | undefined;
    server.use(
      http.patch('/api/agents/:id/config', async ({ request }) => {
        patchBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ ...base });
      }),
      http.post('/api/agents/:id/restart', () => HttpResponse.json({ ...base })),
    );
    wrap(base);
    fireEvent.change(screen.getByTestId('agent-config-max-concurrent'), { target: { value: '4' } });
    fireEvent.change(screen.getByTestId('agent-config-executor-mode'), { target: { value: 'profile' } });
    await waitFor(() => expect((screen.getByTestId('agent-config-executor-profile') as HTMLSelectElement).options.length).toBeGreaterThan(1));
    fireEvent.change(screen.getByTestId('agent-config-executor-profile'), { target: { value: 'rp-codex' } });
    fireEvent.click(screen.getByTestId('agent-config-executor-add'));
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(patchBody).toBeDefined());
    expect(patchBody).toMatchObject({
      max_concurrent_tasks: 4,
      allowed_executors: [{ cli: 'codex', model: 'gpt-5.5', runtime_selection: { mode: 'profile', profile_id: 'rp-codex' } }],
    });
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
    wrap(base); // base has no auto_assignable → defaults ON
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
    wrap(base); // base has no include_description_in_system_prompt → defaults ON
    const toggle = screen.getByTestId('agent-config-include-description');
    expect(toggle).toHaveAttribute('aria-checked', 'true');
    // restart-to-apply hint is shown next to the switch.
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

  it('model: renders Runtime Catalog compatible models as a select', () => {
    wrap(base);
    const cli = screen.getByTestId('agent-config-cli') as HTMLSelectElement;
    const model = screen.getByTestId('agent-config-model') as HTMLSelectElement;
    expect(Array.from(cli.options).map((o) => o.value)).toEqual(['claude-code', 'codex']);
    expect(Array.from(model.options).map((o) => o.value)).toEqual(['opus-4-8']);
  });

  it('model: an unmapped historical value is readable but cannot be saved as-is', () => {
    const custom = 'my-org/custom-model-2099';
    wrap({ ...base, model: custom });
    expect((screen.getByTestId('agent-config-model') as HTMLSelectElement).value).toBe(custom);
    expect(screen.getByTestId('agent-config-edit-save')).toBeDisabled();
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
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-cancel'));
    expect(screen.queryByTestId('confirm-modal')).toBeNull();
    expect(screen.getByTestId('agent-config-edit-modal')).toBeInTheDocument();
    expect(patched).toBe(false);
  });
});

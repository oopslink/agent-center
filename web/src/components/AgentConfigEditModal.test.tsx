// T236 — AgentConfigEditModal: edit LLM config, confirm (restart warning), then
// PATCH the config and restart a running agent.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { AgentConfigEditModal } from './AgentConfigEditModal';
import type { Agent } from '@/api/types';

const base: Agent = {
  id: 'A1', organization_id: 'O-1', name: 'bot-1', description: '',
  model: 'claude-opus-4-8', cli: 'claude-code', reasoning: '', mode: '', provider: '',
  env_vars: {}, worker_id: 'w-1', lifecycle: 'running', availability: 'busy',
  created_by: 'user:hayang', version: 1, created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T02:00:00Z',
};

function wrap(agent: Agent, onClose = () => {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AgentConfigEditModal agent={agent} onClose={onClose} />
    </QueryClientProvider>,
  );
}

function runtimeCatalog() {
  return {
    org_id: 'O-1',
    revision: 8,
    clis: [
      { id: 'cli-claude', key: 'claude-code', display_name: 'Claude Code', executable: 'claude', enabled: true },
      { id: 'cli-codex', key: 'codex', display_name: 'Codex', executable: 'codex', enabled: true },
    ],
    models: [
      { id: 'model-claude-opus', key: 'claude-opus', model_key: 'claude-opus-4-8', display_name: 'Claude Opus', compatible_cli_keys: ['claude-code'], enabled: true },
      { id: 'model-claude-sonnet', key: 'claude-sonnet', model_key: 'claude-sonnet-4-6', display_name: 'Claude Sonnet', compatible_cli_keys: ['claude-code'], enabled: true },
      { id: 'model-opus-short', key: 'opus-short', model_key: 'opus-4-8', display_name: 'Opus executor', compatible_cli_keys: ['claude-code'], enabled: true },
      { id: 'model-gpt', key: 'gpt-5-5', model_key: 'gpt-5.5', display_name: 'GPT-5.5', compatible_cli_keys: ['codex'], enabled: true },
    ],
  };
}

async function waitRuntimeReady() {
  await waitFor(() => expect(screen.getByTestId('agent-config-edit-save')).not.toBeDisabled());
}

beforeEach(() => {
  server.use(http.get('/api/ai-runtime', () => HttpResponse.json(runtimeCatalog())));
});

afterEach(() => cleanup());

describe('AgentConfigEditModal (T236)', () => {
  it('prefills the form from the agent config', () => {
    wrap({ ...base, model: 'claude-sonnet-4-6', cli: 'codex', reasoning: 'high', mode: 'plan', provider: 'anthropic' });
    expect((screen.getByTestId('agent-config-model') as HTMLSelectElement).value).toBe('claude-sonnet-4-6');
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
    await waitRuntimeReady();
    fireEvent.change(env, { target: { value: 'FOO=baz\nEMPTY=' } });
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));

    await waitFor(() => expect(patchBody).toBeDefined());
    expect(patchBody).toMatchObject({ env_vars: { FOO: 'baz', EMPTY: '' } });
  });

  it('env vars: invalid lines block confirmation and show an error', async () => {
    wrap(base);
    await waitRuntimeReady();
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

    // edit a couple of fields
    fireEvent.change(screen.getByTestId('agent-config-reasoning'), { target: { value: 'high' } });
    fireEvent.change(screen.getByTestId('agent-config-model'), { target: { value: 'claude-sonnet-4-6' } });
    // Save → confirm dialog (running → restart warning)
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    const confirm = await screen.findByTestId('confirm-modal');
    expect(confirm).toHaveTextContent(/restart/i);

    // confirm → PATCH then restart, then close
    fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(patchBody).toMatchObject({ model: 'claude-sonnet-4-6', cli: 'claude-code', reasoning: 'high' });
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
    await waitRuntimeReady();
    expect(screen.queryAllByTestId('agent-config-executor-chip')).toHaveLength(0);
    fireEvent.change(screen.getByTestId('agent-config-executor-cli'), { target: { value: 'codex' } });
    fireEvent.change(screen.getByTestId('agent-config-executor-model'), { target: { value: 'gpt-5.5' } });
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
    await waitRuntimeReady();
    fireEvent.change(screen.getByTestId('agent-config-max-concurrent'), { target: { value: '4' } });
    fireEvent.change(screen.getByTestId('agent-config-executor-model'), { target: { value: 'opus-4-8' } });
    fireEvent.click(screen.getByTestId('agent-config-executor-add'));
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(patchBody).toBeDefined());
    expect(patchBody).toMatchObject({
      max_concurrent_tasks: 4,
      allowed_executors: [{ cli: 'claude-code', model: 'opus-4-8' }],
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
    wrap(base); // base has no include_description_in_system_prompt → defaults ON
    await waitRuntimeReady();
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

  it('model: renders catalog models compatible with the selected CLI', async () => {
    wrap(base);
    await waitRuntimeReady();
    const modelSelect = screen.getByTestId('agent-config-model');
    const values = Array.from(modelSelect.querySelectorAll('option')).map((o) => (o as HTMLOptionElement).value);
    expect(values).toContain('claude-opus-4-8');
    expect(values).toContain('claude-sonnet-4-6');
    expect(values).not.toContain('gpt-5.5');
  });

  it('model: search narrows compatible catalog options', async () => {
    wrap(base);
    await waitRuntimeReady();
    fireEvent.change(screen.getByTestId('agent-config-model-search'), { target: { value: 'sonnet' } });
    const values = Array.from(screen.getByTestId('agent-config-model').querySelectorAll('option'))
      .map((o) => (o as HTMLOptionElement).value);
    expect(values).toContain('claude-sonnet-4-6');
    expect(values).not.toContain('opus-4-8');
  });

  it('model: changing CLI clears an incompatible model instead of replacing it', async () => {
    wrap(base);
    await waitRuntimeReady();
    expect((screen.getByTestId('agent-config-model') as HTMLSelectElement).value).toBe('claude-opus-4-8');
    fireEvent.change(screen.getByTestId('agent-config-cli'), { target: { value: 'codex' } });
    expect((screen.getByTestId('agent-config-model') as HTMLSelectElement).value).toBe('');
    expect(screen.getByTestId('agent-config-edit-save')).toBeDisabled();
    fireEvent.change(screen.getByTestId('agent-config-model'), { target: { value: 'gpt-5.5' } });
    expect(screen.getByTestId('agent-config-edit-save')).not.toBeDisabled();
  });

  it('model: stale current values remain visible as unavailable and block save', async () => {
    wrap({ ...base, cli: 'codex', model: 'deleted-model' });
    await waitFor(() => expect(screen.getByTestId('agent-config-runtime-catalog-state')).toHaveAttribute('data-empty', 'false'));
    expect((screen.getByTestId('agent-config-model') as HTMLSelectElement).value).toBe('deleted-model');
    expect(screen.getByTestId('agent-config-runtime-selection-error')).toBeInTheDocument();
    expect(screen.getByTestId('agent-config-edit-save')).toBeDisabled();
  });

  it('concurrency: stale executor pairs remain visible as unavailable and block save', async () => {
    wrap({
      ...base,
      max_concurrent_tasks: 2,
      allowed_executors: [{ cli: 'codex', model: 'deleted-executor-model' }],
    });
    await waitFor(() => expect(screen.getByTestId('agent-config-runtime-catalog-state')).toHaveAttribute('data-empty', 'false'));
    const chip = screen.getByTestId('agent-config-executor-chip');
    expect(chip).toHaveAttribute('data-selectable', 'false');
    expect(chip).toHaveTextContent(/unavailable/i);
    expect(screen.getByTestId('agent-config-executor-selection-error')).toBeInTheDocument();
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
    await waitRuntimeReady();
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-cancel'));
    expect(screen.queryByTestId('confirm-modal')).toBeNull();
    expect(screen.getByTestId('agent-config-edit-modal')).toBeInTheDocument();
    expect(patched).toBe(false);
  });
});

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

async function pickRuntimeModel(testId: string, query: string, value?: string) {
  const input = screen.getByTestId(testId) as HTMLInputElement;
  await waitFor(() => expect(input).not.toBeDisabled());
  fireEvent.focus(input);
  fireEvent.change(input, { target: { value: query } });
  const options = await screen.findAllByTestId(`${testId}-option`);
  const option = value ? options.find((o) => o.getAttribute('data-value') === value) : options[0];
  if (!option) throw new Error(`option not found: ${value ?? query}`);
  fireEvent.click(option);
}

async function waitRuntimeReady() {
  await waitFor(() => expect(screen.getByTestId('agent-config-edit-save')).not.toBeDisabled());
}

afterEach(() => cleanup());

describe('AgentConfigEditModal (T236)', () => {
  it('prefills the form from the agent config', async () => {
    wrap({ ...base, model: 'claude-sonnet-4-6', cli: 'codex', reasoning: 'high', mode: 'plan', provider: 'anthropic' });
    await waitFor(() => expect(screen.getByTestId('agent-config-model')).toHaveValue('Claude Sonnet 4.6'));
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
    await waitRuntimeReady();
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));

    await waitFor(() => expect(patchBody).toBeDefined());
    expect(patchBody).toMatchObject({ env_vars: { FOO: 'baz', EMPTY: '' } });
  });

  it('env vars: invalid lines block confirmation and show an error', async () => {
    wrap(base);
    fireEvent.change(screen.getByTestId('agent-config-env'), { target: { value: '1BAD=value' } });
    await waitRuntimeReady();
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
    await pickRuntimeModel('agent-config-model', 'sonnet', 'claude-sonnet-4-6');
    // Save → confirm dialog (running → restart warning)
    await waitRuntimeReady();
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
    expect(screen.queryAllByTestId('agent-config-executor-chip')).toHaveLength(0);
    const cli = screen.getByTestId('agent-config-executor-cli') as HTMLSelectElement;
    await waitFor(() => expect(cli).not.toBeDisabled());
    fireEvent.change(cli, { target: { value: 'codex' } });
    await pickRuntimeModel('agent-config-executor-model', 'gpt-5.5', 'gpt-5.5');
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
    await pickRuntimeModel('agent-config-executor-model', 'opus', 'claude-opus-4-8');
    fireEvent.click(screen.getByTestId('agent-config-executor-add'));
    await waitRuntimeReady();
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(patchBody).toBeDefined());
    expect(patchBody).toMatchObject({
      max_concurrent_tasks: 4,
      allowed_executors: [{ cli: 'claude-code', model: 'claude-opus-4-8' }],
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
    await waitRuntimeReady();
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
    await waitRuntimeReady();
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
    await waitRuntimeReady();
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(patchBody).toBeDefined());
    expect(patchBody).toMatchObject({ include_description_in_system_prompt: false });
  });

  it('T728: include-description toggle echoes the persisted false value', () => {
    wrap({ ...base, include_description_in_system_prompt: false });
    expect(screen.getByTestId('agent-config-include-description')).toHaveAttribute('aria-checked', 'false');
  });

  it('model: renders AI Runtime options with metadata and the Models shortcut', async () => {
    wrap(base);
    const input = screen.getByTestId('agent-config-model') as HTMLInputElement;
    await waitFor(() => expect(input).not.toBeDisabled());
    expect(input).toHaveAttribute('role', 'combobox');
    expect(input.getAttribute('list')).toBeNull();
    fireEvent.focus(input);
    expect(await screen.findByTestId('agent-config-model-options')).toHaveTextContent('Claude Opus 4.8');
    expect(screen.getByTestId('agent-config-model-options')).toHaveTextContent('200,000 ctx');
    expect(screen.getByTestId('agent-config-model-models-link')).toHaveTextContent('AI Runtime Models');
  });

  it('model: a free-typed non-option value only filters and does not PATCH through', async () => {
    let patchBody: Record<string, unknown> | undefined;
    server.use(
      http.patch('/api/agents/:id/config', async ({ request }) => {
        patchBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ ...base });
      }),
      http.post('/api/agents/:id/restart', () => HttpResponse.json({ ...base })),
    );
    wrap(base);
    const custom = 'my-org/custom-model-2099';
    const input = screen.getByTestId('agent-config-model') as HTMLInputElement;
    await waitFor(() => expect(input).not.toBeDisabled());
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: custom } });
    expect(screen.getByTestId('agent-config-model-empty')).toHaveTextContent('No matching runtime models');
    fireEvent.keyDown(input, { key: 'Escape' });
    expect(input).toHaveValue('Claude Opus 4.8');
    await waitRuntimeReady();
    fireEvent.click(screen.getByTestId('agent-config-edit-save'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(patchBody).toBeDefined());
    expect(patchBody).toMatchObject({ model: 'claude-opus-4-8' });
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

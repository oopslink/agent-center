import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { OrgContext } from '@/OrgContext';
import AiRuntime from './AiRuntime';

function renderPage(path = '/organizations/test/ai-runtime') {
  window.history.pushState({}, '', path);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OrgContext.Provider value={{ slug: 'test', orgId: 'org-test', orgName: 'Test Org' }}>
        <MemoryRouter initialEntries={[path]}>
          <AiRuntime />
        </MemoryRouter>
      </OrgContext.Provider>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('AiRuntime page', () => {
  it('renders the organization runtime catalog and admin controls for owners', async () => {
    renderPage();
    expect(await screen.findByTestId('page-AiRuntime')).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByTestId('ai-runtime-permission')).toHaveAttribute('data-can-manage', 'true'),
    );
    expect(screen.getByTestId('ai-runtime-export-yaml')).toHaveAttribute(
      'href',
      '/api/orgs/test/ai-runtime/export?format=yaml',
    );
    const segments = screen.getByTestId('segmented-nav');
    expect(within(segments).getByTestId('system-seg-ai-runtime')).toHaveAttribute('data-active', 'true');
    expect(within(segments).getByTestId('system-seg-environment')).toHaveAttribute('data-active', 'false');
    expect(await screen.findByText('GPT-5')).toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-tab-profiles')).not.toBeInTheDocument();
    expect(screen.queryByText('Default coding')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-create-profile')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-edit-profile')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-set-default')).not.toBeInTheDocument();
    expect(screen.getByTestId('ai-runtime-create-model')).toBeInTheDocument();
    expect(screen.getByTestId('ai-runtime-import-models')).toBeInTheDocument();
    expect(screen.getAllByTestId('ai-runtime-edit-model').length).toBeGreaterThan(0);
    expect(screen.getAllByTestId('ai-runtime-disable-model').length).toBeGreaterThan(0);
    fireEvent.click(screen.getByTestId('ai-runtime-tab-clis'));
    expect(await screen.findByText('Codex CLI')).toBeInTheDocument();
    expect(screen.getByTestId('ai-runtime-create-cli')).toBeInTheDocument();
    expect(screen.getAllByTestId('ai-runtime-edit-cli').length).toBeGreaterThan(0);
    expect(screen.getAllByTestId('ai-runtime-disable-cli').length).toBeGreaterThan(0);
  });

  it('keeps organization members read-only while leaving the page visible', async () => {
    server.use(
      http.get('/api/orgs', () =>
        HttpResponse.json([
          { id: 'org-test', slug: 'test', name: 'Test Org', role: 'member', created_at: '2026-01-01T00:00:00Z' },
        ]),
      ),
    );
    renderPage();
    expect(await screen.findByTestId('page-AiRuntime')).toBeInTheDocument();
    expect(screen.getByTestId('ai-runtime-permission')).toHaveAttribute('data-can-manage', 'false');
    expect(screen.getByTestId('ai-runtime-export-yaml')).toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-create-profile')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-edit-profile')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-set-default')).not.toBeInTheDocument();
    const table = await screen.findByTestId('ai-runtime-catalog');
    expect(within(table).getByText('GPT-5')).toBeInTheDocument();
    expect(within(table).queryByText('Default coding')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-create-model')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-import-models')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-edit-model')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-disable-model')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('ai-runtime-tab-clis'));
    expect(screen.queryByTestId('ai-runtime-create-cli')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-edit-cli')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-disable-cli')).not.toBeInTheDocument();
  });

  it('ignores the retired profiles tab URL and keeps the Models tab active', async () => {
    renderPage('/organizations/test/ai-runtime?tab=profiles');
    expect(await screen.findByText('GPT-5')).toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-tab-profiles')).not.toBeInTheDocument();
    expect(screen.getByTestId('ai-runtime-tab-models')).toHaveAttribute('aria-selected', 'true');
  });

  it('creates a runtime model through the org-scoped POST endpoint with expected_revision', async () => {
    let payload: unknown = null;
    server.use(
      http.post('/api/orgs/:slug/ai-runtime/models', async ({ params, request }) => {
        payload = { slug: params.slug, body: await request.json() };
        return HttpResponse.json({ revision: 4, entry: { id: 'runtime-model-gpt-5-mini' } }, { status: 201 });
      }),
    );
    renderPage();
    expect(await screen.findByTestId('page-AiRuntime')).toBeInTheDocument();
    fireEvent.click(await screen.findByTestId('ai-runtime-create-model'));
    fireEvent.change(screen.getByTestId('ai-runtime-model-key'), { target: { value: 'gpt-5-mini' } });
    fireEvent.change(screen.getByTestId('ai-runtime-model-model-key'), { target: { value: 'gpt-5-mini' } });
    fireEvent.change(screen.getByTestId('ai-runtime-model-display-name'), { target: { value: 'GPT-5 mini' } });
    const enabledSwitch = screen.getByTestId('ai-runtime-model-enabled');
    expect(enabledSwitch).toHaveAttribute('role', 'switch');
    expect(enabledSwitch).toHaveAttribute('aria-checked', 'true');
    fireEvent.click(enabledSwitch);
    expect(enabledSwitch).toHaveAttribute('aria-checked', 'false');
    fireEvent.click(screen.getByTestId('ai-runtime-form-save'));
    await waitFor(() =>
      expect(payload).toEqual({
        slug: 'test',
        body: {
          expected_revision: 3,
          value: {
            key: 'gpt-5-mini',
            model_key: 'gpt-5-mini',
            display_name: 'GPT-5 mini',
            compatible_cli_keys: ['claude-code'],
            default_parameters: {},
            enabled: false,
            tier: '',
          },
        },
      }),
    );
  });

  it('edits a runtime model through the org-scoped PATCH endpoint with expected_revision', async () => {
    let payload: unknown = null;
    server.use(
      http.patch('/api/orgs/:slug/ai-runtime/models/:id', async ({ params, request }) => {
        payload = { slug: params.slug, id: params.id, body: await request.json() };
        return HttpResponse.json({ revision: 4, entry: { id: params.id } });
      }),
    );
    renderPage('/organizations/test/ai-runtime?tab=models');
    expect(await screen.findByText('GPT-5')).toBeInTheDocument();
    fireEvent.click(screen.getAllByTestId('ai-runtime-edit-model')[0]);
    fireEvent.change(screen.getByTestId('ai-runtime-model-display-name'), { target: { value: 'GPT-5 Updated' } });
    fireEvent.click(screen.getByTestId('ai-runtime-form-save'));
    await waitFor(() =>
      expect(payload).toEqual({
        slug: 'test',
        id: 'runtime-model-gpt-5',
        body: {
          expected_revision: 3,
          value: {
            key: 'gpt-5',
            model_key: 'gpt-5',
            display_name: 'GPT-5 Updated',
            compatible_cli_keys: ['codex'],
            default_parameters: {},
            enabled: true,
            context_window: 400000,
            input_cost_per_mtok: 1.25,
            output_cost_per_mtok: 10,
            tier: 'frontier',
          },
        },
      }),
    );
  });

  it('disables a runtime model through the revisioned PATCH endpoint', async () => {
    let payload: unknown = null;
    server.use(
      http.patch('/api/orgs/:slug/ai-runtime/models/:id', async ({ params, request }) => {
        payload = { slug: params.slug, id: params.id, body: await request.json() };
        return HttpResponse.json({ revision: 4, entry: { id: params.id } });
      }),
    );
    renderPage('/organizations/test/ai-runtime?tab=models');
    expect(await screen.findByText('GPT-5')).toBeInTheDocument();
    fireEvent.click(screen.getAllByTestId('ai-runtime-disable-model')[0]);
    expect(await screen.findByTestId('confirm-modal-message')).toHaveTextContent('GPT-5');
    fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    await waitFor(() =>
      expect(payload).toEqual({
        slug: 'test',
        id: 'runtime-model-gpt-5',
        body: {
          expected_revision: 3,
          value: {
            key: 'gpt-5',
            model_key: 'gpt-5',
            display_name: 'GPT-5',
            compatible_cli_keys: ['codex'],
            default_parameters: {},
            enabled: false,
            context_window: 400000,
            input_cost_per_mtok: 1.25,
            output_cost_per_mtok: 10,
            tier: 'frontier',
          },
        },
      }),
    );
  });

  it('disables a runtime CLI through the revisioned PATCH endpoint', async () => {
    let payload: unknown = null;
    server.use(
      http.patch('/api/orgs/:slug/ai-runtime/clis/:id', async ({ params, request }) => {
        payload = { slug: params.slug, id: params.id, body: await request.json() };
        return HttpResponse.json({ revision: 4, entry: { id: params.id } });
      }),
    );
    renderPage('/organizations/test/ai-runtime?tab=clis');
    expect(await screen.findByText('Codex CLI')).toBeInTheDocument();
    fireEvent.click(screen.getAllByTestId('ai-runtime-disable-cli')[0]);
    expect(await screen.findByTestId('confirm-modal-message')).toHaveTextContent('Claude Code');
    fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    await waitFor(() =>
      expect(payload).toEqual({
        slug: 'test',
        id: 'runtime-cli-claude-code',
        body: {
          expected_revision: 3,
          value: {
            key: 'claude-code',
            display_name: 'Claude Code',
            executable: 'claude',
            version_constraint: '>=1.0.0',
            required_features: ['workspace'],
            parameter_schema: {},
            enabled: false,
          },
        },
      }),
    );
  });

  it('surfaces revision conflicts from create/edit mutations', async () => {
    server.use(
      http.patch('/api/orgs/:slug/ai-runtime/models/:id', () =>
        HttpResponse.json(
          { error: 'runtime_catalog_revision_conflict', message: 'catalog revision changed' },
          { status: 409 },
        ),
      ),
    );
    renderPage('/organizations/test/ai-runtime?tab=models');
    expect((await screen.findAllByTestId('ai-runtime-model-row')).length).toBeGreaterThan(0);
    fireEvent.click(screen.getAllByTestId('ai-runtime-edit-model')[0]);
    fireEvent.change(screen.getByTestId('ai-runtime-model-display-name'), { target: { value: 'Stale edit' } });
    fireEvent.click(screen.getByTestId('ai-runtime-form-save'));
    expect(await screen.findByTestId('ai-runtime-form-error')).toHaveTextContent('runtime_catalog_revision_conflict');
  });

  it('shows system CLI edit constraints in the CLI editor', async () => {
    renderPage('/organizations/test/ai-runtime?tab=clis');
    expect(await screen.findByText('Codex CLI')).toBeInTheDocument();
    fireEvent.click(screen.getAllByTestId('ai-runtime-edit-cli')[0]);
    expect(screen.getByTestId('ai-runtime-system-hint')).toBeInTheDocument();
    expect(screen.getByTestId('ai-runtime-immutable-key-hint')).toBeInTheDocument();
    expect(screen.getByTestId('ai-runtime-cli-key')).toBeDisabled();
  });

  it('previews and applies a models-only bulk import while preserving CLIs', async () => {
    let previewPayload: unknown = null;
    let applyPayload: unknown = null;
    server.use(
      http.post('/api/orgs/:slug/ai-runtime/import/preview', async ({ params, request }) => {
        const body = await request.json() as {
          strategy?: string;
          document?: { runtime?: { clis?: unknown[]; models?: Array<{ key?: string }> } };
        };
        previewPayload = { slug: params.slug, body };
        return HttpResponse.json({
          report: {
            dry_run: true,
            applied: false,
            revision: 3,
            items: [
              { entity_type: 'cli', key: 'codex', action: 'unchanged' },
              { entity_type: 'model', key: 'gpt-5-mini', action: 'create' },
            ],
            diagnostics: [
              { code: 'model_tier_missing', severity: 'warning', path: 'runtime.models[1].tier', message: 'tier is optional' },
            ],
          },
          validation_token: 'validation-token-1',
          expires_at: '2026-08-08T00:00:00Z',
          document_sha256: 'abcdef0123456789',
        });
      }),
      http.post('/api/orgs/:slug/ai-runtime/import/apply', async ({ params, request }) => {
        const body = await request.json() as { validation_token?: string; document?: unknown; strategy?: string };
        applyPayload = { slug: params.slug, body };
        return HttpResponse.json({
          dry_run: false,
          applied: true,
          revision: 4,
          items: [{ entity_type: 'model', key: 'gpt-5-mini', action: 'create' }],
          diagnostics: [],
        });
      }),
    );
    renderPage('/organizations/test/ai-runtime?tab=models');
    expect((await screen.findAllByTestId('ai-runtime-model-row')).length).toBeGreaterThan(0);
    fireEvent.click(await screen.findByTestId('ai-runtime-import-models'));
    fireEvent.change(screen.getByTestId('ai-runtime-model-import-json'), {
      target: {
        value: JSON.stringify([
          {
            key: 'gpt-5-mini',
            model_key: 'gpt-5-mini',
            display_name: 'GPT-5 mini',
            compatible_cli_keys: ['codex'],
            enabled: true,
          },
        ]),
      },
    });
    fireEvent.click(screen.getByTestId('ai-runtime-model-import-preview-btn'));
    expect(await screen.findByTestId('ai-runtime-model-import-change')).toHaveTextContent('gpt-5-mini');
    expect(screen.getByTestId('ai-runtime-model-import-preview')).toHaveTextContent('1 CLIs');
    expect(screen.getByTestId('ai-runtime-model-import-preview')).not.toHaveTextContent('Profiles');
    expect(screen.getByTestId('ai-runtime-model-import-diagnostics')).toHaveTextContent('tier is optional');
    await waitFor(() => {
      const payload = previewPayload as {
        slug?: string;
        body?: { strategy?: string; document?: { kind?: string; runtime?: Record<string, unknown> } };
      };
      expect(payload.slug).toBe('test');
      expect(payload.body?.strategy).toBe('merge');
      expect(payload.body?.document?.kind).toBe('agent-center-ai-runtime');
      expect(payload.body?.document?.runtime?.clis).toEqual(expect.arrayContaining([expect.objectContaining({ key: 'codex' })]));
      expect(payload.body?.document?.runtime?.models).toEqual(expect.arrayContaining([
        expect.objectContaining({ key: 'gpt-5' }),
        expect.objectContaining({ key: 'gpt-5-mini' }),
      ]));
      expect(payload.body?.document?.runtime?.profiles).toBeUndefined();
      expect(payload.body?.document?.runtime?.default_profile_key).toBeUndefined();
    });
    fireEvent.click(screen.getByTestId('ai-runtime-model-import-apply'));
    expect(await screen.findByTestId('ai-runtime-model-import-applied')).toHaveTextContent('4');
    await waitFor(() => {
      const payload = applyPayload as {
        slug?: string;
        body?: { strategy?: string; validation_token?: string; document?: { runtime?: Record<string, unknown> } };
      };
      expect(payload.slug).toBe('test');
      expect(payload.body?.strategy).toBe('merge');
      expect(payload.body?.validation_token).toBe('validation-token-1');
      expect(payload.body?.document?.runtime?.clis).toEqual(expect.arrayContaining([expect.objectContaining({ key: 'codex' })]));
      expect(payload.body?.document?.runtime?.models).toEqual(expect.arrayContaining([expect.objectContaining({ key: 'gpt-5-mini' })]));
      expect(payload.body?.document?.runtime?.profiles).toBeUndefined();
      expect(payload.body?.document?.runtime?.default_profile_key).toBeUndefined();
    });
  });
});

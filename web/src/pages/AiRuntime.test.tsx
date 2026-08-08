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
    expect(await screen.findByTestId('ai-runtime-default-profile')).toHaveTextContent('Default coding');
    expect(screen.getAllByTestId('ai-runtime-profile-row')).toHaveLength(2);
    expect(screen.getAllByTestId('ai-runtime-set-default')).toHaveLength(2);
    expect(screen.getByTestId('ai-runtime-create-profile')).toBeInTheDocument();
    expect(screen.getAllByTestId('ai-runtime-edit-profile')).toHaveLength(2);

    fireEvent.click(screen.getByTestId('ai-runtime-tab-models'));
    expect(await screen.findByTestId('ai-runtime-model-row')).toHaveTextContent('GPT-5');
    expect(screen.getByTestId('ai-runtime-create-model')).toBeInTheDocument();
    expect(screen.getByTestId('ai-runtime-import-models')).toBeInTheDocument();
    expect(screen.getByTestId('ai-runtime-edit-model')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('ai-runtime-tab-clis'));
    expect(await screen.findByTestId('ai-runtime-cli-row')).toHaveTextContent('Codex CLI');
    expect(screen.getByTestId('ai-runtime-create-cli')).toBeInTheDocument();
    expect(screen.getByTestId('ai-runtime-edit-cli')).toBeInTheDocument();
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
    expect(within(table).getByText('Default coding')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('ai-runtime-tab-models'));
    expect(screen.queryByTestId('ai-runtime-create-model')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-import-models')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-edit-model')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('ai-runtime-tab-clis'));
    expect(screen.queryByTestId('ai-runtime-create-cli')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-edit-cli')).not.toBeInTheDocument();
  });

  it('sets a new default profile through the org-scoped endpoint', async () => {
    let called = false;
    server.use(
      http.put('/api/orgs/:slug/ai-runtime/default-profile', async ({ params, request }) => {
        const body = (await request.json()) as { expected_revision?: number; profile_id?: string };
        called = params.slug === 'test' && body.expected_revision === 3 && body.profile_id === 'runtime-profile-review';
        return HttpResponse.json({ revision: 4, default_runtime_profile_id: body.profile_id });
      }),
    );
    renderPage();
    const buttons = await screen.findAllByTestId('ai-runtime-set-default');
    fireEvent.click(buttons[1]);
    await waitFor(() => expect(called).toBe(true));
  });

  it('creates a runtime profile through the org-scoped POST endpoint with expected_revision', async () => {
    let payload: unknown = null;
    server.use(
      http.post('/api/orgs/:slug/ai-runtime/profiles', async ({ params, request }) => {
        payload = { slug: params.slug, body: await request.json() };
        return HttpResponse.json({ revision: 4, entry: { id: 'runtime-profile-planning' } }, { status: 201 });
      }),
    );
    renderPage();
    expect(await screen.findByTestId('page-AiRuntime')).toBeInTheDocument();
    fireEvent.click(await screen.findByTestId('ai-runtime-create-profile'));
    fireEvent.change(screen.getByTestId('ai-runtime-profile-key'), { target: { value: 'planning' } });
    fireEvent.change(screen.getByTestId('ai-runtime-profile-name'), { target: { value: 'Planning' } });
    const enabledSwitch = screen.getByTestId('ai-runtime-profile-enabled');
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
            key: 'planning',
            name: 'Planning',
            description: '',
            cli_key: 'codex',
            model_key: 'gpt-5',
            parameters: {},
            enabled: false,
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
    expect(await screen.findByTestId('ai-runtime-model-row')).toHaveTextContent('GPT-5');
    fireEvent.click(await screen.findByTestId('ai-runtime-edit-model'));
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
    expect(await screen.findByTestId('ai-runtime-model-row')).toBeInTheDocument();
    fireEvent.click(await screen.findByTestId('ai-runtime-edit-model'));
    fireEvent.change(screen.getByTestId('ai-runtime-model-display-name'), { target: { value: 'Stale edit' } });
    fireEvent.click(screen.getByTestId('ai-runtime-form-save'));
    expect(await screen.findByTestId('ai-runtime-form-error')).toHaveTextContent('runtime_catalog_revision_conflict');
  });

  it('shows system CLI edit constraints in the CLI editor', async () => {
    renderPage('/organizations/test/ai-runtime?tab=clis');
    expect(await screen.findByTestId('ai-runtime-cli-row')).toHaveTextContent('Codex CLI');
    fireEvent.click(await screen.findByTestId('ai-runtime-edit-cli'));
    expect(screen.getByTestId('ai-runtime-system-hint')).toBeInTheDocument();
    expect(screen.getByTestId('ai-runtime-immutable-key-hint')).toBeInTheDocument();
    expect(screen.getByTestId('ai-runtime-cli-key')).toBeDisabled();
  });

  it('previews and applies a models-only bulk import while preserving Profiles and CLIs', async () => {
    let previewPayload: unknown = null;
    let applyPayload: unknown = null;
    server.use(
      http.post('/api/orgs/:slug/ai-runtime/import/preview', async ({ params, request }) => {
        const body = await request.json() as {
          strategy?: string;
          document?: { runtime?: { clis?: unknown[]; models?: Array<{ key?: string }>; profiles?: unknown[] } };
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
              { entity_type: 'profile', key: 'default-coding', action: 'unchanged' },
              { entity_type: 'profile', key: 'review', action: 'unchanged' },
            ],
            diagnostics: [
              { code: 'model_tier_missing', severity: 'warning', path: 'runtime.models[1].tier', message: 'tier is optional' },
            ],
          },
          coverage: [],
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
    expect(await screen.findByTestId('ai-runtime-model-row')).toBeInTheDocument();
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
    expect(screen.getByTestId('ai-runtime-model-import-preview')).toHaveTextContent('2 Profiles');
    expect(screen.getByTestId('ai-runtime-model-import-diagnostics')).toHaveTextContent('tier is optional');
    await waitFor(() => {
      expect(previewPayload).toEqual(
        expect.objectContaining({
          slug: 'test',
          body: expect.objectContaining({
            strategy: 'merge',
            document: expect.objectContaining({
              kind: 'agent-center-ai-runtime',
              runtime: expect.objectContaining({
                clis: expect.arrayContaining([expect.objectContaining({ key: 'codex' })]),
                profiles: expect.arrayContaining([
                  expect.objectContaining({ key: 'default-coding' }),
                  expect.objectContaining({ key: 'review' }),
                ]),
                models: expect.arrayContaining([
                  expect.objectContaining({ key: 'gpt-5' }),
                  expect.objectContaining({ key: 'gpt-5-mini' }),
                ]),
              }),
            }),
          }),
        }),
      );
    });
    fireEvent.click(screen.getByTestId('ai-runtime-model-import-apply'));
    expect(await screen.findByTestId('ai-runtime-model-import-applied')).toHaveTextContent('4');
    await waitFor(() =>
      expect(applyPayload).toEqual(
        expect.objectContaining({
          slug: 'test',
          body: expect.objectContaining({
            strategy: 'merge',
            validation_token: 'validation-token-1',
            document: expect.objectContaining({
              runtime: expect.objectContaining({
                clis: expect.arrayContaining([expect.objectContaining({ key: 'codex' })]),
                profiles: expect.arrayContaining([expect.objectContaining({ key: 'default-coding' })]),
                models: expect.arrayContaining([expect.objectContaining({ key: 'gpt-5-mini' })]),
              }),
            }),
          }),
        }),
      ),
    );
  });
});

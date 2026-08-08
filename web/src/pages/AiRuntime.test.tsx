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
    expect(await screen.findByTestId('ai-runtime-default-profile')).toHaveTextContent('Default coding');
    expect(screen.getAllByTestId('ai-runtime-profile-row')).toHaveLength(2);
    expect(screen.getAllByTestId('ai-runtime-set-default')).toHaveLength(2);
    expect(screen.getByTestId('ai-runtime-add-profile')).toBeInTheDocument();
    expect(screen.getByTestId('system-seg-ai-runtime')).toHaveAttribute('data-active', 'true');

    fireEvent.click(screen.getByTestId('ai-runtime-tab-models'));
    expect(await screen.findByTestId('ai-runtime-model-row')).toHaveTextContent('GPT-5');
    expect(screen.getByTestId('ai-runtime-import-models')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('ai-runtime-tab-clis'));
    expect(await screen.findByTestId('ai-runtime-cli-row')).toHaveTextContent('Codex CLI');
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
    expect(screen.queryByTestId('ai-runtime-set-default')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-add-profile')).not.toBeInTheDocument();
    const table = await screen.findByTestId('ai-runtime-catalog');
    expect(within(table).getByText('Default coding')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('ai-runtime-tab-models'));
    expect(screen.queryByTestId('ai-runtime-import-models')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-edit-model')).not.toBeInTheDocument();
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

  it('creates a profile with expected_revision optimistic locking', async () => {
    let body: unknown = null;
    server.use(
      http.post('/api/orgs/:slug/ai-runtime/profiles', async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({ revision: 4, entry: { id: 'new-profile' } }, { status: 201 });
      }),
    );
    renderPage();
    await screen.findByTestId('ai-runtime-add-profile');
    fireEvent.click(screen.getByTestId('ai-runtime-add-profile'));
    fireEvent.change(screen.getByTestId('ai-runtime-field-key'), { target: { value: 'custom-review' } });
    fireEvent.change(screen.getByTestId('ai-runtime-field-name'), { target: { value: 'Custom review' } });
    fireEvent.click(screen.getByTestId('ai-runtime-editor-save'));
    await waitFor(() =>
      expect(body).toMatchObject({
        expected_revision: 3,
        value: {
          key: 'custom-review',
          name: 'Custom review',
          cli_key: 'codex',
          model_key: 'gpt-5',
          parameters: {},
          enabled: true,
        },
      }),
    );
  });

  it('edits the system CLI while showing the immutable built-in hint', async () => {
    let body: unknown = null;
    server.use(
      http.patch('/api/orgs/:slug/ai-runtime/clis/:id', async ({ params, request }) => {
        body = { id: params.id, ...((await request.json()) as Record<string, unknown>) };
        return HttpResponse.json({ revision: 4, entry: { id: params.id } });
      }),
    );
    renderPage();
    fireEvent.click(await screen.findByTestId('ai-runtime-tab-clis'));
    fireEvent.click(await screen.findByTestId('ai-runtime-edit-cli'));
    expect(screen.getByTestId('ai-runtime-editor-immutable-hint')).toHaveTextContent('System CLI');
    expect(screen.getByTestId('ai-runtime-field-key')).toBeDisabled();
    fireEvent.change(screen.getByTestId('ai-runtime-field-display-name'), { target: { value: 'Codex Runtime' } });
    fireEvent.click(screen.getByTestId('ai-runtime-editor-save'));
    await waitFor(() =>
      expect(body).toMatchObject({
        id: 'runtime-cli-codex',
        expected_revision: 3,
        value: {
          key: 'codex',
          display_name: 'Codex Runtime',
          executable: 'codex',
          enabled: true,
        },
      }),
    );
  });

  it('surfaces revision conflicts from create/edit mutations', async () => {
    server.use(
      http.post('/api/orgs/:slug/ai-runtime/profiles', () =>
        HttpResponse.json(
          { error: 'runtime_catalog_revision_conflict', message: 'catalog revision changed' },
          { status: 409 },
        ),
      ),
    );
    renderPage();
    fireEvent.click(await screen.findByTestId('ai-runtime-add-profile'));
    fireEvent.change(screen.getByTestId('ai-runtime-field-key'), { target: { value: 'conflict' } });
    fireEvent.change(screen.getByTestId('ai-runtime-field-name'), { target: { value: 'Conflict' } });
    fireEvent.click(screen.getByTestId('ai-runtime-editor-save'));
    expect(await screen.findByTestId('ai-runtime-editor-error')).toHaveTextContent('catalog revision changed');
  });

  it('previews and applies a models-only bulk import via AI Runtime import endpoints', async () => {
    let previewBody: unknown = null;
    let applyBody: unknown = null;
    server.use(
      http.post('/api/orgs/:slug/ai-runtime/import/preview', async ({ request }) => {
        previewBody = await request.json();
        return HttpResponse.json({
          report: {
            dry_run: true,
            applied: false,
            revision: 3,
            diagnostics: [],
            items: [
              { entity_type: 'cli', key: 'codex', action: 'unchanged' },
              { entity_type: 'profile', key: 'default-coding', action: 'unchanged' },
              { entity_type: 'model', key: 'gpt-5.1', action: 'create' },
            ],
          },
          validation_token: 'preview-token',
          expires_at: '2026-08-08T00:10:00Z',
          document_sha256: 'sha',
        });
      }),
      http.post('/api/orgs/:slug/ai-runtime/import/apply', async ({ request }) => {
        applyBody = await request.json();
        return HttpResponse.json({ dry_run: true, applied: true, revision: 4, items: [], diagnostics: [] });
      }),
    );
    renderPage();
    fireEvent.click(await screen.findByTestId('ai-runtime-tab-models'));
    fireEvent.click(screen.getByTestId('ai-runtime-import-models'));
    fireEvent.change(screen.getByTestId('ai-runtime-import-json'), {
      target: {
        value: JSON.stringify([
          { key: 'gpt-5.1', model_key: 'gpt-5.1', display_name: 'GPT-5.1', compatible_cli_keys: ['codex'], enabled: true },
        ]),
      },
    });
    fireEvent.click(screen.getByTestId('ai-runtime-import-preview-run'));
    expect(await screen.findByTestId('ai-runtime-import-preview')).toHaveTextContent('gpt-5.1');
    expect(previewBody).toMatchObject({
      strategy: 'merge',
      document: {
        runtime: {
          clis: [{ key: 'codex' }],
          profiles: [{ key: 'default-coding' }, { key: 'review' }],
        },
      },
    });
    expect((previewBody as { document: { runtime: { models: Array<{ key: string }> } } }).document.runtime.models.map((m) => m.key)).toEqual([
      'gpt-5',
      'gpt-5.1',
    ]);
    fireEvent.click(screen.getByTestId('ai-runtime-import-apply'));
    await waitFor(() =>
      expect(applyBody).toMatchObject({
        strategy: 'merge',
        validation_token: 'preview-token',
      }),
    );
  });
});

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
    expect(screen.getByTestId('ai-runtime-import-models')).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('ai-runtime-tab-models'));
    expect(await screen.findByTestId('ai-runtime-model-row')).toHaveTextContent('GPT-5');
    expect(screen.getByTestId('ai-runtime-add-model')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('ai-runtime-tab-clis'));
    expect(await screen.findByTestId('ai-runtime-cli-row')).toHaveTextContent('Codex CLI');
    expect(screen.getByTestId('ai-runtime-add-cli')).toBeInTheDocument();
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
    expect(screen.queryByTestId('ai-runtime-import-models')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-add-profile')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-set-default')).not.toBeInTheDocument();
    const table = await screen.findByTestId('ai-runtime-catalog');
    expect(within(table).getByText('Default coding')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('ai-runtime-tab-models'));
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

  it('creates a profile through the revision-guarded AI Runtime endpoint', async () => {
    let posted: unknown = null;
    server.use(
      http.post('/api/orgs/:slug/ai-runtime/profiles', async ({ params, request }) => {
        posted = { slug: params.slug, body: await request.json() };
        return HttpResponse.json({ revision: 4, entry: { id: 'runtime-profile-new' } }, { status: 201 });
      }),
    );
    renderPage();
    await screen.findByTestId('ai-runtime-add-profile');
    fireEvent.click(screen.getByTestId('ai-runtime-add-profile'));
    fireEvent.change(screen.getByTestId('ai-runtime-profile-key'), { target: { value: 'ops' } });
    fireEvent.change(screen.getByTestId('ai-runtime-profile-name'), { target: { value: 'Ops' } });
    fireEvent.click(screen.getByTestId('ai-runtime-form-save'));
    await waitFor(() =>
      expect(posted).toEqual({
        slug: 'test',
        body: {
          expected_revision: 3,
          value: {
            key: 'ops',
            name: 'Ops',
            description: '',
            cli_key: 'codex',
            model_key: 'gpt-5',
            parameters: {},
            enabled: true,
          },
        },
      }),
    );
  });

  it('edits a model and surfaces revision conflicts from PATCH', async () => {
    let patched: unknown = null;
    server.use(
      http.patch('/api/orgs/:slug/ai-runtime/models/:id', async ({ params, request }) => {
        patched = { id: params.id, body: await request.json() };
        return HttpResponse.json(
          { error: 'runtime_catalog_revision_conflict', message: 'catalog revision changed' },
          { status: 409 },
        );
      }),
    );
    renderPage();
    fireEvent.click(await screen.findByTestId('ai-runtime-tab-models'));
    fireEvent.click(await screen.findByTestId('ai-runtime-edit-model'));
    expect(screen.getByTestId('ai-runtime-model-key')).toBeDisabled();
    fireEvent.change(screen.getByTestId('ai-runtime-model-display-name'), { target: { value: 'GPT-5 updated' } });
    fireEvent.click(screen.getByTestId('ai-runtime-form-save'));
    await waitFor(() => expect(screen.getByTestId('ai-runtime-form-error')).toHaveTextContent('catalog revision changed'));
    expect(patched).toEqual({
      id: 'runtime-model-gpt-5',
      body: expect.objectContaining({
        expected_revision: 3,
        value: expect.objectContaining({ key: 'gpt-5', display_name: 'GPT-5 updated' }),
      }),
    });
  });

  it('creates and edits CLIs while marking system built-ins clearly', async () => {
    const calls: unknown[] = [];
    server.use(
      http.post('/api/orgs/:slug/ai-runtime/clis', async ({ request }) => {
        calls.push({ method: 'POST', body: await request.json() });
        return HttpResponse.json({ revision: 4, entry: { id: 'runtime-cli-custom' } }, { status: 201 });
      }),
      http.patch('/api/orgs/:slug/ai-runtime/clis/:id', async ({ params, request }) => {
        calls.push({ method: 'PATCH', id: params.id, body: await request.json() });
        return HttpResponse.json({ revision: 4, entry: { id: params.id } });
      }),
    );
    renderPage();
    fireEvent.click(await screen.findByTestId('ai-runtime-tab-clis'));
    fireEvent.click(await screen.findByTestId('ai-runtime-edit-cli'));
    expect(screen.getByTestId('ai-runtime-system-note')).toBeInTheDocument();
    expect(screen.getByTestId('ai-runtime-cli-key')).toBeDisabled();
    fireEvent.change(screen.getByTestId('ai-runtime-cli-display-name'), { target: { value: 'Codex CLI updated' } });
    fireEvent.click(screen.getByTestId('ai-runtime-form-save'));
    await waitFor(() => expect(calls.some((c) => (c as { method: string }).method === 'PATCH')).toBe(true));

    fireEvent.click(screen.getByTestId('ai-runtime-add-cli'));
    fireEvent.change(screen.getByTestId('ai-runtime-cli-key'), { target: { value: 'custom-cli' } });
    fireEvent.change(screen.getByTestId('ai-runtime-cli-display-name'), { target: { value: 'Custom CLI' } });
    fireEvent.change(screen.getByTestId('ai-runtime-cli-executable'), { target: { value: 'custom' } });
    fireEvent.click(screen.getByTestId('ai-runtime-form-save'));
    await waitFor(() => expect(calls.some((c) => (c as { method: string }).method === 'POST')).toBe(true));
    expect(calls).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          method: 'PATCH',
          id: 'runtime-cli-codex',
          body: expect.objectContaining({
            expected_revision: 3,
            value: expect.objectContaining({ key: 'codex', display_name: 'Codex CLI updated' }),
          }),
        }),
        expect.objectContaining({
          method: 'POST',
          body: expect.objectContaining({
            expected_revision: 3,
            value: expect.objectContaining({ key: 'custom-cli', executable: 'custom' }),
          }),
        }),
      ]),
    );
  });

  it('previews and applies a model-only batch import with current profiles and CLIs preserved', async () => {
    let previewBody: any = null;
    let applyBody: any = null;
    server.use(
      http.post('/api/orgs/:slug/ai-runtime/import/preview', async ({ request }) => {
        previewBody = await request.json();
        return HttpResponse.json({
          report: {
            dry_run: true,
            applied: false,
            revision: 3,
            items: [{ entity_type: 'model', key: 'gpt-5-mini', action: 'create' }],
            diagnostics: [],
          },
          validation_token: 'preview-token',
          expires_at: '2026-07-01T00:10:00Z',
          document_sha256: 'abcdef0123456789',
        });
      }),
      http.post('/api/orgs/:slug/ai-runtime/import/apply', async ({ request }) => {
        applyBody = await request.json();
        return HttpResponse.json({
          dry_run: false,
          applied: true,
          revision: 4,
          items: [{ entity_type: 'model', key: 'gpt-5-mini', action: 'create' }],
          diagnostics: [],
        });
      }),
    );
    renderPage();
    await screen.findByTestId('ai-runtime-import-models');
    fireEvent.click(screen.getByTestId('ai-runtime-import-models'));
    expect(screen.getByTestId('ai-runtime-import-note')).toHaveTextContent('complete document');
    fireEvent.change(screen.getByTestId('ai-runtime-import-json'), {
      target: {
        value: JSON.stringify([
          { model_id: 'gpt-5-mini', display_name: 'GPT-5 mini', context_window: 128000, input_cost: 0.15, output_cost: 0.6 },
        ]),
      },
    });
    fireEvent.click(screen.getByTestId('ai-runtime-import-preview'));
    await waitFor(() => expect(screen.getByTestId('ai-runtime-import-item')).toHaveTextContent('gpt-5-mini'));
    expect(previewBody.strategy).toBe('merge');
    expect(previewBody.document.runtime.clis).toHaveLength(1);
    expect(previewBody.document.runtime.profiles).toHaveLength(2);
    expect(previewBody.document.runtime.models.map((m: { key: string }) => m.key)).toEqual(['gpt-5', 'gpt-5-mini']);
    fireEvent.click(screen.getByTestId('ai-runtime-import-apply'));
    await waitFor(() => expect(screen.getByTestId('ai-runtime-import-success')).toBeInTheDocument());
    expect(applyBody).toEqual(expect.objectContaining({ strategy: 'merge', validation_token: 'preview-token' }));
  });
});

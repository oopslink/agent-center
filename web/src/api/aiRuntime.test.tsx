import type React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { act, cleanup, renderHook } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import {
  useApplyAIRuntimeImport,
  useCreateRuntimeModel,
  usePreviewAIRuntimeImport,
  type RuntimeExportDocument,
} from './aiRuntime';

function wrapper({ children }: { children: React.ReactNode }): React.ReactElement {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

afterEach(() => cleanup());

describe('aiRuntime API hooks', () => {
  it('posts model creates to the org-scoped AI Runtime endpoint with expected_revision', async () => {
    window.history.pushState({}, '', '/organizations/test/ai-runtime');
    let posted: unknown = null;
    server.use(
      http.post('/api/orgs/:slug/ai-runtime/models', async ({ params, request }) => {
        posted = { slug: params.slug, body: await request.json() };
        return HttpResponse.json({ revision: 4, entry: { id: 'runtime-model-new' } }, { status: 201 });
      }),
    );
    const { result } = renderHook(() => useCreateRuntimeModel(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({
        expectedRevision: 3,
        value: {
          key: 'gpt-5-mini',
          model_key: 'gpt-5-mini',
          display_name: 'GPT-5 mini',
          compatible_cli_keys: ['codex'],
          default_parameters: {},
          enabled: true,
        },
      });
    });
    expect(posted).toEqual({
      slug: 'test',
      body: expect.objectContaining({
        expected_revision: 3,
        value: expect.objectContaining({ key: 'gpt-5-mini' }),
      }),
    });
  });

  it('uses two-stage preview/apply import with the validation token', async () => {
    window.history.pushState({}, '', '/organizations/test/ai-runtime');
    const document: RuntimeExportDocument = {
      schema_version: 1,
      kind: 'agent-center-ai-runtime',
      exported_at: '2026-07-01T00:00:00Z',
      runtime: { clis: [], models: [], profiles: [] },
    };
    let applyBody: unknown = null;
    server.use(
      http.post('/api/orgs/:slug/ai-runtime/import/preview', async ({ request }) => {
        const body = await request.json();
        return HttpResponse.json({
          report: { dry_run: true, applied: false, revision: 3, items: [], diagnostics: [] },
          validation_token: body ? 'token-1' : '',
          expires_at: '2026-07-01T00:10:00Z',
          document_sha256: 'abc',
        });
      }),
      http.post('/api/orgs/:slug/ai-runtime/import/apply', async ({ request }) => {
        applyBody = await request.json();
        return HttpResponse.json({ dry_run: false, applied: true, revision: 4, items: [], diagnostics: [] });
      }),
    );
    const preview = renderHook(() => usePreviewAIRuntimeImport(), { wrapper });
    const apply = renderHook(() => useApplyAIRuntimeImport(), { wrapper });
    let token = '';
    await act(async () => {
      token = (await preview.result.current.mutateAsync({ strategy: 'merge', document })).validation_token;
      await apply.result.current.mutateAsync({ strategy: 'merge', document, validationToken: token });
    });
    expect(applyBody).toEqual({ strategy: 'merge', document, validation_token: 'token-1' });
  });
});

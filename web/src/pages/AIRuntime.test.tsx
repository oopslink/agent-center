import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import AIRuntime from './AIRuntime';

const catalog = {
  org_id: 'org-1',
  revision: 4,
  default_runtime_profile_id: 'prof-1',
  clis: [{ id: 'cli-1', key: 'codex', display_name: 'Codex', executable: 'codex', required_features: [], enabled: true, system: true }],
  models: [{
    id: 'model-1',
    key: 'gpt',
    model_key: 'gpt-5',
    display_name: 'GPT-5',
    compatible_cli_keys: ['codex'],
    default_parameters: {},
    enabled: true,
  }],
  profiles: [
    { id: 'prof-1', key: 'coding', name: 'Coding', cli_key: 'codex', model_key: 'gpt', parameters: {}, enabled: true },
    { id: 'prof-2', key: 'review', name: 'Review', cli_key: 'codex', model_key: 'gpt', parameters: {}, enabled: true },
  ],
  coverage: [{
    scope: 'basic_capability_coverage',
    profile_id: 'prof-1',
    online_worker_count: 1,
    eligible_worker_count: 2,
    status: 'partial',
    calculated_at: '2026-08-06T00:00:00Z',
  }],
};

function wrap() {
  window.history.pushState({}, '', '/organizations/acme/ai-runtime');
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AIRuntime />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('AIRuntime', () => {
  it('separates basic coverage from schedulability and writes default profile with canary', async () => {
    let defaultBody: Record<string, unknown> | undefined;
    server.use(
      http.get('/api/orgs/:slug/ai-runtime', () => HttpResponse.json(catalog)),
      http.get('/api/orgs/:slug/ai-runtime/impact', () =>
        HttpResponse.json({
          entity_type: 'profile',
          entity_id: 'prof-1',
          action: 'set_default',
          reference_counts: [{ source: 'catalog_default', entity_type: 'profile', entity_id: 'prof-1', count: 1, mutable: true }],
          basic_capability_coverage: catalog.coverage,
          execution_schedulability: [],
          snapshot_back_mutation: false,
          historical_snapshot_policy: 'historical runtime snapshots are append-only',
          calculated_at: '2026-08-06T00:00:00Z',
        }),
      ),
      http.get('/api/orgs/:slug/ai-runtime/audit', () =>
        HttpResponse.json({ entries: [{ id: 'audit-1', actor: 'user:1', entity_type: 'catalog', entity_key: 'org-1', action: 'default_profile_changed', revision: 4, occurred_at: '2026-08-06T00:00:00Z' }] }),
      ),
      http.put('/api/orgs/:slug/ai-runtime/default-profile', async ({ request }) => {
        defaultBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ revision: 5, default_runtime_profile_id: 'prof-2' });
      }),
    );
    wrap();

    expect(await screen.findByTestId('ai-runtime-basic-coverage')).toHaveTextContent('partial');
    expect(screen.getByTestId('ai-runtime-schedulability-note')).toHaveTextContent(/No execution schedulability/i);
    expect(await screen.findByTestId('ai-runtime-impact')).toHaveTextContent('catalog_default');
    expect(await screen.findByTestId('ai-runtime-audit')).toHaveTextContent('default_profile_changed');

    fireEvent.change(screen.getByTestId('ai-runtime-default-select'), { target: { value: 'prof-2' } });
    fireEvent.click(screen.getByTestId('ai-runtime-canary-toggle'));
    fireEvent.change(screen.getByTestId('ai-runtime-canary-percent'), { target: { value: '25' } });
    fireEvent.click(screen.getByTestId('ai-runtime-default-save'));
    await waitFor(() => expect(defaultBody).toBeDefined());
    expect(defaultBody).toMatchObject({ expected_revision: 4, profile_id: 'prof-2', rollout: { enabled: true, label: 'canary', percent: 25 } });
  });
});

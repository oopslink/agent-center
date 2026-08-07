import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import OrgModelCatalog from './OrgModelCatalog';

const catalog = {
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

function wrap() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <OrgModelCatalog />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('OrgModelCatalog AI Runtime', () => {
  it('renders runtime profiles and separates basic coverage from schedulability', async () => {
    server.use(
      http.get('/api/ai-runtime', () => HttpResponse.json(catalog)),
      http.get('/api/ai-runtime/coverage', () =>
        HttpResponse.json({
          coverage_kind: 'basic_capability_coverage',
          schedulability_kind: 'effective_schedulability_not_inferred',
          coverage: [{ profile_id: 'rp-default', online_worker_count: 1, eligible_worker_count: 1, status: 'covered', reasons: [], calculated_at: '2026-08-06T00:00:00Z' }],
          diagnostics: [],
        }),
      ),
    );
    wrap();
    expect((await screen.findAllByText('Default coding')).length).toBeGreaterThan(0);
    expect(screen.getAllByTestId('model-catalog-row')).toHaveLength(2);
    expect(screen.getByTestId('runtime-coverage-panel')).toHaveTextContent('Basic capability coverage');
    expect(screen.getByTestId('runtime-coverage-panel')).toHaveTextContent('Effective schedulability: not inferred');
  });

  it('shows impact preview after changing the default profile', async () => {
    server.use(
      http.get('/api/ai-runtime', () => HttpResponse.json(catalog)),
      http.put('/api/ai-runtime/default-profile', () =>
        HttpResponse.json({
          revision: 4,
          default_runtime_profile_id: 'rp-codex',
          impact_preview: {
            entity_type: 'profile',
            entity_id: 'rp-codex',
            action: 'default_profile_changed',
            reference_counts: {
              profile_id: 'rp-codex',
              default_profile: 1,
              agent_profile_selections: 0,
              executor_profile_selections: 1,
              team_role_profile_selections: 2,
              team_role_inherit_selections: 3,
              historical_execution_snapshot: 0,
            },
            affected_new_runs: 7,
            historical_note: 'historical runtime snapshots are immutable and are not rewritten',
            gray_release_ready: false,
          },
        }),
      ),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('runtime-set-default'));
    await waitFor(() => expect(screen.getByTestId('runtime-impact-preview')).toBeInTheDocument());
    expect(screen.getByTestId('runtime-impact-preview')).toHaveTextContent('Affected new runs');
    expect(screen.getByTestId('runtime-impact-preview')).toHaveTextContent('Historical snapshots');
  });

  it('creates a profile using catalog-backed CLI and model selectors', async () => {
    let posted: Record<string, unknown> | undefined;
    server.use(
      http.get('/api/ai-runtime', () => HttpResponse.json(catalog)),
      http.post('/api/ai-runtime/profiles', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({
          revision: 4,
          entry: { id: 'rp-new', ...(posted.value as Record<string, unknown>) },
          impact_preview: {
            entity_type: 'profile',
            entity_id: 'rp-new',
            action: 'created',
            reference_counts: {
              profile_id: 'rp-new',
              default_profile: 0,
              agent_profile_selections: 0,
              executor_profile_selections: 0,
              team_role_profile_selections: 0,
              team_role_inherit_selections: 0,
              historical_execution_snapshot: 0,
            },
            affected_new_runs: 0,
            historical_note: 'historical runtime snapshots are immutable and are not rewritten',
            gray_release_ready: false,
          },
        }, { status: 201 });
      }),
    );
    wrap();
    const add = await screen.findByTestId('model-catalog-add-btn');
    await waitFor(() => expect(add).not.toBeDisabled());
    fireEvent.click(add);
    fireEvent.change(screen.getByTestId('runtime-profile-key'), { target: { value: 'new-profile' } });
    fireEvent.change(screen.getByTestId('runtime-profile-name'), { target: { value: 'New profile' } });
    fireEvent.change(screen.getByTestId('runtime-profile-cli'), { target: { value: 'codex' } });
    fireEvent.click(screen.getByTestId('mc-form-save'));
    await waitFor(() => expect(posted).toBeDefined());
    expect(posted).toMatchObject({
      expected_revision: 3,
      value: { key: 'new-profile', cli_key: 'codex', model_key: 'gpt-runtime' },
    });
  });
});

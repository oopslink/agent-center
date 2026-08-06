import { afterEach, describe, expect, it } from 'vitest';
import React from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { RoleBuilder, newRole } from './RoleBuilder';
import type { RoleInput } from '@/api/teams';

const runtimeCatalog = {
  org_id: 'org-1',
  revision: 1,
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
  profiles: [{ id: 'prof-1', key: 'coding', name: 'Coding', cli_key: 'codex', model_key: 'gpt', parameters: {}, enabled: true }],
  coverage: [],
};

function Harness() {
  const [roles, setRoles] = React.useState<RoleInput[]>([newRole('dev')]);
  return <RoleBuilder roles={roles} onChange={setRoles} idPrefix="test" />;
}

function wrap() {
  window.history.pushState({}, '', '/organizations/acme/teams/new');
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <Harness />
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('RoleBuilder runtime selection', () => {
  it('exposes inherit/profile/override with catalog-backed choices', async () => {
    server.use(http.get('/api/orgs/:slug/ai-runtime', () => HttpResponse.json(runtimeCatalog)));
    wrap();

    expect(screen.getByTestId('test-role-0-runtime-inherit')).toHaveTextContent(/inherit/i);
    fireEvent.change(screen.getByTestId('test-role-0-runtime-mode'), { target: { value: 'profile' } });
    await waitFor(() => expect(screen.getByTestId('test-role-0-runtime-profile')).toHaveValue('prof-1'));

    fireEvent.change(screen.getByTestId('test-role-0-runtime-mode'), { target: { value: 'override' } });
    await waitFor(() => expect(screen.getByTestId('test-role-0-cli')).toHaveValue('codex'));
    expect(screen.getByTestId('test-role-0-model')).toHaveValue('gpt');
  });
});

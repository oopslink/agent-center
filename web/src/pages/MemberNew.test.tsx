import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { server } from '@/test/mswServer';
import MemberNew from './MemberNew';

// Probe route renders the current pathname so navigation targets are assertable.
function LocationProbe() {
  const loc = useLocation();
  return <div data-testid="location-probe">{loc.pathname}</div>;
}

function wrap(path = '/members/new?kind=agent') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/members/new" element={<MemberNew />} />
          <Route path="*" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function runtimeCatalog() {
  return {
    org_id: 'org-test',
    revision: 7,
    default_runtime_profile_id: 'profile-codex',
    clis: [
      { id: 'cli-codex', key: 'codex', display_name: 'Codex CLI', executable: 'codex', enabled: true },
    ],
    models: [
      { id: 'model-gpt', key: 'gpt-5', model_key: 'gpt-5', display_name: 'GPT-5', compatible_cli_keys: ['codex'], enabled: true },
    ],
    profiles: [
      { id: 'profile-codex', key: 'default-codex', name: 'Default Codex', cli_key: 'codex', model_key: 'gpt-5', parameters: {}, enabled: true },
    ],
  };
}

function workersHandler() {
  return http.get('/api/workers', () =>
    HttpResponse.json({
      workers: [
        {
          worker_id: 'w-7',
          name: 'box-7',
          status: 'online',
          capabilities: [{ agent_cli: 'codex', detected: true, enabled: true }],
        },
      ],
    }),
  );
}

async function chooseWorker() {
  fireEvent.click(screen.getByTestId('mn-worker-trigger'));
  await waitFor(() => expect(screen.getByTestId('mn-worker-options')).toHaveTextContent('box-7'));
  fireEvent.click(screen.getByTestId('mn-worker-option'));
}

describe('MemberNew — Add agent runtime default', () => {
  afterEach(() => cleanup());

  it('selects the AI Runtime default supported by the worker and submits it when untouched', async () => {
    let posted: Record<string, unknown> | null = null;
    server.use(
      http.get('/api/ai-runtime', () => HttpResponse.json(runtimeCatalog())),
      workersHandler(),
      http.post('/api/members/agent', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 'a-new', identity_id: 'a-new', kind: 'agent', display_name: 'newbot' }, { status: 201 });
      }),
    );
    wrap();

    await userEvent.type(screen.getByLabelText('Display name'), 'newbot');
    await chooseWorker();
    await waitFor(() => expect(screen.getByLabelText(/Model/i)).toHaveValue('gpt-5'));
    expect(screen.getByLabelText('CLI')).toHaveValue('codex');

    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    await waitFor(() => expect(posted).not.toBeNull());
    expect(posted).toMatchObject({ display_name: 'newbot', worker_id: 'w-7', cli: 'codex', model: 'gpt-5' });
  });
});

// dev2/v281: Add-agent's Cancel + post-create fallback target the canonical
// /agents page (the retired /members/agents now just redirects there).
describe('MemberNew — agent navigation targets canonical /agents (dev2/v281)', () => {
  afterEach(() => cleanup());

  it('Cancel (agent kind) navigates to /agents, not /members/agents', async () => {
    server.use(
      http.get('/api/ai-runtime', () => HttpResponse.json(runtimeCatalog())),
      workersHandler(),
    );
    wrap();
    await screen.findByLabelText('Display name');
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.getByTestId('location-probe')).toHaveTextContent('/agents'));
    expect(screen.getByTestId('location-probe')).not.toHaveTextContent('/members/agents');
  });

  it('post-create fallback (no identity_id) navigates to /agents', async () => {
    server.use(
      http.get('/api/ai-runtime', () => HttpResponse.json(runtimeCatalog())),
      workersHandler(),
      // Response without identity_id → MemberNew falls back to the list page.
      http.post('/api/members/agent', () =>
        HttpResponse.json({ id: 'a-new', kind: 'agent', display_name: 'newbot' }, { status: 201 }),
      ),
    );
    wrap();
    await userEvent.type(await screen.findByLabelText('Display name'), 'newbot');
    await chooseWorker();
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    await waitFor(() => expect(screen.getByTestId('location-probe')).toHaveTextContent('/agents'));
    expect(screen.getByTestId('location-probe')).not.toHaveTextContent('/members/agents');
  });
});

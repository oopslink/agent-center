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
  return http.get('/api/ai-runtime', () =>
    HttpResponse.json({
      org_id: 'org-test',
      revision: 1,
      clis: [
        { id: 'runtime-cli-claude', key: 'claude-code', display_name: 'Claude Code', executable: 'claude', enabled: true },
      ],
      models: [
        { id: 'runtime-model-claude-opus', key: 'claude-opus-4-8', model_key: 'claude-opus-4-8', display_name: 'Claude Opus', compatible_cli_keys: ['claude-code'], enabled: true },
      ],
    }),
  );
}

function fleetWithWorker() {
  return http.get('/api/fleet', () =>
    HttpResponse.json({
      tasks: [],
      workers: [
        {
          worker_id: 'w-7',
          name: 'box-7',
          status: 'online',
          active_count: 0,
          capabilities: [{ agent_cli: 'claude-code', detected: true, enabled: true }],
        },
      ],
      pending_issues: [],
    }),
  );
}

describe('MemberNew — Add agent runtime selection', () => {
  afterEach(() => cleanup());

  it('selects the first compatible Runtime pair after worker selection and submits it when untouched', async () => {
    let posted: Record<string, unknown> | null = null;
    server.use(
      runtimeCatalog(),
      fleetWithWorker(),
      http.post('/api/members/agent', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 'a-new', identity_id: 'a-new', kind: 'agent', display_name: 'newbot' }, { status: 201 });
      }),
    );
    wrap();

    await userEvent.type(screen.getByLabelText('Display name'), 'newbot');
    // Pick the worker via the EntitySelect (open → click option).
    fireEvent.click(screen.getByTestId('mn-worker-trigger'));
    await waitFor(() => expect(screen.getByTestId('mn-worker-options')).toHaveTextContent('box-7'));
    fireEvent.click(screen.getByTestId('mn-worker-option'));
    await waitFor(() => expect((screen.getByLabelText(/Model/i) as HTMLSelectElement).value).toBe('claude-opus-4-8'));

    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    await waitFor(() => expect(posted).not.toBeNull());
    expect(posted).toMatchObject({ display_name: 'newbot', worker_id: 'w-7', cli: 'claude-code', model: 'claude-opus-4-8' });
  });
});

// dev2/v281: Add-agent's Cancel + post-create fallback target the canonical
// /agents page (the retired /members/agents now just redirects there).
describe('MemberNew — agent navigation targets canonical /agents (dev2/v281)', () => {
  afterEach(() => cleanup());

  it('Cancel (agent kind) navigates to /agents, not /members/agents', async () => {
    server.use(
      runtimeCatalog(),
      fleetWithWorker(),
    );
    wrap();
    await screen.findByLabelText('Display name');
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.getByTestId('location-probe')).toHaveTextContent('/agents'));
    expect(screen.getByTestId('location-probe')).not.toHaveTextContent('/members/agents');
  });

  it('post-create fallback (no identity_id) navigates to /agents', async () => {
    server.use(
      runtimeCatalog(),
      fleetWithWorker(),
      // Response without identity_id → MemberNew falls back to the list page.
      http.post('/api/members/agent', () =>
        HttpResponse.json({ id: 'a-new', kind: 'agent', display_name: 'newbot' }, { status: 201 }),
      ),
    );
    wrap();
    await userEvent.type(await screen.findByLabelText('Display name'), 'newbot');
    fireEvent.click(screen.getByTestId('mn-worker-trigger'));
    await waitFor(() => expect(screen.getByTestId('mn-worker-options')).toHaveTextContent('box-7'));
    fireEvent.click(screen.getByTestId('mn-worker-option'));
    await waitFor(() => expect((screen.getByLabelText(/Model/i) as HTMLSelectElement).value).toBe('claude-opus-4-8'));
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    await waitFor(() => expect(screen.getByTestId('location-probe')).toHaveTextContent('/agents'));
    expect(screen.getByTestId('location-probe')).not.toHaveTextContent('/members/agents');
  });
});

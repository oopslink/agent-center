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

describe('MemberNew — Add agent model default (#232 MemberNew gap)', () => {
  afterEach(() => cleanup());

  it('prefills Model with the explicit default and submits it when untouched', async () => {
    let posted: Record<string, unknown> | null = null;
    server.use(
      http.get('/api/workers', () =>
        HttpResponse.json({ workers: [{ worker_id: 'w-7', name: 'box-7', status: 'online' }] }),
      ),
      http.post('/api/members/agent', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 'a-new', identity_id: 'a-new', kind: 'agent', display_name: 'newbot' }, { status: 201 });
      }),
    );
    wrap();

    // Model is pre-filled with the explicit default (was an empty input → null
    // model → blank Profile, the original dogfood pain that #232 missed here).
    const model = screen.getByLabelText(/Model/i) as HTMLInputElement;
    await waitFor(() => expect(model.value).toBe('claude-opus-4-8'));

    await userEvent.type(screen.getByLabelText('Display name'), 'newbot');
    // Pick the worker via the EntitySelect (open → click option).
    fireEvent.click(screen.getByTestId('mn-worker-trigger'));
    await waitFor(() => expect(screen.getByTestId('mn-worker-options')).toHaveTextContent('box-7'));
    fireEvent.click(screen.getByTestId('mn-worker-option'));

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
      http.get('/api/workers', () =>
        HttpResponse.json({ workers: [{ worker_id: 'w-7', name: 'box-7', status: 'online' }] }),
      ),
    );
    wrap();
    await screen.findByLabelText('Display name');
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.getByTestId('location-probe')).toHaveTextContent('/agents'));
    expect(screen.getByTestId('location-probe')).not.toHaveTextContent('/members/agents');
  });

  it('post-create fallback (no identity_id) navigates to /agents', async () => {
    server.use(
      http.get('/api/workers', () =>
        HttpResponse.json({ workers: [{ worker_id: 'w-7', name: 'box-7', status: 'online' }] }),
      ),
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
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    await waitFor(() => expect(screen.getByTestId('location-probe')).toHaveTextContent('/agents'));
    expect(screen.getByTestId('location-probe')).not.toHaveTextContent('/members/agents');
  });
});

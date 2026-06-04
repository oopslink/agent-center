import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import MemberNew from './MemberNew';

function wrap(path = '/members/new?kind=agent') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/members/new" element={<MemberNew />} />
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

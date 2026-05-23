import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import AppLayout from './AppLayout';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/channels']}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/channels" element={<div data-testid="page-Channels">x</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AppLayout — Input Requests sidebar badge', () => {
  afterEach(() => cleanup());

  it('badge reflects pending IR count', async () => {
    server.use(
      http.get('/api/input_requests', () =>
        HttpResponse.json([
          { id: 'A', status: 'pending', execution_id: 'E', question: 'q', urgency: 'normal', created_at: 't' },
          { id: 'B', status: 'pending', execution_id: 'E', question: 'q', urgency: 'normal', created_at: 't' },
          { id: 'C', status: 'responded', execution_id: 'E', question: 'q', urgency: 'normal', created_at: 't', answer: 'y', decided_by: 'u', decided_at: 't' },
        ]),
      ),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('nav-badge-inputRequests')).toHaveTextContent('2'));
  });

  it('no badge rendered when zero pending', async () => {
    server.use(http.get('/api/input_requests', () => HttpResponse.json([])));
    wrap();
    await waitFor(() => expect(screen.getByTestId('page-Channels')).toBeInTheDocument());
    expect(screen.queryByTestId('nav-badge-inputRequests')).not.toBeInTheDocument();
  });
});

import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import TaskTrace from './TaskTrace';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/tasks/:id/trace" element={<TaskTrace />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('TaskTrace page', () => {
  afterEach(() => cleanup());

  it('renders the trace timeline', async () => {
    server.use(
      http.get('/api/tasks/:id/trace', () =>
        HttpResponse.json([
          {
            id: 'ev-1',
            event_type: 'tool.call',
            occurred_at: '2026-05-24T01:00:00Z',
            payload: { tool: 'Bash' },
          },
        ]),
      ),
    );
    wrap('/tasks/T-1/trace');
    await waitFor(() => expect(screen.getByTestId('trace-timeline')).toBeInTheDocument());
    expect(screen.getByTestId('trace-row')).toHaveAttribute('data-event-type', 'tool.call');
  });

  it('surfaces trace error', async () => {
    server.use(
      http.get('/api/tasks/:id/trace', () =>
        HttpResponse.json({ error: 'query_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap('/tasks/T-1/trace');
    await waitFor(() => expect(screen.getByTestId('trace-error')).toHaveTextContent(/db down/));
  });
});

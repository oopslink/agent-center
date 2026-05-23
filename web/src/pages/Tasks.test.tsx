import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import Tasks from './Tasks';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Tasks page', () => {
  afterEach(() => cleanup());

  it('renders task rows', async () => {
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json([
          { id: 'T-1', kind: 'task', name: 'rebuild docs', status: 'active' },
        ]),
      ),
    );
    wrap(<Tasks />);
    await waitFor(() => expect(screen.getByTestId('task-row')).toBeInTheDocument());
    expect(screen.getByText('rebuild docs')).toBeInTheDocument();
    expect(screen.getByText(/view trace/i)).toBeInTheDocument();
  });

  it('shows empty state', async () => {
    server.use(http.get('/api/conversations', () => HttpResponse.json([])));
    wrap(<Tasks />);
    await waitFor(() => expect(screen.getByTestId('tasks-empty')).toBeInTheDocument());
  });

  it('surfaces API error', async () => {
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap(<Tasks />);
    await waitFor(() => expect(screen.getByTestId('tasks-error')).toHaveTextContent(/db down/));
  });
});

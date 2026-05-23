import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import DMs from './DMs';

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

describe('DMs page', () => {
  afterEach(() => cleanup());

  it('renders DM rows from the API', async () => {
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json([
          { id: 'C-D1', kind: 'dm', name: 'with bot-1', status: 'active' },
          { id: 'C-D2', kind: 'dm', name: '', status: 'active' },
        ]),
      ),
    );
    wrap(<DMs />);
    await waitFor(() => expect(screen.getAllByTestId('dm-row')).toHaveLength(2));
    expect(screen.getByText('with bot-1')).toBeInTheDocument();
    // Row without a name falls back to its id.
    expect(screen.getByText('C-D2')).toBeInTheDocument();
  });

  it('shows the empty state when there are no DMs', async () => {
    server.use(http.get('/api/conversations', () => HttpResponse.json([])));
    wrap(<DMs />);
    await waitFor(() => expect(screen.getByTestId('dms-empty')).toBeInTheDocument());
  });

  it('Start a DM header button opens the modal', async () => {
    server.use(http.get('/api/conversations', () => HttpResponse.json([])));
    wrap(<DMs />);
    await waitFor(() => expect(screen.getByTestId('dms-empty')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('dms-new-button'));
    expect(screen.getByTestId('dm-start-modal')).toBeInTheDocument();
  });

  it('surfaces API error', async () => {
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap(<DMs />);
    await waitFor(() => expect(screen.getByTestId('dms-error')).toHaveTextContent(/db down/));
  });
});

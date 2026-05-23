import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import Channels from './Channels';

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

describe('Channels page', () => {
  afterEach(() => cleanup());

  it('renders the channels list from the API', async () => {
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json([
          { id: 'C1', kind: 'channel', name: 'alpha', status: 'active', description: 'plan' },
          { id: 'C2', kind: 'channel', name: 'ops', status: 'active', description: '' },
        ]),
      ),
    );
    wrap(<Channels />);
    await waitFor(() => expect(screen.getAllByTestId('channel-row')).toHaveLength(2));
    expect(screen.getByText('alpha')).toBeInTheDocument();
    expect(screen.getByText('ops')).toBeInTheDocument();
  });

  it('shows the empty state when there are no channels', async () => {
    server.use(http.get('/api/conversations', () => HttpResponse.json([])));
    wrap(<Channels />);
    await waitFor(() => expect(screen.getByTestId('channels-empty')).toBeInTheDocument());
  });

  it('opens the create modal from the header button', async () => {
    server.use(http.get('/api/conversations', () => HttpResponse.json([])));
    wrap(<Channels />);
    await waitFor(() => expect(screen.getByTestId('channels-empty')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('channels-new-button'));
    expect(screen.getByTestId('channel-create-modal')).toBeInTheDocument();
  });

  it('surfaces the API error', async () => {
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap(<Channels />);
    await waitFor(() =>
      expect(screen.getByTestId('channels-error')).toHaveTextContent(/db down/),
    );
  });
});

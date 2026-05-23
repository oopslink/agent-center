import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import ChannelDetail from './ChannelDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/channels/:name" element={<ChannelDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const channelListHandler = http.get('/api/conversations', () =>
  HttpResponse.json([
    { id: 'C-alpha', kind: 'channel', name: 'alpha', status: 'active', description: 'plan' },
  ]),
);
const channelShowHandler = http.get('/api/conversations/:id', ({ params }) =>
  HttpResponse.json({
    id: params.id,
    kind: 'channel',
    name: 'alpha',
    status: 'active',
    description: 'plan',
    participants: [
      {
        identity_id: 'user:hayang',
        role: 'owner',
        joined_at: '2026-05-24T00:00:00Z',
        joined_by: 'user:hayang',
      },
    ],
  }),
);
const messagesHandler = http.get('/api/conversations/:id/messages', () =>
  HttpResponse.json([
    {
      id: 'M1',
      conversation_id: 'C-alpha',
      sender_identity_id: 'user:hayang',
      content_kind: 'text',
      content: 'hello world',
      direction: 'inbound',
      posted_at: '2026-05-24T01:00:00Z',
    },
  ]),
);

describe('ChannelDetail page', () => {
  afterEach(() => cleanup());

  it('renders header + messages + composer when found', async () => {
    server.use(channelListHandler, channelShowHandler, messagesHandler);
    wrap('/channels/alpha');
    await waitFor(() =>
      expect(screen.getByText('hello world')).toBeInTheDocument(),
    );
    expect(screen.getByText('alpha')).toBeInTheDocument();
    expect(screen.getByTestId('message-composer')).toBeInTheDocument();
    expect(screen.getByTestId('participants-panel')).toBeInTheDocument();
    expect(screen.getByText(/1 participant/)).toBeInTheDocument();
  });

  it('shows not-found state for an unknown channel name', async () => {
    server.use(http.get('/api/conversations', () => HttpResponse.json([])));
    wrap('/channels/ghost');
    await waitFor(() =>
      expect(screen.getByTestId('channel-not-found')).toBeInTheDocument(),
    );
  });

  it('surfaces messages-error when the messages query fails', async () => {
    server.use(
      channelListHandler,
      channelShowHandler,
      http.get('/api/conversations/:id/messages', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap('/channels/alpha');
    await waitFor(() =>
      expect(screen.getByTestId('messages-error')).toHaveTextContent(/db down/),
    );
  });
});

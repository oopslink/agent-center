import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
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
          <Route path="/channels/:channelId" element={<ChannelDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// v2.7.1 #247: ChannelDetail loads by channel_id (URL segment) via the detail
// GET — no more by-name list lookup.
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

  it('loads the channel by id (URL = hash) + renders name in chrome (#247)', async () => {
    server.use(channelShowHandler, messagesHandler);
    wrap('/channels/C-alpha');
    await waitFor(() =>
      expect(screen.getByText('hello world')).toBeInTheDocument(),
    );
    // URL carries the id; the channel NAME still shows as chrome (heading + breadcrumb leaf).
    expect(screen.getByRole('heading', { name: 'alpha' })).toBeInTheDocument();
    expect(screen.getByTestId('breadcrumb')).toHaveTextContent('alpha');
    expect(screen.getByTestId('page-ChannelDetail')).toHaveAttribute('data-channel-id', 'C-alpha');
    // #264 P1: the message body now renders through the surface-agnostic shell.
    expect(screen.getByTestId('conversation-view')).toHaveAttribute('data-surface', 'channel');
    expect(screen.getByTestId('message-composer')).toBeInTheDocument();
    expect(screen.getByTestId('participants-panel')).toBeInTheDocument();
    expect(screen.getByText(/1 participant/)).toBeInTheDocument();
  });

  it('shows not-found state when the channel id does not resolve', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such channel' }, { status: 404 }),
      ),
    );
    wrap('/channels/channel-ghost');
    await waitFor(() =>
      expect(screen.getByTestId('channel-not-found')).toHaveTextContent(/no such channel/),
    );
  });

  it('surfaces a messages error via the shared shell when the messages query fails', async () => {
    server.use(
      channelShowHandler,
      http.get('/api/conversations/:id/messages', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap('/channels/C-alpha');
    // #264 P1: error now renders inside ConversationView (shared `conversation-error`).
    await waitFor(() =>
      expect(screen.getByTestId('conversation-error')).toHaveTextContent(/db down/),
    );
  });
});

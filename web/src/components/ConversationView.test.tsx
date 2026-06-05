import type React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { ConversationView } from './ConversationView';
import type { Message } from '@/api/types';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/organizations/acme/channels/C1']}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

const msg = (id: string, content: string): Message => ({
  id,
  conversation_id: 'C1',
  sender_identity_id: 'user:hayang',
  content_kind: 'text',
  content,
  direction: 'inbound',
  posted_at: '2026-05-24T01:00:00Z',
});

describe('ConversationView (#264 surface-agnostic shell)', () => {
  afterEach(() => cleanup());

  it('renders surface-tagged shell with header + message body (loading → messages) + composer', async () => {
    server.use(
      http.get('/api/conversations/C1/messages', () =>
        HttpResponse.json([msg('m1', 'hello world'), msg('m2', 'second')]),
      ),
    );
    wrap(
      <ConversationView
        surface="channel"
        conversationId="C1"
        header={<div data-testid="surface-header">#general</div>}
      />,
    );
    // surface-tagged shell + header chrome injected
    const view = screen.getByTestId('conversation-view');
    expect(view).toHaveAttribute('data-surface', 'channel');
    expect(screen.getByTestId('surface-header')).toHaveTextContent('#general');
    // body resolves messages (shared MessageList) + composer present
    await waitFor(() => expect(screen.getByText('hello world')).toBeInTheDocument());
    expect(screen.getByText('second')).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/message/i)).toBeInTheDocument(); // MessageComposer input
  });

  it('shows the error state when the message fetch fails', async () => {
    server.use(
      http.get('/api/conversations/C1/messages', () =>
        HttpResponse.json({ message: 'boom' }, { status: 500 }),
      ),
    );
    wrap(<ConversationView surface="dm" conversationId="C1" />);
    expect(await screen.findByTestId('conversation-error')).toBeInTheDocument();
    expect(screen.getByTestId('conversation-view')).toHaveAttribute('data-surface', 'dm');
  });

  it('renders the optional side panel beside the body (e.g. channel participants)', async () => {
    server.use(
      http.get('/api/conversations/C1/messages', () => HttpResponse.json([])),
    );
    wrap(
      <ConversationView
        surface="channel"
        conversationId="C1"
        sidePanel={<aside data-testid="side-panel">participants</aside>}
      />,
    );
    expect(await screen.findByTestId('side-panel')).toBeInTheDocument();
  });
});

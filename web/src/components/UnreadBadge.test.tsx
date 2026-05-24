import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import { UnreadBadge } from './UnreadBadge';

function renderBadge(convId: string) {
  return render(<UnreadBadge conversationId={convId} />, { wrapper: makeWrapper() });
}

describe('UnreadBadge', () => {
  afterEach(() => cleanup());

  it('renders nothing while loading + when count == 0', async () => {
    server.use(
      http.get('/api/conversations/:id/unread', () =>
        HttpResponse.json(
          { conversation_id: 'C1', user_id: 'user:hayang', last_seen_message_id: '', unread_count: 0 },
          { status: 200 },
        ),
      ),
    );
    renderBadge('C1');
    // Initially loading → null.
    expect(screen.queryByTestId('unread-badge')).toBeNull();
    // After the query resolves with 0, still null.
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryByTestId('unread-badge')).toBeNull();
  });

  it('renders pill with numeric count', async () => {
    server.use(
      http.get('/api/conversations/:id/unread', () =>
        HttpResponse.json(
          { conversation_id: 'C1', user_id: 'user:hayang', last_seen_message_id: 'M0', unread_count: 7 },
          { status: 200 },
        ),
      ),
    );
    renderBadge('C1');
    const badge = await screen.findByTestId('unread-badge');
    expect(badge).toHaveTextContent('7');
    expect(badge.getAttribute('data-unread-count')).toBe('7');
  });

  it('renders "999+" overflow label at the cap', async () => {
    server.use(
      http.get('/api/conversations/:id/unread', () =>
        HttpResponse.json(
          { conversation_id: 'C1', user_id: 'user:hayang', last_seen_message_id: 'M0', unread_count: 999 },
          { status: 200 },
        ),
      ),
    );
    renderBadge('C1');
    const badge = await screen.findByTestId('unread-badge');
    expect(badge).toHaveTextContent('999+');
  });

  it('renders nothing when the query errors', async () => {
    server.use(
      http.get('/api/conversations/:id/unread', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db' }, { status: 500 }),
      ),
    );
    renderBadge('C1');
    await waitFor(() => {
      // Either still loading or errored — both render null.
      expect(screen.queryByTestId('unread-badge')).toBeNull();
    });
  });
});

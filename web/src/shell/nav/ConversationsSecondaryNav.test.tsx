// v2.10.0 [T2 / T64] — Conversations col② per-module secondary nav.
// Mockup: docs/design/v2.10.0/shell-conversations-tasks.html 例1 (Channels /
// Direct messages sections with rows + unread badges; deleted-peer DM delete).
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { ConversationsSecondaryNav } from './ConversationsSecondaryNav';

function renderNav(initial = '/dms') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <ConversationsSecondaryNav orgBase="" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  server.use(
    http.get('/api/conversations', ({ request }) => {
      const kind = new URL(request.url).searchParams.get('kind');
      if (kind === 'channel') {
        return HttpResponse.json([
          { id: 'C1', kind: 'channel', name: 'agent-center-dev', status: 'active', unread_count: 3, mention_count: 1 },
          { id: 'C2', kind: 'channel', name: 'general', status: 'active', unread_count: 2, mention_count: 0 },
          { id: 'C3', kind: 'channel', name: 'archived-one', status: 'archived', unread_count: 0, mention_count: 0 },
        ]);
      }
      if (kind === 'dm') {
        return HttpResponse.json([
          { id: 'D1', kind: 'dm', status: 'active', peer_identity_id: 'user:o', peer_display_name: 'oopslink', unread_count: 1, mention_count: 0 },
          // deleted-peer DM: has a peer ref but no resolvable display name.
          { id: 'D-DEL', kind: 'dm', status: 'active', peer_identity_id: 'user:gone', unread_count: 0, mention_count: 0 },
        ]);
      }
      return HttpResponse.json([]);
    }),
  );
});
afterEach(() => cleanup());

describe('ConversationsSecondaryNav (T64 col② / 例1)', () => {
  it('renders Channels + Direct messages sections with rows linking to each conversation', async () => {
    renderNav();
    expect(await screen.findByText('Channels')).toBeInTheDocument();
    expect(screen.getByText('Direct messages')).toBeInTheDocument();

    const c1 = await screen.findByRole('link', { name: /agent-center-dev/ });
    expect(c1).toHaveAttribute('href', '/channels/C1');
    const d1 = screen.getByRole('link', { name: /oopslink/ });
    expect(d1).toHaveAttribute('href', '/dms/D1');
  });

  it('excludes archived channels', async () => {
    renderNav();
    await screen.findByRole('link', { name: /agent-center-dev/ });
    expect(screen.queryByText(/archived-one/)).toBeNull();
  });

  it('shows mention badge / unread dot per conversation', async () => {
    renderNav();
    const c1 = await screen.findByRole('link', { name: /agent-center-dev/ });
    // C1: mention_count 1 → red mention badge with the precise count.
    expect(within(c1).getByTestId('conversation-mention-badge')).toHaveTextContent('1');
    // C2: unread only → neutral unread dot.
    const c2 = screen.getByRole('link', { name: /general/ });
    expect(within(c2).getByTestId('conversation-unread-dot')).toBeInTheDocument();
  });

  it('exposes create affordances linking to the channel / DM index surfaces', async () => {
    renderNav();
    expect(await screen.findByTestId('conv-new-channel')).toHaveAttribute('href', '/channels');
    expect(screen.getByTestId('conv-new-dm')).toHaveAttribute('href', '/dms');
  });

  it('a deleted-peer DM renders "(deleted)" and keeps a manual delete action; others do not', async () => {
    renderNav();
    await screen.findByRole('link', { name: /oopslink/ });
    expect(screen.getByText('(deleted)')).toBeInTheDocument();
    const deleteButtons = screen.getAllByTestId('sidebar-dm-delete-button');
    expect(deleteButtons).toHaveLength(1); // only the deleted-peer DM
  });

  it('confirms before deleting a deleted-peer DM', async () => {
    let deleteCalled = '';
    server.use(
      http.delete('/api/conversations/:id', ({ params }) => {
        deleteCalled = String(params.id);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderNav('/dms/D-DEL');
    fireEvent.click(await screen.findByTestId('sidebar-dm-delete-button'));
    // Confirm dialog appears; nothing deleted until confirmed.
    const confirm = await screen.findByRole('button', { name: /^Delete$/ });
    expect(deleteCalled).toBe('');
    fireEvent.click(confirm);
    await waitFor(() => expect(deleteCalled).toBe('D-DEL'));
  });
});

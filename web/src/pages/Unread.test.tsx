// T343 — mobile "Unread" page: the cross-source unread digest as a full screen.
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import Unread from './Unread';
import type { UnreadConversationRow } from '@/api/types';

afterEach(() => cleanup());

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/unread']}>
        <Unread />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function mockDigest(rows: UnreadConversationRow[]) {
  server.use(http.get('/api/unread-conversations', () => HttpResponse.json(rows)));
}

const TASK_MENTION: UnreadConversationRow = {
  conversation_id: 'tc1',
  source_type: 'task',
  source_ref: 'pm://tasks/t1',
  source_id: 't1',
  project_id: 'p1',
  title: 'My churn fix',
  last_message_preview: 'can you confirm',
  last_message_sender: 'oopslink',
  updated_at: '2026-06-22T09:24:00Z',
  unread_count: 2,
  mention_count: 1,
  route: '/projects/p1/tasks/t1',
};
const CHANNEL_UNREAD: UnreadConversationRow = {
  conversation_id: 'c1',
  source_type: 'channel',
  source_ref: 'id://organizations/o1',
  source_id: 'c1',
  title: 'research-room',
  last_message_preview: 'F2 已 cut',
  last_message_sender: 'dev1',
  updated_at: '2026-06-22T08:55:00Z',
  unread_count: 3,
  mention_count: 0,
  route: '/channels/c1',
};

describe('mobile Unread page (T343)', () => {
  it('lists unread conversations with the segmented nav + mark-all-read', async () => {
    mockDigest([TASK_MENTION, CHANNEL_UNREAD]);
    renderPage();
    expect(await screen.findByTestId('page-Unread')).toBeInTheDocument();
    // the mobile Conversations segments are present (Unread | Channels | DMs)
    expect(screen.getByTestId('conv-seg-unread')).toBeInTheDocument();
    expect(screen.getByTestId('conv-seg-channels')).toBeInTheDocument();
    const rows = await screen.findAllByTestId('unread-conv-row');
    expect(rows).toHaveLength(2);
    expect(screen.getByText('My churn fix')).toBeInTheDocument();
    expect(screen.getByText('research-room')).toBeInTheDocument();
  });

  it('the @me filter narrows to mention rows only', async () => {
    mockDigest([TASK_MENTION, CHANNEL_UNREAD]);
    renderPage();
    await screen.findAllByTestId('unread-conv-row');
    fireEvent.click(screen.getByTestId('unread-filter-mentions'));
    const rows = screen.getAllByTestId('unread-conv-row');
    expect(rows).toHaveLength(1);
    expect(within(rows[0]).getByText('My churn fix')).toBeInTheDocument();
  });

  it('mark-all-read posts to the digest endpoint', async () => {
    mockDigest([TASK_MENTION, CHANNEL_UNREAD]);
    const marked = vi.fn();
    server.use(
      http.post('/api/unread-conversations/mark-all-read', () => {
        marked();
        return HttpResponse.json({ marked: 2 });
      }),
    );
    renderPage();
    await screen.findAllByTestId('unread-conv-row');
    fireEvent.click(screen.getByTestId('unread-mark-all-read'));
    await waitFor(() => expect(marked).toHaveBeenCalled());
  });

  it('shows a friendly empty state when caught up', async () => {
    mockDigest([]);
    renderPage();
    expect(await screen.findByTestId('unread-empty')).toBeInTheDocument();
    // mark-all-read is disabled with nothing to mark
    expect(screen.getByTestId('unread-mark-all-read')).toBeDisabled();
  });
});

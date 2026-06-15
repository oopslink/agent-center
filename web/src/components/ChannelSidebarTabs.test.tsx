import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import type { Participant } from '@/api/types';
import { ChannelSidebarTabs } from './ChannelSidebarTabs';

const participants: Participant[] = [
  { identity_id: 'agent:dev', role: 'member', joined_at: '2026-05-01T00:00:00Z' } as Participant,
];

const msg = (over: Record<string, unknown> = {}) => ({
  id: 'M1',
  conversation_id: 'C-1',
  sender_identity_id: 'user:hayang',
  content_kind: 'text',
  content: 'hi',
  direction: 'inbound',
  posted_at: '2026-05-24T01:00:00Z',
  ...over,
});

function mockApi({ threads = [], messages = [] }: { threads?: unknown[]; messages?: unknown[] }) {
  server.use(
    http.get('/api/conversations/:id/threads', () => HttpResponse.json(threads)),
    http.get('/api/conversations/:id/messages', () => HttpResponse.json(messages)),
  );
}

function wrap() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <ChannelSidebarTabs conversationId="C-1" participants={participants} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('ChannelSidebarTabs (v2.10.1 [T96] channel col④ 3-tab)', () => {
  afterEach(() => cleanup());

  it('renders Participants / Threads / Files tabs with Participants active by default', async () => {
    mockApi({});
    wrap();
    // v2.10.2 [T128]: the default tab is now labeled "Participants" (was "Chat").
    expect(screen.getByTestId('channel-tab-participants')).toHaveTextContent('Participants');
    expect(screen.getByTestId('channel-tab-participants')).toHaveAttribute('data-active', 'true');
    expect(screen.getByTestId('channel-tab-threads')).toHaveAttribute('data-active', 'false');
    expect(screen.getByTestId('channel-tab-files')).toHaveAttribute('data-active', 'false');
    // Participants tab shows the participants panel (with invite/remove); not the others.
    expect(await screen.findByTestId('participants-panel')).toBeInTheDocument();
    expect(screen.getByTestId('channel-panel-threads')).toHaveAttribute('hidden');
    expect(screen.queryByTestId('thread-list')).not.toBeInTheDocument();
  });

  it('Threads tab shows the thread list with a count badge', async () => {
    mockApi({
      threads: [
        { root: msg({ id: 'R1', content: 'spec talk' }), reply_count: 2, thread_last_activity_at: '2026-05-24T02:00:00Z' },
      ],
    });
    wrap();
    // count badge on the Threads tab reflects the thread count.
    expect(await screen.findByTestId('channel-tab-threads-count')).toHaveTextContent('1');
    fireEvent.click(screen.getByTestId('channel-tab-threads'));
    expect(screen.getByTestId('channel-tab-threads')).toHaveAttribute('data-active', 'true');
    const panel = screen.getByTestId('channel-panel-threads');
    expect(await within(panel).findByTestId('thread-list')).toBeInTheDocument();
    expect(within(panel).getByTestId('thread-list-items')).toBeInTheDocument();
  });

  it('Files tab shows shared files with a count badge', async () => {
    mockApi({
      messages: [
        msg({ attachments: [{ uri: 'ac://files/a', filename: 'log.txt', mime_type: 'text/plain', size: 42 }] }),
      ],
    });
    wrap();
    expect(await screen.findByTestId('channel-tab-files-count')).toHaveTextContent('1');
    fireEvent.click(screen.getByTestId('channel-tab-files'));
    const panel = screen.getByTestId('channel-panel-files');
    expect(await within(panel).findByTestId('shared-files-panel')).toBeInTheDocument();
  });

  it('Files tab shows an empty state when there are no shared files', async () => {
    mockApi({ messages: [msg({})] });
    wrap();
    fireEvent.click(screen.getByTestId('channel-tab-files'));
    const panel = screen.getByTestId('channel-panel-files');
    await waitFor(() => expect(within(panel).getByTestId('channel-files-empty')).toBeInTheDocument());
    // no count badge when zero.
    expect(screen.queryByTestId('channel-tab-files-count')).not.toBeInTheDocument();
  });
});

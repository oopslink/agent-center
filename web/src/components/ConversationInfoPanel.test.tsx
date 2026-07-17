import type React from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import type { Participant } from '@/api/types';
import { ConversationInfoSheet } from './ConversationInfoPanel';

// mobile-redesign-conversations.md §3.5 + §5, mockup frame ⑦ — the read-only
// info card shown inside the Context Panel bottom sheet. The ⓘ button itself is
// the shared <ContextPanelMobileButton> (covered by ContextPanelMobileEntry.test
// + the ChannelDetail/DMDetail page tests, which assert the real route wiring).

const participants: Participant[] = [
  { identity_id: 'user:alice', role: 'member', joined_at: '2026-05-01T00:00:00Z' } as Participant,
  { identity_id: 'agent:dev', role: 'member', joined_at: '2026-05-01T00:00:00Z' } as Participant,
  {
    identity_id: 'user:gone',
    role: 'member',
    joined_at: '2026-05-01T00:00:00Z',
    left_at: '2026-05-02T00:00:00Z',
  } as Participant,
];

function mockApi({ messages = [] }: { messages?: unknown[] } = {}) {
  server.use(
    http.get('/api/conversations/:id/messages', () => HttpResponse.json(messages)),
    http.get('/api/members', () => HttpResponse.json([])),
    http.get('/api/agents', () => HttpResponse.json([])),
  );
}

function withQuery(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>
  );
}

describe('ConversationInfoSheet', () => {
  afterEach(() => cleanup());
  // The resolver hits /api/members + /api/agents; an unresolved ref falls back to
  // the raw identity ref, which is all these assertions need.

  it('shows the channel title + description and an active-members preview', async () => {
    mockApi();
    render(
      withQuery(
        <ConversationInfoSheet
          conversationId="C-1"
          title="# general"
          description="工程日常讨论与公告"
          participants={participants}
        />,
      ),
    );
    expect(screen.getByTestId('conversation-info-title')).toHaveTextContent('# general');
    expect(screen.getByTestId('conversation-info-description')).toHaveTextContent('工程日常讨论与公告');
    // 3 participants, 1 departed → Members (2), 2 rows.
    expect(screen.getByTestId('conversation-info-members')).toHaveTextContent('Members (2)');
    expect(screen.getAllByTestId('conversation-info-member-row')).toHaveLength(2);
  });

  it('omits the description block when the channel has none', () => {
    mockApi();
    render(
      withQuery(<ConversationInfoSheet conversationId="C-1" title="# general" participants={participants} />),
    );
    expect(screen.queryByTestId('conversation-info-description')).not.toBeInTheDocument();
  });

  it('hides the Members preview for a DM (showMembers=false, fixed 1:1)', () => {
    mockApi();
    render(withQuery(<ConversationInfoSheet conversationId="C-1" title="@alice" showMembers={false} />));
    expect(screen.queryByTestId('conversation-info-members')).not.toBeInTheDocument();
    // Files still shows (a DM can share attachments).
    expect(screen.getByTestId('conversation-info-files')).toBeInTheDocument();
  });

  it('lists shared files from message attachments, deduped, with an empty state otherwise', async () => {
    mockApi({
      messages: [
        {
          id: 'M1',
          conversation_id: 'C-1',
          sender_identity_id: 'user:alice',
          content_kind: 'text',
          content: 'see this',
          direction: 'inbound',
          posted_at: '2026-05-24T01:00:00Z',
          attachments: [{ uri: 'f://1', filename: '部署脚本.sh', mime_type: 'text/plain', size: 10 }],
        },
      ],
    });
    render(withQuery(<ConversationInfoSheet conversationId="C-1" title="# general" showMembers={false} />));
    expect(await screen.findByText('部署脚本.sh')).toBeInTheDocument();
    expect(screen.getByTestId('conversation-info-files')).toHaveTextContent('Files (1)');
  });

  it('shows the files empty state when nothing has been shared', async () => {
    mockApi();
    render(withQuery(<ConversationInfoSheet conversationId="C-1" title="# general" showMembers={false} />));
    expect(await screen.findByTestId('conversation-info-files-empty')).toBeInTheDocument();
  });

  it('caps the members preview at 5 rows and shows a "+N more" tail', () => {
    mockApi();
    const many: Participant[] = Array.from({ length: 8 }, (_, i) => ({
      identity_id: `user:u${i}`,
      role: 'member',
      joined_at: '2026-05-01T00:00:00Z',
    })) as Participant[];
    render(withQuery(<ConversationInfoSheet conversationId="C-1" title="# general" participants={many} />));
    expect(screen.getAllByTestId('conversation-info-member-row')).toHaveLength(5);
    expect(screen.getByTestId('conversation-info-members-more')).toHaveTextContent('3');
  });

  it('shows the no-members empty state for a channel with no active participants', () => {
    mockApi();
    render(withQuery(<ConversationInfoSheet conversationId="C-1" title="# general" participants={[]} />));
    expect(screen.getByTestId('conversation-info-members-empty')).toBeInTheDocument();
  });
});

// Guard the redesign's structural promise: the ⓘ sheet is a DIFFERENT density
// from the segment panels (read-only preview vs full interactive panels), which
// is how spec §3.5 and §5 reconcile (mockup frames ④ vs ⑦).
describe('ConversationInfoSheet vs the segment panels', () => {
  afterEach(() => cleanup());
  it('is read-only — it renders no tablist/panel chrome of its own', () => {
    mockApi();
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: true, media: query, onchange: null,
      addEventListener: () => {}, removeEventListener: () => {},
      addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false,
    }));
    render(
      withQuery(<ConversationInfoSheet conversationId="C-1" title="# general" participants={participants} />),
    );
    expect(screen.queryByRole('tablist')).not.toBeInTheDocument();
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
    vi.unstubAllGlobals();
  });
});

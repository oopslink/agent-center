import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import type React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useAppStore } from '@/store/app';
import { server } from '@/test/mswServer';
import { ParticipantsPanel } from './ParticipantsPanel';
import type { Participant } from '@/api/types';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const owner: Participant = {
  identity_id: 'user:hayang',
  role: 'owner',
  joined_at: '2026-05-24T00:00:00Z',
  joined_by: 'user:hayang',
};
const member: Participant = {
  identity_id: 'agent:bot-1',
  role: 'member',
  joined_at: '2026-05-24T00:01:00Z',
  joined_by: 'user:hayang',
};

describe('ParticipantsPanel', () => {
  it('renders a Threads section listing the conversation threads', async () => {
    server.use(
      http.get('/api/conversations/:id/threads', () =>
        HttpResponse.json(
          [
            {
              root: {
                id: 'M1',
                conversation_id: 'C1',
                sender_identity_id: 'user:hayang',
                content_kind: 'text',
                content: 'a panel thread',
                direction: 'inbound',
                posted_at: '2026-06-12T00:00:00Z',
              },
              reply_count: 2,
            },
          ],
          { status: 200 },
        ),
      ),
    );
    wrap(<ParticipantsPanel conversationId="C1" participants={[owner, member]} />);
    expect(await screen.findByTestId('thread-list')).toBeInTheDocument();
    expect(await screen.findByText(/a panel thread/)).toBeInTheDocument();
  });

  beforeEach(() => {
    useAppStore.setState({ currentUserId: 'user:hayang' });
    // localStorage may carry collapsed=1 between tests; reset to expanded.
    try {
      localStorage.removeItem('ac.participants.collapsed');
    } catch {
      // ignore
    }
  });
  afterEach(() => cleanup());

  it('shows participant count chip + each active participant', () => {
    wrap(<ParticipantsPanel conversationId="C1" participants={[owner, member]} />);
    // 8th channel redesign: count now lives in a dedicated pill.
    expect(screen.getByTestId('participants-count-chip')).toHaveTextContent('2');
    expect(screen.getAllByTestId('participant-row')).toHaveLength(2);
  });

  it('shows OWNER / MEMBER role badges (not-color-only: literal text)', () => {
    wrap(<ParticipantsPanel conversationId="C1" participants={[owner, member]} />);
    const badges = screen.getAllByTestId('participant-role-badge');
    expect(badges).toHaveLength(2);
    const ownerBadge = badges.find((b) => b.getAttribute('data-role') === 'owner');
    const memberBadge = badges.find((b) => b.getAttribute('data-role') === 'member');
    expect(ownerBadge).toHaveTextContent('OWNER');
    expect(memberBadge).toHaveTextContent('MEMBER');
  });

  it('renders "(deleted)" for a participant whose member no longer resolves (#192/E1)', async () => {
    // No members resolve → a deleted-agent participant must read "(deleted)",
    // never the raw agent: ref.
    server.use(http.get('/api/members', () => HttpResponse.json([])));
    const gone: Participant = {
      identity_id: 'agent:gone-1',
      role: 'member',
      joined_at: '2026-05-24T00:02:00Z',
      joined_by: 'user:hayang',
    };
    wrap(<ParticipantsPanel conversationId="C1" participants={[owner, gone]} />);
    await waitFor(() => {
      const goneEl = screen
        .getAllByTestId('participant-name')
        .find((n) => n.getAttribute('data-entity-id') === 'agent:gone-1');
      expect(goneEl).toBeDefined();
      expect(goneEl).toHaveTextContent('(deleted)');
      expect(goneEl).toHaveAttribute('data-deleted', 'true');
    });
  });

  it('hides Invite button + remove buttons when caller is not the owner', () => {
    useAppStore.setState({ currentUserId: 'user:someone-else' });
    wrap(<ParticipantsPanel conversationId="C1" participants={[owner, member]} />);
    expect(screen.queryByTestId('invite-open')).not.toBeInTheDocument();
    expect(screen.queryByTestId('participant-remove')).not.toBeInTheDocument();
  });

  it('owner sees Invite button which opens the member-invite modal', () => {
    wrap(<ParticipantsPanel conversationId="C1" participants={[owner]} />);
    fireEvent.click(screen.getByTestId('invite-open'));
    expect(screen.getByTestId('member-invite-modal')).toBeInTheDocument();
  });

  it('owner can remove a non-owner participant', async () => {
    let removed: string | undefined;
    server.use(
      http.delete('/api/conversations/:id/participants/:identity_id', ({ params }) => {
        removed = params.identity_id as string;
        return HttpResponse.json({ event_id: 'E-rm' });
      }),
    );
    wrap(<ParticipantsPanel conversationId="C1" participants={[owner, member]} />);
    fireEvent.click(screen.getByTestId('participant-remove'));
    await waitFor(() => expect(removed).toBe('agent:bot-1'));
  });

  it('collapses and expands the panel', () => {
    wrap(<ParticipantsPanel conversationId="C1" participants={[owner, member]} />);
    fireEvent.click(screen.getByTestId('participants-collapse'));
    expect(screen.getByTestId('participants-panel')).toHaveAttribute('data-collapsed', 'true');
    fireEvent.click(screen.getByTestId('participants-expand'));
    expect(screen.getByTestId('participants-panel')).toHaveAttribute('data-collapsed', 'false');
  });
});

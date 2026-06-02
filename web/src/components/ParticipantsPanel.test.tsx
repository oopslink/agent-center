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

  it('shows participant count + each active participant', () => {
    wrap(<ParticipantsPanel conversationId="C1" participants={[owner, member]} />);
    expect(screen.getByText(/Participants \(2\)/)).toBeInTheDocument();
    expect(screen.getAllByTestId('participant-row')).toHaveLength(2);
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

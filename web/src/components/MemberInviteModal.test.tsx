import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import type React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { MemberInviteModal } from './MemberInviteModal';
import type { Participant } from '@/api/types';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const members = [
  { id: 'm-alice', organization_id: 'org', identity_id: 'user-alice', kind: 'user', role: 'member', status: 'joined', joined_at: 't', display_name: 'Alice' },
  { id: 'm-bob', organization_id: 'org', identity_id: 'user-bob', kind: 'user', role: 'member', status: 'joined', joined_at: 't', display_name: 'Bob' },
  { id: 'm-bot', organization_id: 'org', identity_id: 'agent-bot', kind: 'agent', role: 'member', status: 'joined', joined_at: 't', display_name: 'BotOne' },
];

// alice is an ACTIVE participant → excluded. bob was KICKED from this channel
// (left_at set) → still a valid candidate (re-invitable, per PD's §-1 clarification).
const existing: Participant[] = [
  { identity_id: 'user:user-alice', role: 'member', joined_at: 't', joined_by: 'user:owner' },
  { identity_id: 'user:user-bob', role: 'member', joined_at: 't', joined_by: 'user:owner', left_at: 't2', left_reason: 'kicked' },
];

function mockMembers() {
  server.use(http.get('/api/members', () => HttpResponse.json(members)));
}

describe('MemberInviteModal (#167)', () => {
  afterEach(() => cleanup());

  it('lists org members as candidates with Human/Agent tags, excluding existing participants', async () => {
    mockMembers();
    wrap(<MemberInviteModal conversationId="C1" participants={existing} onClose={() => {}} />);
    await waitFor(() => expect(screen.getAllByTestId('invite-candidate').length).toBe(2));
    expect(screen.getByText('Bob')).toBeInTheDocument();
    expect(screen.getByText('BotOne')).toBeInTheDocument();
    expect(screen.queryByText('Alice')).not.toBeInTheDocument(); // already a participant
    const kinds = screen.getAllByTestId('invite-candidate-kind').map((e) => e.textContent);
    expect(kinds).toContain('Human');
    expect(kinds).toContain('Agent');
  });

  it('filters candidates by the search query', async () => {
    mockMembers();
    wrap(<MemberInviteModal conversationId="C1" participants={existing} onClose={() => {}} />);
    await waitFor(() => expect(screen.getAllByTestId('invite-candidate').length).toBe(2));
    await userEvent.type(screen.getByTestId('invite-search'), 'bot');
    await waitFor(() => expect(screen.getAllByTestId('invite-candidate').length).toBe(1));
    expect(screen.getByText('BotOne')).toBeInTheDocument();
  });

  it('multi-selects and batch-invites the chosen members with prefixed refs', async () => {
    mockMembers();
    const invited: string[] = [];
    server.use(
      http.post('/api/conversations/:id/participants', async ({ request }) => {
        const body = (await request.json()) as { identity_id: string };
        invited.push(body.identity_id);
        return HttpResponse.json({ event_id: 'E' });
      }),
    );
    let closed = false;
    wrap(<MemberInviteModal conversationId="C1" participants={existing} onClose={() => { closed = true; }} />);
    await waitFor(() => expect(screen.getAllByTestId('invite-candidate').length).toBe(2));
    const checks = screen.getAllByTestId('invite-candidate-check');
    fireEvent.click(checks[0]); // user-bob
    fireEvent.click(checks[1]); // agent-bot
    fireEvent.click(screen.getByTestId('invite-confirm'));
    await waitFor(() => expect(closed).toBe(true));
    expect(invited.sort()).toEqual(['agent:agent-bot', 'user:user-bob']);
  });
});

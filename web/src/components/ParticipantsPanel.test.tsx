import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
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
  });
  afterEach(() => cleanup());

  it('shows participant count + each active participant', () => {
    wrap(<ParticipantsPanel conversationId="C1" participants={[owner, member]} />);
    expect(screen.getByText(/Participants \(2\)/)).toBeInTheDocument();
    expect(screen.getAllByTestId('participant-row')).toHaveLength(2);
  });

  it('hides invite form + remove buttons when caller is not the owner', () => {
    useAppStore.setState({ currentUserId: 'user:someone-else' });
    wrap(<ParticipantsPanel conversationId="C1" participants={[owner, member]} />);
    expect(screen.queryByTestId('invite-input')).not.toBeInTheDocument();
    expect(screen.queryByTestId('participant-remove')).not.toBeInTheDocument();
  });

  it('owner can invite a new identity via the form', async () => {
    server.use(
      http.post('/api/conversations/:id/participants', () =>
        HttpResponse.json({ event_id: 'E-inv' }),
      ),
    );
    wrap(<ParticipantsPanel conversationId="C1" participants={[owner]} />);
    const input = screen.getByTestId('invite-input') as HTMLInputElement;
    await userEvent.type(input, 'agent:newbie');
    fireEvent.click(screen.getByTestId('invite-submit'));
    await waitFor(() => expect(input.value).toBe(''));
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

  it('surfaces invite errors without clearing the input', async () => {
    server.use(
      http.post('/api/conversations/:id/participants', () =>
        HttpResponse.json({ error: 'invalid_input', message: 'bad id' }, { status: 400 }),
      ),
    );
    wrap(<ParticipantsPanel conversationId="C1" participants={[owner]} />);
    const input = screen.getByTestId('invite-input') as HTMLInputElement;
    await userEvent.type(input, 'no-prefix');
    fireEvent.click(screen.getByTestId('invite-submit'));
    await waitFor(() => expect(screen.getByTestId('invite-error')).toBeInTheDocument());
    expect(input.value).toBe('no-prefix');
  });
});

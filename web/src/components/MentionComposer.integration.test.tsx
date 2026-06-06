import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import type React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { MessageComposer } from './MessageComposer';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe('MessageComposer + #275 mention picker', () => {
  beforeEach(() => {
    server.use(
      http.post('/api/conversations/:id/messages', () =>
        HttpResponse.json({ message_id: 'M', event_id: 'E' }, { status: 201 }),
      ),
      http.get('/api/members', () =>
        HttpResponse.json([
          { identity_id: 'u-alice', kind: 'user', display_name: 'Alice' },
          { identity_id: 'a-bob', kind: 'agent', display_name: 'Bob' },
        ]),
      ),
      http.get('/api/conversations', () =>
        HttpResponse.json([
          { id: 'c-gen', kind: 'channel', name: 'general', status: 'active' },
          { id: 'c-dm', kind: 'dm', name: '', status: 'active' },
        ]),
      ),
    );
  });
  afterEach(() => cleanup());

  const ta = () => screen.getByTestId('composer-textarea') as HTMLTextAreaElement;

  it('typing @ opens the picker with filtered members', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    await userEvent.type(ta(), '@a');
    await waitFor(() => expect(screen.getByTestId('mention-picker')).toBeInTheDocument());
    expect(screen.getByText('Alice')).toBeInTheDocument();
    expect(screen.queryByText('Bob')).not.toBeInTheDocument(); // 'a' filters Bob out
    // combobox aria wired
    expect(ta()).toHaveAttribute('aria-expanded', 'true');
    expect(ta()).toHaveAttribute('aria-controls');
  });

  it('ArrowDown + Enter inserts the mention token WITH trailing space (wake boundary)', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    await userEvent.type(ta(), '@');
    await waitFor(() => expect(screen.getByText('Bob')).toBeInTheDocument());
    fireEvent.keyDown(ta(), { key: 'ArrowDown' }); // active → Bob
    fireEvent.keyDown(ta(), { key: 'Enter' }); // select (not send)
    await waitFor(() => expect(ta().value).toBe('@Bob '));
    // picker closed after select
    expect(screen.queryByTestId('mention-picker')).not.toBeInTheDocument();
  });

  it('Enter while the picker is open selects, does NOT send the message', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    await userEvent.type(ta(), '@a');
    await waitFor(() => expect(screen.getByText('Alice')).toBeInTheDocument());
    fireEvent.keyDown(ta(), { key: 'Enter' });
    // selected → textarea now holds the token (message not cleared/sent)
    await waitFor(() => expect(ta().value).toBe('@Alice '));
  });

  it('typing # opens the channel picker (channels only)', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    await userEvent.type(ta(), '#g');
    await waitFor(() => expect(screen.getByTestId('mention-picker')).toBeInTheDocument());
    expect(screen.getByText('general')).toBeInTheDocument();
  });

  it('Escape closes the picker without inserting', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    await userEvent.type(ta(), '@a');
    await waitFor(() => expect(screen.getByTestId('mention-picker')).toBeInTheDocument());
    // Esc keydown closes; the trailing keyUp fires sync() — which must NOT reopen
    // the still-present "@a" trigger (FINDING-1 Esc runtime no-op regression).
    fireEvent.keyDown(ta(), { key: 'Escape' });
    fireEvent.keyUp(ta(), { key: 'Escape' });
    await waitFor(() => expect(screen.queryByTestId('mention-picker')).not.toBeInTheDocument());
    expect(ta().value).toBe('@a'); // unchanged
    // stays dismissed across further keyups (caret unchanged) ...
    fireEvent.keyUp(ta(), { key: 'Escape' });
    expect(screen.queryByTestId('mention-picker')).not.toBeInTheDocument();
  });

  it('typing after Esc reopens the picker (dismissal expires on trigger change)', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    await userEvent.type(ta(), '@a');
    await waitFor(() => expect(screen.getByTestId('mention-picker')).toBeInTheDocument());
    fireEvent.keyDown(ta(), { key: 'Escape' });
    fireEvent.keyUp(ta(), { key: 'Escape' });
    await waitFor(() => expect(screen.queryByTestId('mention-picker')).not.toBeInTheDocument());
    // typing changes the query → dismissal expires → picker reopens
    await userEvent.type(ta(), 'l');
    await waitFor(() => expect(screen.getByTestId('mention-picker')).toBeInTheDocument());
  });
});

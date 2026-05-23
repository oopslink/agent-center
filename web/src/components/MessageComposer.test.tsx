import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
} from 'vitest';
import { cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
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

describe('MessageComposer', () => {
  beforeEach(() => {
    // Reset MSW handler to a fast 201 by default.
    server.use(
      http.post('/api/conversations/:id/messages', () =>
        HttpResponse.json({ message_id: 'M-NEW', event_id: 'E-1' }, { status: 201 }),
      ),
    );
  });
  afterEach(() => cleanup());

  it('disables send button until there is non-whitespace input', () => {
    wrap(<MessageComposer conversationId="C1" />);
    expect(screen.getByTestId('composer-send')).toBeDisabled();
  });

  it('sends on Enter and clears the textarea', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    const ta = screen.getByTestId('composer-textarea') as HTMLTextAreaElement;
    await userEvent.type(ta, 'hello');
    expect(screen.getByTestId('composer-send')).not.toBeDisabled();
    fireEvent.keyDown(ta, { key: 'Enter' });
    await waitFor(() => expect(ta.value).toBe(''));
  });

  it('Shift+Enter inserts a newline instead of sending', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    const ta = screen.getByTestId('composer-textarea') as HTMLTextAreaElement;
    await userEvent.type(ta, 'line1');
    fireEvent.keyDown(ta, { key: 'Enter', shiftKey: true });
    // textarea still has the original content (shift+enter is a default
    // text insertion; we just assert the send did not clear it).
    expect(ta.value).toBe('line1');
  });

  it('surfaces send errors and keeps the draft intact', async () => {
    server.use(
      http.post('/api/conversations/:id/messages', () =>
        HttpResponse.json({ error: 'too_long', message: 'message too long' }, { status: 400 }),
      ),
    );
    wrap(<MessageComposer conversationId="C1" />);
    const ta = screen.getByTestId('composer-textarea') as HTMLTextAreaElement;
    await userEvent.type(ta, 'oops');
    fireEvent.click(screen.getByTestId('composer-send'));
    await waitFor(() => expect(screen.getByTestId('composer-error')).toBeInTheDocument());
    expect(ta.value).toBe('oops'); // preserved for retry
  });
});

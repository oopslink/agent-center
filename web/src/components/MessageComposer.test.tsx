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

  it('does not send on Enter during an IME composition; sends after it ends (#222)', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    const ta = screen.getByTestId('composer-textarea') as HTMLTextAreaElement;
    await userEvent.type(ta, '你好');
    // Composition active: the Enter that confirms the IME candidate must NOT send.
    fireEvent.compositionStart(ta);
    fireEvent.keyDown(ta, { key: 'Enter' });
    expect(ta.value).toBe('你好'); // not cleared → not sent
    // Composition ends → a real Enter sends.
    fireEvent.compositionEnd(ta);
    fireEvent.keyDown(ta, { key: 'Enter' });
    await waitFor(() => expect(ta.value).toBe(''));
  });

  it('does not send when the keydown reports isComposing (#222)', async () => {
    wrap(<MessageComposer conversationId="C1" />);
    const ta = screen.getByTestId('composer-textarea') as HTMLTextAreaElement;
    await userEvent.type(ta, 'abc');
    fireEvent.keyDown(ta, { key: 'Enter', isComposing: true });
    expect(ta.value).toBe('abc'); // guarded by e.nativeEvent.isComposing
  });

  it('renders icon attach + send buttons with tooltips (#222)', () => {
    wrap(<MessageComposer conversationId="C1" />);
    const composer = screen.getByTestId('message-composer');
    const textarea = screen.getByTestId('composer-textarea');
    const attach = screen.getByTestId('composer-attach');
    const send = screen.getByTestId('composer-send');
    expect(attach).toHaveAttribute('aria-label', 'Attach file');
    expect(send).toHaveAttribute('title', 'Send (Enter)');
    expect(send).toHaveAttribute('aria-label', 'Send');
    // Keep the two icon controls aligned with the one-line textarea.
    expect(composer.className).toContain('items-center');
    expect(textarea.className).toContain('h-10');
    expect(textarea.className).toContain('leading-10');
    expect(textarea.className).not.toContain('min-h');
    expect(attach.className).toContain('h-10');
    expect(attach.className).toContain('w-10');
    expect(send.className).toContain('h-10');
    expect(send.className).toContain('w-10');
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

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

  // v2.10.2 [T148] owner-directed: the attach (+) and send buttons sit in a bottom
  // action bar and are one size smaller (h-8 w-8 = 32px), consistent across desktop
  // + mobile. This SUPERSEDES the v2.10.1 [M2] ≥44px touch sizing for these two
  // composer controls per owner request (flagged for Tester2 touch/AA review).
  it('attach + send buttons are the smaller, consistent size in the bottom bar (T148)', () => {
    wrap(<MessageComposer conversationId="C1" />);
    for (const id of ['composer-attach', 'composer-send']) {
      const cls = screen.getByTestId(id).className;
      expect(cls).toContain('h-8');
      expect(cls).toContain('w-8');
      // no responsive split + no leftover larger sizing.
      expect(cls).not.toContain('h-11');
      expect(cls).not.toContain('md:h-10');
    }
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
    // v2.10.2 [T148]: vertical stack (textarea on top, action bar at the bottom),
    // an auto-growing textarea (leading-5, no fixed h-10/leading-10), and the
    // smaller h-8 buttons grouped in a bottom bar.
    expect(composer.className).toContain('flex-col');
    expect(textarea.className).toContain('leading-5');
    expect(textarea.className).not.toContain('h-10');
    expect(textarea.className).not.toContain('leading-10');
    expect(attach.className).toContain('h-8');
    expect(attach.className).toContain('w-8');
    expect(send.className).toContain('h-8');
    expect(send.className).toContain('w-8');
    // attach + send share one parent action bar (the bottom row), distinct from
    // the textarea above.
    expect(attach.parentElement).toBe(send.parentElement);
    expect(attach.parentElement?.className).toContain('items-center');
  });

  // v2.10.2 [T148]: the textarea auto-grows (JS-driven height + overflow toggle,
  // capped at 4 rows). jsdom has no layout engine, so we assert the WIRING — the
  // effect set an explicit inline height + overflow-y and the field starts at one
  // row with manual resize off — not the pixel growth (a real-window check).
  it('auto-grow textarea is JS-driven (height + overflow set), starts at 1 row (T148)', () => {
    wrap(<MessageComposer conversationId="C1" />);
    const ta = screen.getByTestId('composer-textarea') as HTMLTextAreaElement;
    expect(ta.getAttribute('rows')).toBe('1');
    expect(ta.className).toContain('resize-none');
    // the auto-grow effect runs on mount → an inline height + overflow-y are set.
    expect(ta.style.height).not.toBe('');
    expect(['auto', 'hidden']).toContain(ta.style.overflowY);
  });

  it('sends parent_message_id when given a parentMessageId (thread reply)', async () => {
    let seenParent: string | undefined;
    server.use(
      http.post('/api/conversations/:id/messages', async ({ request }) => {
        const body = (await request.json()) as { parent_message_id?: string; content: string };
        seenParent = body.parent_message_id;
        return HttpResponse.json({ message_id: 'M-NEW', event_id: 'E-1' }, { status: 201 });
      }),
    );
    wrap(<MessageComposer conversationId="C1" parentMessageId="M-root" />);
    const ta = screen.getByTestId('composer-textarea') as HTMLTextAreaElement;
    await userEvent.type(ta, 'a reply');
    fireEvent.keyDown(ta, { key: 'Enter' });
    await waitFor(() => expect(ta.value).toBe(''));
    expect(seenParent).toBe('M-root');
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

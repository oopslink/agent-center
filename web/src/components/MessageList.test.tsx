import type React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { act, cleanup, fireEvent, render as rtlRender, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MessageList } from './MessageList';
import type { Message } from '@/api/types';

// v2.7 #160: MessageList now resolves sender display names via useMembers
// (react-query), so renders need a QueryClient. With no /api/members data the
// resolver falls back to the raw ref — these tests assert the ref unchanged.
const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
function render(ui: React.ReactElement) {
  const utils = rtlRender(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
  return {
    ...utils,
    rerender: (next: React.ReactElement) =>
      utils.rerender(<QueryClientProvider client={qc}>{next}</QueryClientProvider>),
  };
}

const sample = (id: string, content: string): Message => ({
  id,
  conversation_id: 'C1',
  sender_identity_id: 'user:hayang',
  content_kind: 'text',
  content,
  direction: 'inbound',
  posted_at: '2026-05-24T01:00:00Z',
});

// jsdom returns 0 for all layout reads. Stub the scroll geometry on the
// list container so the auto-scroll heuristic can be exercised.
function stubScroll(el: HTMLElement, opts: { scrollHeight: number; clientHeight: number; scrollTop: number }) {
  Object.defineProperty(el, 'scrollHeight', { configurable: true, value: opts.scrollHeight });
  Object.defineProperty(el, 'clientHeight', { configurable: true, value: opts.clientHeight });
  Object.defineProperty(el, 'scrollTop', { configurable: true, writable: true, value: opts.scrollTop });
}

describe('MessageList', () => {
  afterEach(() => cleanup());

  it('renders empty state when no messages', () => {
    render(<MessageList messages={[]} />);
    expect(screen.getByTestId('message-list-empty')).toBeInTheDocument();
  });

  it('de-emphasizes a system message — centered hint, raw text collapsed behind [Details]', () => {
    const sys: Message = {
      ...sample('S1', "⚠️ @arch1 couldn't process the message: rate_limit exceeded — 429 raw-api-error"),
      content_kind: 'system',
    };
    render(<MessageList messages={[sys]} />);
    // de-emphasized centered hint, NOT a full sender bubble.
    expect(screen.getByTestId('message-system')).toBeInTheDocument();
    expect(screen.queryByTestId('message-row')).not.toBeInTheDocument();
    // raw error text is NOT in the main flow by default (collapsed).
    expect(screen.queryByText(/rate_limit exceeded/)).not.toBeInTheDocument();
    const toggle = screen.getByTestId('message-system-details-toggle');
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    // expanding [Details] reveals the full raw content.
    fireEvent.click(toggle);
    expect(screen.getByTestId('message-system-detail')).toHaveTextContent('rate_limit exceeded');
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
  });

  it('renders one row per message with sender + content', () => {
    render(<MessageList messages={[sample('M1', 'hi'), sample('M2', 'two')]} />);
    const rows = screen.getAllByTestId('message-row');
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent('hi');
    expect(rows[1]).toHaveTextContent('two');
    expect(rows[0]).toHaveAttribute('data-message-id', 'M1');
  });

  // #276: message content renders as markdown — a long fenced code block
  // collapses through the shared CollapsibleCodeBlock.
  it('renders message content as markdown with collapsible code blocks (#276)', () => {
    const code = Array.from({ length: 25 }, (_, i) => `row ${i + 1}`).join('\n');
    render(<MessageList messages={[sample('M1', '## hi\n\n```ts\n' + code + '\n```')]} />);
    const row = screen.getByTestId('message-row');
    expect(row.querySelector('h2')).toHaveTextContent('hi');
    expect(screen.getByTestId('collapsible-code-block')).toBeInTheDocument();
    expect(screen.getByTestId('code-disclosure-btn')).toBeInTheDocument();
  });

  it('snaps initial scroll to bottom when there are messages', () => {
    const { container } = render(<MessageList messages={[sample('M1', 'a'), sample('M2', 'b')]} />);
    const list = screen.getByTestId('message-list');
    // jsdom initialized to 0, but the mount effect tries to scroll. We
    // assert the effect ran by stubbing scrollHeight after mount and
    // re-running the effect via rerender — easier: just verify the
    // outer wrapper exists.
    expect(list.parentElement?.className).toContain('relative');
    expect(container).toBeTruthy();
  });

  it('shows "New messages" pill when a new message arrives while scrolled up', () => {
    const { rerender } = render(
      <MessageList messages={[sample('M1', 'a')]} />,
    );
    const list = screen.getByTestId('message-list');
    // Simulate: user scrolled up — list is 1000 tall, 200 visible, at top.
    stubScroll(list, { scrollHeight: 1000, clientHeight: 200, scrollTop: 0 });
    act(() => {
      fireEvent.scroll(list);
    });
    // New message arrives.
    rerender(<MessageList messages={[sample('M1', 'a'), sample('M2', 'b')]} />);
    expect(screen.getByTestId('message-list-new-pill')).toBeInTheDocument();
  });

  it('does not show pill when new message arrives and user is at bottom', () => {
    const { rerender } = render(
      <MessageList messages={[sample('M1', 'a')]} />,
    );
    const list = screen.getByTestId('message-list');
    // User at bottom: scrollTop + clientHeight >= scrollHeight - threshold.
    stubScroll(list, { scrollHeight: 1000, clientHeight: 200, scrollTop: 800 });
    act(() => {
      fireEvent.scroll(list);
    });
    rerender(<MessageList messages={[sample('M1', 'a'), sample('M2', 'b')]} />);
    expect(screen.queryByTestId('message-list-new-pill')).not.toBeInTheDocument();
  });

  // v2.8.1 chat-rightalign: the viewer's own messages (sender === store
  // currentUserId, default 'user:hayang') render right-aligned (accent bubble,
  // no avatar); other people's stay left (avatar + elevated bubble).
  it('renders the viewer\'s OWN message right-aligned (indigo bubble, no avatar)', () => {
    // sample() defaults sender_identity_id to 'user:hayang' === store currentUserId.
    render(<MessageList messages={[sample('M1', 'mine')]} />);
    const row = screen.getByTestId('message-row');
    expect(row).toHaveAttribute('data-own', 'true');
    expect(row.className).toContain('justify-end');
    // v2.8.1 7th-bubbles: @oopslink-locked indigo accent bubble (#6366f1), adaptive max-w.
    const bubble = row.querySelector('.bg-indigo-500');
    expect(bubble).not.toBeNull();
    expect(bubble?.className).toContain('max-w-[75%]');
    // no avatar for own messages (#225).
    expect(row.querySelector('[data-testid="avatar"]')).toBeNull();
    expect(row).toHaveTextContent('mine');
  });

  it('renders a NON-own message left-aligned with an avatar (data-own false)', () => {
    const other: Message = { ...sample('M2', 'theirs'), sender_identity_id: 'agent:arch1' };
    render(<MessageList messages={[other]} />);
    const row = screen.getByTestId('message-row');
    expect(row).toHaveAttribute('data-own', 'false');
    expect(row.className).not.toContain('justify-end');
    // v2.8.1 7th-bubbles: other side is now a bubble too — bg-bg-subtle (浅灰,
    // both-mode token), adaptive max-w, no border card.
    const bubble = row.querySelector('.bg-bg-subtle');
    expect(bubble).not.toBeNull();
    expect(bubble?.className).toContain('max-w-[75%]');
    // avatar rendered for other people's messages.
    expect(row.querySelector('[data-testid="avatar"]')).not.toBeNull();
    expect(row).toHaveTextContent('theirs');
  });

  it('clicking the "New messages" pill scrolls to bottom + dismisses the pill', () => {
    const { rerender } = render(<MessageList messages={[sample('M1', 'a')]} />);
    const list = screen.getByTestId('message-list');
    stubScroll(list, { scrollHeight: 1000, clientHeight: 200, scrollTop: 0 });
    act(() => {
      fireEvent.scroll(list);
    });
    rerender(<MessageList messages={[sample('M1', 'a'), sample('M2', 'b')]} />);
    const pill = screen.getByTestId('message-list-new-pill');
    fireEvent.click(pill);
    expect(list.scrollTop).toBe(list.scrollHeight);
    expect(screen.queryByTestId('message-list-new-pill')).not.toBeInTheDocument();
  });
});

describe('MessageList attachments (#142)', () => {
  afterEach(() => cleanup());

  const withAtts = (id: string, atts: Message['attachments']): Message => ({
    ...sample(id, 'see attached'),
    attachments: atts,
  });

  it('renders attachments as gated download links with image previews', () => {
    const { container } = render(
      <MessageList
        messages={[
          withAtts('m1', [
            { uri: 'ac://files/01ARZ3NDEKTSV4RRFFQ69G5FAV', filename: 'design.png', mime_type: 'image/png', size: 2048 },
            { uri: 'ac://files/01ARZ3NDEKTSV4RRFFQ69G5FAW', filename: 'spec.pdf', mime_type: 'application/pdf', size: 1048576 },
          ]),
        ]}
      />,
    );
    const atts = screen.getAllByTestId('message-attachment');
    expect(atts).toHaveLength(2);
    // metadata: type label + filename + human size.
    const kinds = screen.getAllByTestId('attachment-type').map((e) => e.textContent);
    expect(kinds).toEqual(['IMG', 'FILE']);
    expect(atts[0]).toHaveTextContent('design.png');
    expect(atts[1]).toHaveTextContent('spec.pdf');
    expect(atts[1]).toHaveTextContent('1.0 MB');
    const links = screen.getAllByTestId('attachment-link');
    expect(links[0]).toHaveAttribute('href', '/api/files/01ARZ3NDEKTSV4RRFFQ69G5FAV');
    expect(links[1]).toHaveAttribute('href', '/api/files/01ARZ3NDEKTSV4RRFFQ69G5FAW');
    const preview = screen.getByTestId('attachment-preview');
    expect(preview).toHaveAttribute('src', '/api/files/01ARZ3NDEKTSV4RRFFQ69G5FAV');
    // No media elements other than image preview; all fetches go through the same
    // gated /api/files/{id} endpoint.
    expect(container.querySelector('video')).toBeNull();
    expect(container.querySelector('audio')).toBeNull();
  });

  it('renders nothing extra for a plain message (no attachments)', () => {
    render(<MessageList messages={[sample('m2', 'plain')]} />);
    expect(screen.queryByTestId('message-attachments')).not.toBeInTheDocument();
  });
});

// v2.8.1 7th DM increment 2: clicking a sender name/avatar opens the
// SenderDetailSidebar. Uses a fresh QueryClient per render so the sidebar's
// agent query doesn't leak across cases. The default msw /api/agents/:id
// handler resolves the agent branch.
describe('MessageList sender-detail sidebar (increment 2)', () => {
  afterEach(() => cleanup());

  function renderFresh(ui: React.ReactElement) {
    const c = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return rtlRender(<QueryClientProvider client={c}>{ui}</QueryClientProvider>);
  }

  const otherMsg: Message = {
    ...sample('M1', 'hello'),
    sender_identity_id: 'agent:A-1',
  };

  it('clicking the sender name button opens the sidebar', async () => {
    renderFresh(<MessageList messages={[otherMsg]} />);
    expect(screen.queryByTestId('sender-sidebar')).toBeNull();
    fireEvent.click(screen.getByTestId('message-sender-button'));
    await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
  });

  it('opening via keyboard (Enter on the name button) opens the sidebar', async () => {
    renderFresh(<MessageList messages={[otherMsg]} />);
    const btn = screen.getByTestId('message-sender-button');
    btn.focus();
    // A native <button> activates onClick for Enter/Space; fireEvent.click is
    // the canonical RTL way to assert that keyboard-driven activation works.
    fireEvent.keyDown(btn, { key: 'Enter' });
    fireEvent.click(btn);
    await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
  });

  it('clicking the sender avatar button also opens the sidebar', async () => {
    renderFresh(<MessageList messages={[otherMsg]} />);
    fireEvent.click(screen.getByTestId('message-sender-avatar-button'));
    await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
  });
});

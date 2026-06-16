import type React from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render as rtlRender, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MessageList } from './MessageList';
import { useAppStore } from '@/store/app';
import type { Message } from '@/api/types';

// T189 phase 2 — scroll-up history pagination affordance on MessageList.
const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
function render(ui: React.ReactElement) {
  return rtlRender(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const msg = (id: string): Message => ({
  id,
  conversation_id: 'C1',
  sender_identity_id: 'user:hayang',
  content_kind: 'text',
  content: id,
  direction: 'inbound',
  posted_at: '2026-05-24T01:00:00Z',
});

function stubScroll(el: HTMLElement, opts: { scrollHeight: number; clientHeight: number; scrollTop: number }) {
  Object.defineProperty(el, 'scrollHeight', { configurable: true, value: opts.scrollHeight });
  Object.defineProperty(el, 'clientHeight', { configurable: true, value: opts.clientHeight });
  Object.defineProperty(el, 'scrollTop', { configurable: true, writable: true, value: opts.scrollTop });
}

describe('MessageList — older-history pagination (T189 p2)', () => {
  beforeEach(() => useAppStore.setState({ currentUserId: 'user:hayang' }));
  afterEach(() => {
    cleanup();
    useAppStore.setState({ currentUserId: '' });
  });

  it('shows the "Load earlier" affordance only when onLoadOlder is wired and hasOlder', () => {
    const { rerender } = render(<MessageList messages={[msg('a')]} onLoadOlder={() => {}} hasOlder />);
    expect(screen.getByTestId('message-list-load-older')).toBeInTheDocument();
    // No history left → no affordance.
    rerender(
      <QueryClientProvider client={qc}>
        <MessageList messages={[msg('a')]} onLoadOlder={() => {}} hasOlder={false} />
      </QueryClientProvider>,
    );
    expect(screen.queryByTestId('message-list-older')).not.toBeInTheDocument();
  });

  it('clicking "Load earlier messages" calls onLoadOlder', () => {
    const onLoadOlder = vi.fn();
    render(<MessageList messages={[msg('a')]} onLoadOlder={onLoadOlder} hasOlder />);
    fireEvent.click(screen.getByTestId('message-list-load-older'));
    expect(onLoadOlder).toHaveBeenCalledTimes(1);
  });

  it('shows a disabled "Loading earlier…" label while a page is loading', () => {
    render(<MessageList messages={[msg('a')]} onLoadOlder={() => {}} hasOlder isLoadingOlder />);
    const btn = screen.getByTestId('message-list-load-older');
    expect(btn).toBeDisabled();
    expect(btn).toHaveTextContent('Loading earlier…');
  });

  it('scrolling near the top triggers onLoadOlder', () => {
    const onLoadOlder = vi.fn();
    render(<MessageList messages={[msg('a'), msg('b')]} onLoadOlder={onLoadOlder} hasOlder />);
    const list = screen.getByTestId('message-list');
    stubScroll(list, { scrollHeight: 1000, clientHeight: 300, scrollTop: 10 }); // near top
    fireEvent.scroll(list);
    expect(onLoadOlder).toHaveBeenCalledTimes(1);
  });

  it('does NOT trigger onLoadOlder when scrolled away from the top', () => {
    const onLoadOlder = vi.fn();
    render(<MessageList messages={[msg('a'), msg('b')]} onLoadOlder={onLoadOlder} hasOlder />);
    const list = screen.getByTestId('message-list');
    stubScroll(list, { scrollHeight: 1000, clientHeight: 300, scrollTop: 500 }); // mid
    fireEvent.scroll(list);
    expect(onLoadOlder).not.toHaveBeenCalled();
  });
});

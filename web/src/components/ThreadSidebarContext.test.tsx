import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { ThreadSidebarProvider, useThreadSidebar } from './ThreadSidebarContext';
import type { Message } from '@/api/types';

const root: Message = {
  id: 'M-root',
  conversation_id: 'C1',
  sender_identity_id: 'user:hayang',
  content_kind: 'text',
  content: 'root via provider',
  direction: 'inbound',
  posted_at: '2026-06-12T00:00:00Z',
};

function Opener() {
  const open = useThreadSidebar();
  return (
    <button type="button" data-testid="open" onClick={() => open?.(root)}>
      open
    </button>
  );
}

function NoProvider() {
  const open = useThreadSidebar();
  return <span data-testid="opener-null">{open === null ? 'null' : 'present'}</span>;
}

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe('ThreadSidebarProvider', () => {
  afterEach(() => cleanup());

  it('useThreadSidebar returns null with no provider', () => {
    wrap(<NoProvider />);
    expect(screen.getByTestId('opener-null')).toHaveTextContent('null');
  });

  it('provider opens the single ThreadSidebar with the clicked root message', async () => {
    server.use(
      http.get('/api/conversations/:id/messages/:mid/replies', () =>
        HttpResponse.json([], { status: 200 }),
      ),
    );
    wrap(
      <ThreadSidebarProvider>
        <Opener />
      </ThreadSidebarProvider>,
    );
    expect(screen.queryByTestId('thread-sidebar')).toBeNull();
    await userEvent.click(screen.getByTestId('open'));
    expect(screen.getByTestId('thread-sidebar')).toBeInTheDocument();
    expect(screen.getByText('root via provider')).toBeInTheDocument();
  });
});

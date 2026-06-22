import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { WorkItemConversation } from './WorkItemConversation';

function wrap(ownerRef: string, bannerLabel: string, ownerCode?: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <WorkItemConversation ownerRef={ownerRef} bannerLabel={bannerLabel} ownerCode={ownerCode} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const conv = {
  id: 'conv-1',
  kind: 'task',
  name: 'rebuild docs',
  status: 'active',
  owner_ref: 'pm://tasks/TS-1',
};

describe('WorkItemConversation (#137)', () => {
  afterEach(() => {
    cleanup();
    // T325: the embedded-sidebar collapse state persists in localStorage — reset
    // it so collapse in one test doesn't leak (guard the test-env stub).
    try {
      localStorage.clear?.();
      localStorage.removeItem?.('ac.convsidebar.embedded.collapsed');
    } catch {
      /* test-env storage stub */
    }
  });

  it('renders the owner banner naming the bound task even before the conversation loads', async () => {
    server.use(
      http.get('/api/conversations', () => HttpResponse.json([conv])),
      http.get('/api/conversations/conv-1/messages', () => HttpResponse.json([])),
    );
    wrap('pm://tasks/TS-1', 'rebuild docs');
    const banner = await screen.findByTestId('conversation-owner-banner');
    expect(banner).toHaveAttribute('data-owner-ref', 'pm://tasks/TS-1');
    expect(banner).toHaveTextContent('rebuild docs');
  });

  // Per @oopslink: the banner badge names the bound work item by its concrete
  // short id ("T280" / "I233") instead of the generic "Conversation" label.
  it('shows the owner short id (ownerCode) in the banner badge when provided', async () => {
    server.use(
      http.get('/api/conversations', () => HttpResponse.json([conv])),
      http.get('/api/conversations/conv-1/messages', () => HttpResponse.json([])),
    );
    wrap('pm://tasks/TS-1', 'rebuild docs', 'T280');
    const code = await screen.findByTestId('conversation-owner-code');
    expect(code).toHaveTextContent('T280');
    expect(code).not.toHaveTextContent('Conversation');
  });

  it('embeds the Participants/Threads/Files sidebar inside the chat box on desktop, collapsible (T324/T325)', async () => {
    server.use(
      http.get('/api/conversations', () => HttpResponse.json([conv])),
      http.get('/api/conversations/conv-1/messages', () => HttpResponse.json([])),
      http.get('/api/conversations/conv-1/threads', () => HttpResponse.json([])),
      http.get('/api/conversations/conv-1/files', () => HttpResponse.json([])),
    );
    wrap('pm://tasks/TS-1', 'rebuild docs', 'T280');
    // desktop (jsdom matchMedia → not mobile): the conversation sidebar is part
    // of the chat box, not the shell col④, and starts expanded.
    const sidebar = await screen.findByTestId('conv-embedded-sidebar');
    expect(sidebar).toHaveAttribute('data-collapsed', 'false');
    expect(within(sidebar).getByTestId('conversation-tab-threads')).toBeInTheDocument();
    // T325: a working collapse toggle folds it to a thin strip (tabs gone).
    fireEvent.click(within(sidebar).getByTestId('conv-embedded-sidebar-toggle'));
    const collapsed = screen.getByTestId('conv-embedded-sidebar');
    expect(collapsed).toHaveAttribute('data-collapsed', 'true');
    expect(within(collapsed).queryByTestId('conversation-tab-threads')).toBeNull();
    // expand again.
    fireEvent.click(within(collapsed).getByTestId('conv-embedded-sidebar-toggle'));
    expect(screen.getByTestId('conv-embedded-sidebar')).toHaveAttribute('data-collapsed', 'false');
  });

  it('keeps the work-item ID on mobile but hides the redundant "· linked <title>" (T312)', async () => {
    server.use(
      http.get('/api/conversations', () => HttpResponse.json([conv])),
      http.get('/api/conversations/conv-1/messages', () => HttpResponse.json([])),
    );
    wrap('pm://tasks/TS-1', 'rebuild docs', 'T280');
    // the ID (ownerCode) is visible on every breakpoint (its wrapper is NOT hidden).
    const code = await screen.findByTestId('conversation-owner-code');
    expect(code).toHaveTextContent('T280');
    const label = screen.getByTestId('conversation-owner-label');
    expect(label.className).not.toContain('hidden');
    // only the "· linked <title>" part is hidden on <md (it duplicates the header).
    const title = screen.getByTestId('conversation-owner-title');
    expect(title.className).toContain('hidden');
    expect(title.className).toContain('md:flex');
    // the Maximize action stays available on every breakpoint.
    expect(screen.getByTestId('conversation-maximize-toggle')).toBeInTheDocument();
  });

  it('falls back to the "Conversation" label when no ownerCode is provided', async () => {
    server.use(
      http.get('/api/conversations', () => HttpResponse.json([conv])),
      http.get('/api/conversations/conv-1/messages', () => HttpResponse.json([])),
    );
    wrap('pm://tasks/TS-1', 'rebuild docs');
    const code = await screen.findByTestId('conversation-owner-code');
    expect(code).toHaveTextContent('Conversation');
  });

  it('fetches the conversation BY owner_ref (org-scoped list query)', async () => {
    let seenOwnerRef: string | null = null;
    server.use(
      http.get('/api/conversations', ({ request }) => {
        seenOwnerRef = new URL(request.url).searchParams.get('owner_ref');
        return HttpResponse.json([conv]);
      }),
      http.get('/api/conversations/conv-1/messages', () => HttpResponse.json([])),
    );
    wrap('pm://tasks/TS-1', 'rebuild docs');
    await waitFor(() => expect(seenOwnerRef).toBe('pm://tasks/TS-1'));
  });

  // v2.7 #186-4: a message composer is rendered when a conversation is bound,
  // so a human can send into the task conversation (was read-only before).
  it('renders a message composer when the conversation is bound (#186-4)', async () => {
    server.use(
      http.get('/api/conversations', () => HttpResponse.json([conv])),
      http.get('/api/conversations/conv-1/messages', () => HttpResponse.json([])),
    );
    wrap('pm://tasks/TS-1', 'rebuild docs');
    // #264 P1: the bound conversation body now renders through the surface-agnostic
    // shell (task-thread surface) — so task/issue threads gain markSeen + SSE too.
    await waitFor(() => expect(screen.getByTestId('message-composer')).toBeInTheDocument());
    expect(screen.getByTestId('conversation-view')).toHaveAttribute('data-surface', 'task-thread');
  });

  // T206: the maximize toggle promotes the embedded thread to a full-viewport
  // overlay (mobile chat was cramped at the bottom of a long detail page) and
  // restores it inline. Toggling flips data-maximized + the button's aria state.
  it('maximizes and restores the conversation via the toggle (#T206)', async () => {
    server.use(
      http.get('/api/conversations', () => HttpResponse.json([conv])),
      http.get('/api/conversations/conv-1/messages', () => HttpResponse.json([])),
    );
    wrap('pm://tasks/TS-1', 'rebuild docs');
    const section = await screen.findByTestId('work-item-conversation');
    const toggle = screen.getByTestId('conversation-maximize-toggle');
    // inline by default
    expect(section).toHaveAttribute('data-maximized', 'false');
    expect(toggle).toHaveAttribute('aria-pressed', 'false');
    expect(toggle).toHaveAttribute('aria-label', 'Maximize conversation');
    // maximize → full-viewport overlay, body scroll locked
    fireEvent.click(toggle);
    expect(section).toHaveAttribute('data-maximized', 'true');
    expect(section.className).toContain('fixed');
    expect(toggle).toHaveAttribute('aria-pressed', 'true');
    expect(toggle).toHaveAttribute('aria-label', 'Restore conversation');
    expect(document.body.style.overflow).toBe('hidden');
    // restore → back inline, body scroll released
    fireEvent.click(toggle);
    expect(section).toHaveAttribute('data-maximized', 'false');
    expect(document.body.style.overflow).not.toBe('hidden');
  });

  it('restores a maximized conversation when Escape is pressed (#T206)', async () => {
    server.use(
      http.get('/api/conversations', () => HttpResponse.json([conv])),
      http.get('/api/conversations/conv-1/messages', () => HttpResponse.json([])),
    );
    wrap('pm://tasks/TS-1', 'rebuild docs');
    const section = await screen.findByTestId('work-item-conversation');
    fireEvent.click(screen.getByTestId('conversation-maximize-toggle'));
    expect(section).toHaveAttribute('data-maximized', 'true');
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(section).toHaveAttribute('data-maximized', 'false');
  });

  it('shows an empty hint (not an error) when no conversation is bound to the owner_ref', async () => {
    server.use(http.get('/api/conversations', () => HttpResponse.json([])));
    wrap('pm://tasks/NONE', 'orphan task');
    await waitFor(() =>
      expect(screen.getByTestId('conversation-empty')).toHaveTextContent(/No linked conversation/),
    );
  });

  // v2.7.1 #219: flat chronological stream (no segment grouping headers); a
  // message carrying a work_item_ref shows a per-message "Work item" tag
  // (raw ref on hover), a message without one shows no tag.
  it('renders flat messages with a per-message work-item tag, no grouping headers', async () => {
    server.use(
      http.get('/api/conversations', () => HttpResponse.json([conv])),
      http.get('/api/conversations/conv-1/messages', () =>
        HttpResponse.json([
          { id: '1', conversation_id: 'conv-1', sender_identity_id: 'user:hayang', content_kind: 'text', content: 'kickoff', direction: 'inbound', posted_at: '2026-05-30T01:00:00Z' },
          { id: '2', conversation_id: 'conv-1', sender_identity_id: 'agent:builder', content_kind: 'text', content: 'on it', direction: 'internal', posted_at: '2026-05-30T02:00:00Z', context_refs: { work_item_ref: 'agent://WI-1', task_ref: 'pm://tasks/TS-1', agent_ref: 'agent:builder' } },
        ]),
      ),
    );
    wrap('pm://tasks/TS-1', 'rebuild docs');
    await waitFor(() => expect(screen.getAllByTestId('message-row')).toHaveLength(2));
    // No grouping segment headers (implementation detail removed) + no "Unassociated…".
    expect(screen.queryByTestId('message-segment')).not.toBeInTheDocument();
    expect(screen.queryByText(/Unassociated work item/)).not.toBeInTheDocument();
    // Exactly one message carries a work-item tag; raw ref only on hover.
    const tags = screen.getAllByTestId('message-workitem-tag');
    expect(tags).toHaveLength(1);
    expect(tags[0]).toHaveAttribute('data-work-item-ref', 'agent://WI-1');
    expect(tags[0]).toHaveTextContent('Work item');
    expect(tags[0]).not.toHaveTextContent('agent://WI-1');
    expect(tags[0]).toHaveAttribute('title', 'agent://WI-1');
  });
});

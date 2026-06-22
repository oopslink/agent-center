// I23 (T332) — cross-source "未读会话" digest section.
// Mockup: mockup-conversations-reachability (col② top region).
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { UnreadConversationsSection } from './UnreadConversationsSection';
import type { UnreadConversationRow } from '@/api/types';

const ORG_BASE = '/organizations/acme';

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/']}>
        <UnreadConversationsSection orgBase={ORG_BASE} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function mockDigest(rows: UnreadConversationRow[]) {
  server.use(http.get('/api/unread-conversations', () => HttpResponse.json(rows)));
}

const TASK_MENTION: UnreadConversationRow = {
  conversation_id: 'tc1',
  source_type: 'task',
  source_ref: 'pm://tasks/t1',
  source_id: 't1',
  project_id: 'p1',
  title: 'My churn fix',
  last_message_preview: 'can you confirm',
  last_message_sender: 'oopslink',
  updated_at: '2026-06-22T09:24:00Z',
  unread_count: 2,
  mention_count: 1,
  route: '/projects/p1/tasks/t1',
};
const CHANNEL_UNREAD: UnreadConversationRow = {
  conversation_id: 'c1',
  source_type: 'channel',
  source_ref: 'id://organizations/o1',
  source_id: 'c1',
  title: 'research-room',
  last_message_preview: 'F2 已 cut',
  last_message_sender: 'dev1',
  updated_at: '2026-06-22T08:55:00Z',
  unread_count: 3,
  mention_count: 0,
  route: '/channels/c1',
};
const DM_SINGLE: UnreadConversationRow = {
  conversation_id: 'd1',
  source_type: 'dm',
  source_ref: '',
  source_id: 'd1',
  title: '@oopslink',
  last_message_preview: 'local repro works',
  last_message_sender: 'oopslink',
  updated_at: '2026-06-22T08:00:00Z',
  unread_count: 1,
  mention_count: 0,
  route: '/dms/d1',
};

afterEach(() => {
  cleanup();
  // T334: the collapse state persists in localStorage — reset between tests.
  try {
    localStorage.clear?.();
    localStorage.removeItem?.('ac.unreadconv.collapsed');
  } catch {
    /* test-env stub */
  }
});

describe('UnreadConversationsSection (I23 / T332)', () => {
  beforeEach(() => mockDigest([TASK_MENTION, CHANNEL_UNREAD, DM_SINGLE]));

  it('renders one row per unread source, linking to the orgBase-prefixed route', async () => {
    renderSection();
    expect(await screen.findByTestId('unread-conversations-section')).toBeInTheDocument();
    const rows = screen.getAllByTestId('unread-conv-row');
    expect(rows).toHaveLength(3);

    const bySource = (s: string) =>
      rows.find((row) => row.getAttribute('data-source-type') === s) as HTMLElement;
    expect(bySource('task')).toHaveAttribute('href', `${ORG_BASE}/projects/p1/tasks/t1`);
    expect(bySource('channel')).toHaveAttribute('href', `${ORG_BASE}/channels/c1`);
    expect(bySource('dm')).toHaveAttribute('href', `${ORG_BASE}/dms/d1`);
  });

  it('colors a source tag per family and shows the source label', async () => {
    renderSection();
    await screen.findByTestId('unread-conversations-section');
    const tags = screen.getAllByTestId('unread-conv-source-tag').map((t) => t.textContent);
    expect(tags).toContain('Task');
    expect(tags).toContain('Channel');
    expect(tags).toContain('DM');
  });

  it('marks an @me row with the "Mentions you" label + a brand @N mention badge', async () => {
    renderSection();
    await screen.findByTestId('unread-conversations-section');
    const taskRow = screen.getByRole('link', { name: /My churn fix/ });
    expect(taskRow).toHaveAttribute('data-mention', 'true');
    expect(within(taskRow).getByTestId('unread-conv-mention-label')).toHaveTextContent(/mentions you/i);
    expect(within(taskRow).getByTestId('unread-conv-mention-badge')).toHaveTextContent('@1');
  });

  it('shows English labels + a Mark-all-read action that POSTs (T334)', async () => {
    let marked = false;
    server.use(
      http.post('/api/unread-conversations/mark-all-read', () => {
        marked = true;
        return HttpResponse.json({ marked: 3 });
      }),
    );
    renderSection();
    const section = await screen.findByTestId('unread-conversations-section');
    // English header + filter chips.
    expect(within(section).getByTestId('unread-collapse-toggle')).toHaveTextContent(/Unread/);
    expect(screen.getByTestId('unread-filter-all')).toHaveTextContent(/All/);
    expect(screen.getByTestId('unread-filter-mentions')).toHaveTextContent(/@me/);
    // Mark-all-read button fires the bulk endpoint.
    fireEvent.click(screen.getByTestId('unread-mark-all-read'));
    await waitFor(() => expect(marked).toBe(true));
  });

  it('shows a neutral count badge for multi-unread and a dot for a single unread', async () => {
    renderSection();
    await screen.findByTestId('unread-conversations-section');
    const rows = screen.getAllByTestId('unread-conv-row');
    const bySource = (s: string) =>
      rows.find((row) => row.getAttribute('data-source-type') === s) as HTMLElement;
    expect(within(bySource('channel')).getByTestId('unread-conv-count-badge')).toHaveTextContent('3');
    expect(within(bySource('dm')).getByTestId('unread-conv-dot')).toBeInTheDocument();
  });

  it('filters to @me / unread instantly via the top chips', async () => {
    renderSection();
    await screen.findByTestId('unread-conversations-section');
    // chip counts.
    expect(screen.getByTestId('unread-filter-all')).toHaveTextContent('3');
    expect(screen.getByTestId('unread-filter-mentions')).toHaveTextContent('1');
    expect(screen.getByTestId('unread-filter-unread')).toHaveTextContent('2');

    // @我 → only the mention row.
    fireEvent.click(screen.getByTestId('unread-filter-mentions'));
    expect(screen.getAllByTestId('unread-conv-row')).toHaveLength(1);
    expect(screen.getByRole('link', { name: /My churn fix/ })).toBeInTheDocument();

    // 未读 → only the non-mention rows.
    fireEvent.click(screen.getByTestId('unread-filter-unread'));
    const unreadRows = screen.getAllByTestId('unread-conv-row');
    expect(unreadRows).toHaveLength(2);
    expect(screen.queryByRole('link', { name: /My churn fix/ })).not.toBeInTheDocument();
  });

  it('collapses and expands the digest via the header (T334)', async () => {
    renderSection();
    const toggle = await screen.findByTestId('unread-collapse-toggle');
    // expanded by default → rows + filters shown.
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getAllByTestId('unread-conv-row').length).toBeGreaterThan(0);
    expect(screen.getByTestId('unread-filter-all')).toBeInTheDocument();
    // collapse → rows + filters hidden; header (with count) stays.
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByTestId('unread-conv-row')).toBeNull();
    expect(screen.queryByTestId('unread-filter-all')).toBeNull();
    // expand again.
    fireEvent.click(toggle);
    expect(screen.getAllByTestId('unread-conv-row').length).toBeGreaterThan(0);
  });

  it('renders nothing when there is no unread (dynamic region)', async () => {
    mockDigest([]);
    renderSection();
    // Give the query a tick; the section must stay absent.
    await waitFor(() => {
      expect(screen.queryByTestId('unread-conversations-section')).not.toBeInTheDocument();
    });
  });
});

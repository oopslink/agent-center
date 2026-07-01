// AttentionPanel (v2.26.0 I61) — the reusable "Needs your attention" popout used
// by both the desktop rail and the mobile top bar. These render-level tests pin
// the two-kind presentation: task items name the block flavour and carry NO
// dismiss; mention items name the directed-signal flavour and DO carry a dismiss.
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { AttentionPanel } from './AttentionPanel';
import type { AttentionItem } from '@/api/attention';

function renderPanel(items: AttentionItem[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AttentionPanel items={items} orgBase="/organizations/acme" onClose={vi.fn()} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const TASK: AttentionItem = {
  kind: 'task', severity: 'warning', ref: 'task-1', task_id: 'task-1', reason_type: 'obstacle',
  title: 'Deploy blocked', snippet: 'infra key missing', actor: '', conversation_id: '',
  project_id: 'p1', project_name: 'Alpha', org_ref: 'T9', ts: '2026-06-30T00:00:00Z',
  route: '/projects/p1/tasks/task-1',
};
const MENTION: AttentionItem = {
  kind: 'mention', severity: 'warning', ref: 'c-1', conversation_id: 'c-1', conversation_kind: 'dm',
  title: 'ops', snippet: 'deploy is wedged', actor: 'agent:AG1', mention_count: 2, unread_count: 2,
  message_id: 'm-7', ts: '2026-06-30T01:00:00Z', route: '/dms/c-1',
};

describe('AttentionPanel', () => {
  afterEach(() => cleanup());

  it('shows the empty state when there are no items', () => {
    renderPanel([]);
    expect(screen.getByTestId('rail-alerts-empty')).toBeInTheDocument();
  });

  it('renders a task item: block-flavour badge, deep-link, and NO dismiss', () => {
    renderPanel([TASK]);
    const item = screen.getByTestId('rail-alert-item');
    expect(item).toHaveAttribute('data-kind', 'task');
    expect(item).toHaveAttribute('href', '/organizations/acme/projects/p1/tasks/task-1');
    expect(within(item).getByText('Needs intervention')).toBeInTheDocument();
    expect(screen.queryByTestId('rail-alert-dismiss')).not.toBeInTheDocument();
  });

  it('renders a mention item: directed-signal badge, deep-link, @count, and a dismiss', () => {
    renderPanel([MENTION]);
    const item = screen.getByTestId('rail-alert-item');
    expect(item).toHaveAttribute('data-kind', 'mention');
    expect(item).toHaveAttribute('href', '/organizations/acme/dms/c-1');
    expect(within(item).getByText('Direct message')).toBeInTheDocument();
    expect(screen.getByTestId('rail-alert-mention-count')).toHaveTextContent('@2');
    expect(screen.getByTestId('rail-alert-dismiss')).toBeInTheDocument();
  });
});

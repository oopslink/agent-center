import type React from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { OrgContext } from '@/OrgContext';
import { ReminderDetailModal } from './ReminderDetailModal';

// T286 — the reminder DETAIL modal linkifies ids in its TEXT: the Content is rendered
// through MarkdownMessage so task/plan/issue/@mention refs become links (the same path
// chat uses), and the Target agent id becomes a clickable ref. Mirrors
// MentionText.test.tsx's msw-backed resolver harness.

function renderModal(ui: React.ReactElement, slug = 'test-org') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OrgContext.Provider value={{ slug, orgId: 'O', orgName: 'Test Org' }}>{ui}</OrgContext.Provider>
    </QueryClientProvider>,
  );
}

// The detail fetch (/reminders/:id) + the resolver list endpoints the linkifier loads.
function mockReminderWithRefs(content: string) {
  server.use(
    http.get('/api/members', () => HttpResponse.json([])),
    http.get('/api/plans', () => HttpResponse.json({ items: [], total: 0 })),
    http.get('/api/issues', () => HttpResponse.json({ items: [], total: 0 })),
    http.get('/api/tasks', () =>
      HttpResponse.json({
        items: [
          {
            id: 'task-abc',
            org_ref: 'T123',
            project: { id: 'proj-x', name: 'Project X' },
            title: 'Wire the thing',
            status: 'open',
            assignee: null,
            updated_at: 'x',
            created_at: 'x',
          },
        ],
        total: 1,
      }),
    ),
    http.get('/api/reminders/rmd-1', () =>
      HttpResponse.json({
        id: 'rmd-1',
        organization_id: 'O',
        project_id: 'proj-x',
        creator_ref: 'user:pd',
        remindee_agent_id: 'dev1',
        content,
        status: 'active',
        skip_if_overlap: true,
        deliver_as_creator: true,
        fired_count: 0,
        version: 1,
        schedule: { kind: 'once', once_at: '2099-01-01T17:00:00Z' },
        next_run_at: null,
        last_fired_at: null,
        created_at: 'x',
        updated_at: 'x',
        firings: [],
      }),
    ),
  );
}

describe('ReminderDetailModal id linkification (T286)', () => {
  afterEach(() => cleanup());

  it('linkifies a task-<id> in the reminder Content as a T123 link', async () => {
    mockReminderWithRefs('please follow up on task-abc before EOD');
    renderModal(<ReminderDetailModal slug="test-org" reminderId="rmd-1" onClose={vi.fn()} />);

    const link = await screen.findByTestId('task-ref-token');
    expect(link.tagName).toBe('A');
    expect(link).toHaveTextContent('T123');
    expect(link).not.toHaveTextContent('task-abc');
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/tasks/task-abc');
  });

  it('renders the Target agent id as a clickable ref link', async () => {
    mockReminderWithRefs('nothing to link here');
    renderModal(<ReminderDetailModal slug="test-org" reminderId="rmd-1" onClose={vi.fn()} />);

    const target = await screen.findByTestId('reminder-target-link');
    expect(target.tagName).toBe('BUTTON');
    // displayName falls back to the raw ref when no member resolves — still a ref token.
    expect(target).toHaveTextContent(/dev1/);
  });
});

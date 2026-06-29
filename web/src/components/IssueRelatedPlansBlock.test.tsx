// The issue detail's "Related Plans" panel: the plans derived from this issue (the
// plan-dimension mirror of Derived Tasks). Self-fetching via useRelatedPlansForIssue.
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { IssueRelatedPlansBlock } from './IssueDetailSidebar';

function plan(over: Record<string, unknown> = {}) {
  return {
    id: 'plan-A',
    project_id: 'proj-1',
    name: 'Some plan',
    description: '',
    status: 'running',
    org_ref: 'P12',
    creator_ref: 'user:x',
    conversation_id: 'c1',
    has_failed: false,
    progress: { done: 1, total: 3 },
    created_at: '2026-06-01T00:00:00Z',
    ...over,
  };
}

function renderBlock(issueId = 'issue-1', projectId = 'proj-1') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <IssueRelatedPlansBlock projectId={projectId} issueId={issueId} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('IssueRelatedPlansBlock', () => {
  afterEach(() => cleanup());

  describe('with derived plans', () => {
    beforeEach(() => {
      server.use(
        http.get('/api/projects/:pid/issues/:issueId/related-plans', () =>
          HttpResponse.json({
            plans: [
              plan({ id: 'plan-A', name: 'Alpha plan', org_ref: 'P12', status: 'running' }),
              plan({ id: 'plan-B', name: 'Beta plan', org_ref: 'P14', status: 'done' }),
            ],
          }),
        ),
      );
    });

    it('lists the derived plans with ref, name, status, and a link', async () => {
      renderBlock('issue-1', 'proj-1');
      await waitFor(() => expect(screen.getByTestId('related-plans-list')).toBeInTheDocument());

      const items = screen.getAllByTestId('related-plan-item');
      expect(items).toHaveLength(2);
      expect(items.map((el) => el.getAttribute('data-plan-id'))).toEqual(['plan-A', 'plan-B']);

      const alpha = items[0];
      expect(within(alpha).getByText('P12')).toBeInTheDocument();
      expect(within(alpha).getByText('Alpha plan')).toBeInTheDocument();
      expect(alpha).toHaveAttribute('href', '/projects/proj-1/plans/plan-A');
    });
  });

  it('shows an empty placeholder when no plan is derived from the issue', async () => {
    server.use(
      http.get('/api/projects/:pid/issues/:issueId/related-plans', () =>
        HttpResponse.json({ plans: [] }),
      ),
    );
    renderBlock('issue-1', 'proj-1');
    await waitFor(() => expect(screen.getByTestId('related-plans-empty')).toBeInTheDocument());
    expect(screen.queryByTestId('related-plans-list')).not.toBeInTheDocument();
  });

  it('surfaces a load error', async () => {
    server.use(
      http.get('/api/projects/:pid/issues/:issueId/related-plans', () =>
        HttpResponse.json({ error: 'boom' }, { status: 500 }),
      ),
    );
    renderBlock('issue-1', 'proj-1');
    await waitFor(() => expect(screen.getByTestId('related-plans-error')).toBeInTheDocument());
  });
});

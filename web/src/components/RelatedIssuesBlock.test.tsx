// The plan detail rail's "Related Issues" list: the source issue(s) this plan's tasks
// derive from. Issue-side mirror of the issue sidebar's Derived Tasks list.
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { RelatedIssuesBlock } from './RelatedIssuesBlock';

function issue(over: Record<string, unknown> = {}) {
  return {
    id: 'issue-A',
    project_id: 'proj-1',
    title: 'Some issue',
    description: '',
    status: 'open',
    org_ref: 'I12',
    created_by: 'user:x',
    version: 1,
    tags: [],
    created_at: '2026-06-01T00:00:00Z',
    updated_at: '2026-06-01T00:00:00Z',
    ...over,
  };
}

function renderBlock(currentPlanId = 'plan-cur', projectId = 'proj-1') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <RelatedIssuesBlock projectId={projectId} currentPlanId={currentPlanId} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('RelatedIssuesBlock', () => {
  afterEach(() => cleanup());

  describe('with related issues', () => {
    beforeEach(() => {
      server.use(
        http.get('/api/projects/:pid/plans/:planId/related-issues', () =>
          HttpResponse.json({
            issues: [
              issue({ id: 'issue-A', title: 'Alpha issue', org_ref: 'I12', status: 'open' }),
              issue({ id: 'issue-B', title: 'Beta issue', org_ref: 'I14', status: 'closed' }),
            ],
          }),
        ),
      );
    });

    it('lists the related issues with ref, title, status, and a link', async () => {
      renderBlock('plan-cur', 'proj-1');
      await waitFor(() => expect(screen.getByTestId('related-issues-list')).toBeInTheDocument());

      const items = screen.getAllByTestId('related-issue-item');
      expect(items).toHaveLength(2);

      const ids = items.map((el) => el.getAttribute('data-issue-id'));
      expect(ids).toEqual(['issue-A', 'issue-B']);

      const alpha = items[0];
      expect(within(alpha).getByText('I12')).toBeInTheDocument();
      expect(within(alpha).getByText('Alpha issue')).toBeInTheDocument();
      expect(alpha).toHaveAttribute('href', '/projects/proj-1/issues/issue-A');
    });
  });

  it('shows an empty placeholder when the plan has no source issue', async () => {
    server.use(
      http.get('/api/projects/:pid/plans/:planId/related-issues', () =>
        HttpResponse.json({ issues: [] }),
      ),
    );
    renderBlock('plan-cur', 'proj-1');
    await waitFor(() => expect(screen.getByTestId('related-issues-empty')).toBeInTheDocument());
    expect(screen.queryByTestId('related-issues-list')).not.toBeInTheDocument();
  });

  it('surfaces a load error', async () => {
    server.use(
      http.get('/api/projects/:pid/plans/:planId/related-issues', () =>
        HttpResponse.json({ error: 'boom' }, { status: 500 }),
      ),
    );
    renderBlock('plan-cur', 'proj-1');
    await waitFor(() => expect(screen.getByTestId('related-issues-error')).toBeInTheDocument());
  });
});

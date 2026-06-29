// T581 — the plan detail rail's "Related Plans" list: the OTHER plans derived from the
// same source issue (the backend endpoint already excludes the current plan + the
// built-in pool). Mirrors the issue sidebar's Derived Tasks list.
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { RelatedPlansBlock } from './RelatedPlansBlock';

function plan(over: Record<string, unknown> = {}) {
  return {
    id: 'plan-A',
    project_id: 'proj-1',
    name: 'Other plan',
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

function renderBlock(currentPlanId = 'plan-cur', projectId = 'proj-1') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <RelatedPlansBlock projectId={projectId} currentPlanId={currentPlanId} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('RelatedPlansBlock', () => {
  afterEach(() => cleanup());

  describe('with related plans', () => {
    beforeEach(() => {
      // The backend endpoint returns ONLY the related plans (current plan + builtin
      // pool already excluded server-side), so the component renders them as-is.
      server.use(
        http.get('/api/projects/:pid/plans/:planId/related-plans', () =>
          HttpResponse.json({
            plans: [
              plan({ id: 'plan-A', name: 'Alpha plan', org_ref: 'P12', status: 'running' }),
              plan({ id: 'plan-B', name: 'Beta plan', org_ref: 'P14', status: 'done' }),
            ],
          }),
        ),
      );
    });

    it('lists the related plans with ref, name, status, and a link', async () => {
      renderBlock('plan-cur', 'proj-1');
      await waitFor(() => expect(screen.getByTestId('related-plans-list')).toBeInTheDocument());

      const items = screen.getAllByTestId('related-plan-item');
      expect(items).toHaveLength(2);

      const ids = items.map((el) => el.getAttribute('data-plan-id'));
      expect(ids).toEqual(['plan-A', 'plan-B']);
      expect(ids).not.toContain('plan-cur');

      const alpha = items[0];
      expect(within(alpha).getByText('P12')).toBeInTheDocument();
      expect(within(alpha).getByText('Alpha plan')).toBeInTheDocument();
      expect(alpha).toHaveAttribute('href', '/projects/proj-1/plans/plan-A');
    });
  });

  it('shows an empty placeholder when the issue has no other plans', async () => {
    server.use(
      http.get('/api/projects/:pid/plans/:planId/related-plans', () =>
        HttpResponse.json({ plans: [] }),
      ),
    );
    renderBlock('plan-cur', 'proj-1');
    await waitFor(() => expect(screen.getByTestId('related-plans-empty')).toBeInTheDocument());
    expect(screen.queryByTestId('related-plans-list')).not.toBeInTheDocument();
  });

  it('surfaces a load error', async () => {
    server.use(
      http.get('/api/projects/:pid/plans/:planId/related-plans', () =>
        HttpResponse.json({ error: 'boom' }, { status: 500 }),
      ),
    );
    renderBlock('plan-cur', 'proj-1');
    await waitFor(() => expect(screen.getByTestId('related-plans-error')).toBeInTheDocument());
  });
});

// T581 — the plan detail rail's "Related Plans" list: the OTHER structured plans in
// the same project (excludes the current plan; the list endpoint already excludes the
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

  describe('with sibling plans', () => {
    beforeEach(() => {
      server.use(
        http.get('/api/projects/:pid/plans', () =>
          HttpResponse.json({
            plans: [
              plan({ id: 'plan-A', name: 'Alpha plan', org_ref: 'P12', status: 'running' }),
              plan({ id: 'plan-cur', name: 'Current plan', org_ref: 'P13', status: 'draft' }),
              plan({ id: 'plan-B', name: 'Beta plan', org_ref: 'P14', status: 'done' }),
            ],
            total: 3,
          }),
        ),
      );
    });

    it('lists the other plans (excluding the current one) with ref, name, status, and a link', async () => {
      renderBlock('plan-cur', 'proj-1');
      await waitFor(() => expect(screen.getByTestId('related-plans-list')).toBeInTheDocument());

      const items = screen.getAllByTestId('related-plan-item');
      expect(items).toHaveLength(2); // current plan excluded

      const ids = items.map((el) => el.getAttribute('data-plan-id'));
      expect(ids).toEqual(['plan-A', 'plan-B']);
      expect(ids).not.toContain('plan-cur');

      const alpha = items[0];
      expect(within(alpha).getByText('P12')).toBeInTheDocument();
      expect(within(alpha).getByText('Alpha plan')).toBeInTheDocument();
      expect(alpha).toHaveAttribute('href', '/projects/proj-1/plans/plan-A');
    });
  });

  it('shows an empty placeholder when the project has no other plans', async () => {
    server.use(
      http.get('/api/projects/:pid/plans', () =>
        HttpResponse.json({ plans: [plan({ id: 'plan-cur', name: 'Only plan' })], total: 1 }),
      ),
    );
    renderBlock('plan-cur', 'proj-1');
    await waitFor(() => expect(screen.getByTestId('related-plans-empty')).toBeInTheDocument());
    expect(screen.queryByTestId('related-plans-list')).not.toBeInTheDocument();
  });

  it('surfaces a load error', async () => {
    server.use(
      http.get('/api/projects/:pid/plans', () => HttpResponse.json({ error: 'boom' }, { status: 500 })),
    );
    renderBlock('plan-cur', 'proj-1');
    await waitFor(() => expect(screen.getByTestId('related-plans-error')).toBeInTheDocument());
  });
});

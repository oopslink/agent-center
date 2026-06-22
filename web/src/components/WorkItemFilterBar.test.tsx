import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import { WorkItemFilterBar, EMPTY_DATE_RANGE } from './WorkItemFilterBar';

// T339 — the filter panel is a collapsible disclosure (default open on desktop /
// collapsed on mobile; jsdom matchMedia is absent → desktop → open), with an
// active-filter count badge + Clear in the always-visible header.
afterEach(() => {
  cleanup();
  try {
    localStorage.clear?.();
    localStorage.removeItem?.('ac.workitemfilter.open');
  } catch {
    /* test-env stub */
  }
});

function renderBar(over: Record<string, unknown> = {}) {
  server.use(
    http.get('/api/projects', () => HttpResponse.json([])),
    http.get('/api/members', () => HttpResponse.json([])),
  );
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const props = {
    kind: 'issue' as const,
    selectedStatuses: [] as string[],
    onStatusesChange: () => {},
    selectedProjects: [] as string[],
    onProjectsChange: () => {},
    assignee: '',
    onAssigneeChange: () => {},
    dateRange: EMPTY_DATE_RANGE,
    onDateRangeChange: () => {},
    ...over,
  };
  return render(
    <QueryClientProvider client={qc}>
      <WorkItemFilterBar {...props} />
    </QueryClientProvider>,
  );
}

describe('WorkItemFilterBar collapse (T339)', () => {
  it('is open on desktop by default and toggles the body closed (Clear stays)', () => {
    renderBar();
    const toggle = screen.getByTestId('org-filter-toggle');
    expect(toggle).toHaveAttribute('aria-expanded', 'true'); // jsdom = desktop
    expect(screen.getByTestId('org-filter-status-open')).toBeInTheDocument();
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByTestId('org-filter-status-open')).toBeNull();
    // Clear + the toggle stay in the header even when collapsed.
    expect(screen.getByTestId('org-filter-clear')).toBeInTheDocument();
  });

  it('shows an active-filter count badge in the header', () => {
    renderBar({ selectedStatuses: ['open', 'closed'], assignee: 'agent:agent-1' });
    // 2 statuses + 1 assignee = 3 active filter values.
    expect(screen.getByTestId('org-filter-active-count')).toHaveTextContent('3');
  });

  it('shows no count badge when no filters are active', () => {
    renderBar();
    expect(screen.queryByTestId('org-filter-active-count')).toBeNull();
  });
});

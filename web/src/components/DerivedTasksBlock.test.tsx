import type React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { DerivedTasksBlock } from './IssueDetailSidebar';

// T191: the issue detail sidebar's "Derived Tasks" block — the tasks created
// with derived_from_issue == this issue, reverse-looked-up client-side over the
// project task list. Each row links to the task with its T-number + status.
function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

const task = (extra: Record<string, unknown> = {}) => ({
  id: 'task-x', project_id: 'proj-1', title: 'A task', description: '',
  status: 'open', version: 1, created_at: 'x', updated_at: 'x', ...extra,
});

afterEach(() => cleanup());

describe('DerivedTasksBlock (T191)', () => {
  it('lists ONLY the tasks derived from this issue, with T-number + title + status + link', async () => {
    server.use(
      http.get('*/projects/proj-1/tasks', () =>
        HttpResponse.json({
          tasks: [
            task({ id: 'task-a', org_ref: 'T10', title: 'Wire the endpoint', status: 'open', derived_from_issue: 'issue-1' }),
            task({ id: 'task-b', org_ref: 'T11', title: 'Fix the panel', status: 'completed', derived_from_issue: 'issue-1' }),
            // derived from a DIFFERENT issue → excluded
            task({ id: 'task-c', org_ref: 'T12', title: 'Other', status: 'open', derived_from_issue: 'issue-2' }),
            // not derived from any issue → excluded
            task({ id: 'task-d', org_ref: 'T13', title: 'Standalone', status: 'running' }),
          ],
        }),
      ),
    );
    wrap(<DerivedTasksBlock projectId="proj-1" issueId="issue-1" />);
    const list = await screen.findByTestId('derived-tasks-list');
    const items = within(list).getAllByTestId('derived-task-item');
    expect(items).toHaveLength(2);
    // T-numbers of the two derived tasks; the non-derived ones are absent.
    expect(list).toHaveTextContent('T10');
    expect(list).toHaveTextContent('Wire the endpoint');
    expect(list).toHaveTextContent('T11');
    expect(within(list).queryByText('T12')).toBeNull();
    expect(within(list).queryByText('Standalone')).toBeNull();
    // each row links to its task detail page; status chip present.
    expect(items[0]).toHaveAttribute('href', '/projects/proj-1/tasks/task-a');
    expect(within(items[0]).getByTestId('status-chip')).toHaveAttribute('data-status', 'open');
    expect(within(items[1]).getByTestId('status-chip')).toHaveAttribute('data-status', 'completed');
  });

  it('shows a placeholder when no task is derived from the issue', async () => {
    server.use(
      http.get('*/projects/proj-1/tasks', () =>
        HttpResponse.json({ tasks: [task({ id: 'task-d', derived_from_issue: 'issue-other' })] }),
      ),
    );
    wrap(<DerivedTasksBlock projectId="proj-1" issueId="issue-1" />);
    await waitFor(() => expect(screen.getByTestId('derived-tasks-empty')).toBeInTheDocument());
    expect(screen.queryByTestId('derived-tasks-list')).toBeNull();
  });
});

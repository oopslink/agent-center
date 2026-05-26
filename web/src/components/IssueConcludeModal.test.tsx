import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import { IssueConcludeModal } from './IssueConcludeModal';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe('IssueConcludeModal', () => {
  afterEach(() => cleanup());

  it('renders three resolution kind options + summary field', () => {
    wrap(<IssueConcludeModal issueId="I-1" onClose={() => undefined} />);
    expect(screen.getByTestId('issue-conclude-modal')).toBeInTheDocument();
    expect(screen.getByTestId('issue-conclude-kind-closed_no_action')).toBeInTheDocument();
    expect(screen.getByTestId('issue-conclude-kind-closed_with_tasks')).toBeInTheDocument();
    expect(screen.getByTestId('issue-conclude-kind-withdrawn')).toBeInTheDocument();
    expect(screen.getByTestId('issue-conclude-summary')).toBeInTheDocument();
  });

  it('hides task list by default (no_action selected)', () => {
    wrap(<IssueConcludeModal issueId="I-1" onClose={() => undefined} />);
    expect(screen.queryByTestId('issue-conclude-tasks')).not.toBeInTheDocument();
  });

  it('shows task list when closed_with_tasks selected; add/remove row works', () => {
    wrap(<IssueConcludeModal issueId="I-1" onClose={() => undefined} />);
    fireEvent.click(screen.getByTestId('issue-conclude-kind-closed_with_tasks'));
    expect(screen.getByTestId('issue-conclude-tasks')).toBeInTheDocument();
    expect(screen.getAllByTestId('issue-conclude-task-row')).toHaveLength(1);
    fireEvent.click(screen.getByTestId('issue-conclude-task-add'));
    expect(screen.getAllByTestId('issue-conclude-task-row')).toHaveLength(2);
    fireEvent.click(screen.getAllByTestId('issue-conclude-task-remove')[0]);
    expect(screen.getAllByTestId('issue-conclude-task-row')).toHaveLength(1);
  });

  it('submit disabled until summary present', () => {
    wrap(<IssueConcludeModal issueId="I-1" onClose={() => undefined} />);
    const submit = screen.getByTestId('issue-conclude-submit') as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    fireEvent.change(screen.getByTestId('issue-conclude-summary'), {
      target: { value: 'because reasons' },
    });
    expect(submit.disabled).toBe(false);
  });

  it('POSTs /api/issues/{id}/conclude with the selected kind + summary', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/issues/I-1/conclude', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ issue_id: 'I-1', task_ids: [], event_ids: ['E-1'] });
      }),
    );
    const onClose = vi.fn();
    wrap(<IssueConcludeModal issueId="I-1" onClose={onClose} />);
    fireEvent.change(screen.getByTestId('issue-conclude-summary'), {
      target: { value: 'not doing this' },
    });
    fireEvent.click(screen.getByTestId('issue-conclude-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(received).toMatchObject({
      kind: 'closed_no_action',
      summary: 'not doing this',
    });
  });

  it('POSTs with task list when closed_with_tasks selected', async () => {
    let received: { tasks?: Array<{ title?: string }> } | undefined;
    server.use(
      http.post('/api/issues/I-1/conclude', async ({ request }) => {
        received = (await request.json()) as typeof received;
        return HttpResponse.json({ issue_id: 'I-1', task_ids: ['T-1'], event_ids: ['E-1', 'E-2'] });
      }),
    );
    wrap(<IssueConcludeModal issueId="I-1" onClose={() => undefined} />);
    fireEvent.click(screen.getByTestId('issue-conclude-kind-closed_with_tasks'));
    fireEvent.change(screen.getByTestId('issue-conclude-summary'), {
      target: { value: 'spawning tasks' },
    });
    const titleInput = screen.getByTestId('issue-conclude-task-title');
    fireEvent.change(titleInput, { target: { value: 'fix the bug' } });
    fireEvent.click(screen.getByTestId('issue-conclude-submit'));
    await waitFor(() => expect(received).toBeDefined());
    expect(received?.tasks).toHaveLength(1);
    expect(received?.tasks?.[0]?.title).toBe('fix the bug');
  });
});

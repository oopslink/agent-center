import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import { IssueEditModal } from './IssueEditModal';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const baseIssue = {
  id: 'I-1',
  title: 'old title',
  description: 'old description',
} as const;

describe('IssueEditModal', () => {
  afterEach(() => cleanup());

  it('renders prefilled fields from the issue prop', () => {
    wrap(<IssueEditModal issue={baseIssue} onClose={() => undefined} />);
    expect((screen.getByTestId('issue-edit-title') as HTMLInputElement).value).toBe('old title');
    expect((screen.getByTestId('issue-edit-description') as HTMLTextAreaElement).value).toBe(
      'old description',
    );
  });

  it('disables submit when title cleared', () => {
    wrap(<IssueEditModal issue={baseIssue} onClose={() => undefined} />);
    fireEvent.change(screen.getByTestId('issue-edit-title'), { target: { value: '' } });
    const submit = screen.getByTestId('issue-edit-submit') as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
  });

  it('PATCHes /api/issues/{id} with edited fields + calls onClose', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.patch('/api/issues/I-1', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ issue_id: 'I-1', event_id: 'E-1' });
      }),
    );
    const onClose = vi.fn();
    wrap(<IssueEditModal issue={baseIssue} onClose={onClose} />);
    fireEvent.change(screen.getByTestId('issue-edit-title'), {
      target: { value: 'new title' },
    });
    fireEvent.click(screen.getByTestId('issue-edit-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(received).toMatchObject({ title: 'new title' });
  });
});

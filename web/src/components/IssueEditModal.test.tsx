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
    wrap(<IssueEditModal projectId="proj-a" issue={baseIssue} onClose={() => undefined} />);
    expect((screen.getByTestId('issue-edit-title') as HTMLInputElement).value).toBe('old title');
    expect((screen.getByTestId('issue-edit-description') as HTMLTextAreaElement).value).toBe(
      'old description',
    );
  });

  it('disables submit when title cleared', () => {
    wrap(<IssueEditModal projectId="proj-a" issue={baseIssue} onClose={() => undefined} />);
    fireEvent.change(screen.getByTestId('issue-edit-title'), { target: { value: '' } });
    const submit = screen.getByTestId('issue-edit-submit') as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
  });

  it('PATCHes the nested issue route with edited fields + calls onClose', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.patch('/api/projects/proj-a/issues/I-1', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({
          id: 'I-1',
          project_id: 'proj-a',
          title: 'new title',
          description: 'old description',
          status: 'open',
          created_by: 'user:hayang',
          version: 2,
          created_at: 'x',
          updated_at: 'x',
        });
      }),
    );
    const onClose = vi.fn();
    wrap(<IssueEditModal projectId="proj-a" issue={baseIssue} onClose={onClose} />);
    fireEvent.change(screen.getByTestId('issue-edit-title'), {
      target: { value: 'new title' },
    });
    fireEvent.click(screen.getByTestId('issue-edit-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(received).toMatchObject({ title: 'new title' });
  });
});

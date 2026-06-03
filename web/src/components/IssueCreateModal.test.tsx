import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import { IssueCreateModal } from './IssueCreateModal';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe('IssueCreateModal', () => {
  afterEach(() => cleanup());

  it('renders title + description fields', () => {
    wrap(<IssueCreateModal projectId="proj-a" onClose={() => undefined} />);
    expect(screen.getByTestId('issue-create-modal')).toBeInTheDocument();
    expect(screen.getByTestId('issue-create-title')).toBeInTheDocument();
    expect(screen.getByTestId('issue-create-description')).toBeInTheDocument();
  });

  it('submit is disabled until title is set', () => {
    wrap(<IssueCreateModal projectId="proj-a" onClose={() => undefined} />);
    const submit = screen.getByTestId('issue-create-submit') as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    fireEvent.change(screen.getByTestId('issue-create-title'), {
      target: { value: 'login bug' },
    });
    expect(submit.disabled).toBe(false);
  });

  it('POSTs the nested issue route with the entered fields + calls onClose', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/projects/proj-a/issues', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(
          {
            id: 'IS-NEW',
            project_id: 'proj-a',
            title: 'feature X',
            description: 'do the thing',
            status: 'open',
            created_by: 'user:hayang',
            version: 1,
            created_at: 'x',
            updated_at: 'x',
          },
          { status: 201 },
        );
      }),
    );
    const onClose = vi.fn();
    wrap(<IssueCreateModal projectId="proj-a" onClose={onClose} />);
    fireEvent.change(screen.getByTestId('issue-create-title'), {
      target: { value: 'feature X' },
    });
    fireEvent.change(screen.getByTestId('issue-create-description'), {
      target: { value: 'do the thing' },
    });
    fireEvent.click(screen.getByTestId('issue-create-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(received).toMatchObject({
      title: 'feature X',
      description: 'do the thing',
    });
  });
});

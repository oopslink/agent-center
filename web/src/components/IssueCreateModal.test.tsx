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

const projectsHandler = http.get('/api/projects', () =>
  HttpResponse.json([
    { id: 'proj-1', name: 'Alpha', tags: ['coding'] },
    { id: 'proj-2', name: 'Beta', tags: [] },
  ]),
);

describe('IssueCreateModal', () => {
  afterEach(() => cleanup());

  it('renders project picker + title + description fields', async () => {
    server.use(projectsHandler);
    wrap(<IssueCreateModal onClose={() => undefined} />);
    expect(screen.getByTestId('issue-create-modal')).toBeInTheDocument();
    expect(screen.getByTestId('issue-create-project')).toBeInTheDocument();
    expect(screen.getByTestId('issue-create-title')).toBeInTheDocument();
    expect(screen.getByTestId('issue-create-description')).toBeInTheDocument();
    // Wait for project options to load.
    await waitFor(() => {
      expect(screen.getByText('Alpha')).toBeInTheDocument();
    });
  });

  it('submit is disabled until project + title are set', async () => {
    server.use(projectsHandler);
    wrap(<IssueCreateModal onClose={() => undefined} />);
    const submit = screen.getByTestId('issue-create-submit') as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    // pick project
    await waitFor(() => expect(screen.getByText('Alpha')).toBeInTheDocument());
    fireEvent.change(screen.getByTestId('issue-create-project'), {
      target: { value: 'proj-1' },
    });
    expect(submit.disabled).toBe(true); // title still empty
    fireEvent.change(screen.getByTestId('issue-create-title'), {
      target: { value: 'login bug' },
    });
    expect(submit.disabled).toBe(false);
  });

  it('POSTs /api/issues with the entered fields + calls onClose on success', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(projectsHandler);
    server.use(
      http.post('/api/issues', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(
          { issue_id: 'I-1', conversation_id: 'C-1', event_id: 'E-1' },
          { status: 201 },
        );
      }),
    );
    const onClose = vi.fn();
    wrap(<IssueCreateModal defaultProjectId="proj-1" onClose={onClose} />);
    await waitFor(() => expect(screen.getByText('Alpha')).toBeInTheDocument());
    fireEvent.change(screen.getByTestId('issue-create-title'), {
      target: { value: 'feature X' },
    });
    fireEvent.change(screen.getByTestId('issue-create-description'), {
      target: { value: 'do the thing' },
    });
    fireEvent.click(screen.getByTestId('issue-create-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(received).toMatchObject({
      project_id: 'proj-1',
      title: 'feature X',
      description: 'do the thing',
    });
  });
});

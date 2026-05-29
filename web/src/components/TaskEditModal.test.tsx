import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import { TaskEditModal } from './TaskEditModal';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const baseTask = {
  id: 'T-1',
  title: 'old',
  description: 'old desc',
} as const;

describe('TaskEditModal', () => {
  afterEach(() => cleanup());

  it('renders prefilled fields from the task prop', () => {
    wrap(<TaskEditModal projectId="proj-a" task={baseTask} onClose={() => undefined} />);
    expect((screen.getByTestId('task-edit-title') as HTMLInputElement).value).toBe('old');
    expect((screen.getByTestId('task-edit-description') as HTMLTextAreaElement).value).toBe(
      'old desc',
    );
  });

  it('disables submit when title cleared', () => {
    wrap(<TaskEditModal projectId="proj-a" task={baseTask} onClose={() => undefined} />);
    const submit = screen.getByTestId('task-edit-submit') as HTMLButtonElement;
    expect(submit.disabled).toBe(false);
    fireEvent.change(screen.getByTestId('task-edit-title'), { target: { value: '' } });
    expect(submit.disabled).toBe(true);
  });

  it('PATCHes the nested task route with edited fields + calls onClose', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.patch('/api/projects/proj-a/tasks/T-1', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({
          id: 'T-1',
          project_id: 'proj-a',
          title: 'new title',
          description: 'old desc',
          status: 'open',
          version: 2,
          created_at: 'x',
          updated_at: 'x',
        });
      }),
    );
    const onClose = vi.fn();
    wrap(<TaskEditModal projectId="proj-a" task={baseTask} onClose={onClose} />);
    fireEvent.change(screen.getByTestId('task-edit-title'), {
      target: { value: 'new title' },
    });
    fireEvent.click(screen.getByTestId('task-edit-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(received).toMatchObject({ title: 'new title' });
  });
});

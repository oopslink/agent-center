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
  priority: 'medium',
} as const;

describe('TaskEditModal', () => {
  afterEach(() => cleanup());

  it('renders prefilled fields from the task prop', () => {
    wrap(<TaskEditModal task={baseTask} onClose={() => undefined} />);
    expect((screen.getByTestId('task-edit-title') as HTMLInputElement).value).toBe('old');
    expect((screen.getByTestId('task-edit-description') as HTMLTextAreaElement).value).toBe(
      'old desc',
    );
    expect((screen.getByTestId('task-edit-priority') as HTMLSelectElement).value).toBe('medium');
  });

  it('disables submit when title cleared', () => {
    wrap(<TaskEditModal task={baseTask} onClose={() => undefined} />);
    const submit = screen.getByTestId('task-edit-submit') as HTMLButtonElement;
    expect(submit.disabled).toBe(false);
    fireEvent.change(screen.getByTestId('task-edit-title'), { target: { value: '' } });
    expect(submit.disabled).toBe(true);
  });

  it('PATCHes /api/tasks/{id} with edited fields + calls onClose', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.patch('/api/tasks/T-1', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ task_id: 'T-1', event_id: 'E-1' });
      }),
    );
    const onClose = vi.fn();
    wrap(<TaskEditModal task={baseTask} onClose={onClose} />);
    fireEvent.change(screen.getByTestId('task-edit-title'), {
      target: { value: 'new title' },
    });
    fireEvent.change(screen.getByTestId('task-edit-priority'), {
      target: { value: 'high' },
    });
    fireEvent.click(screen.getByTestId('task-edit-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(received).toMatchObject({
      title: 'new title',
      priority: 'high',
    });
  });
});

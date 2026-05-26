import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import { TaskAbandonModal } from './TaskAbandonModal';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe('TaskAbandonModal', () => {
  afterEach(() => cleanup());

  it('renders reason + message fields', () => {
    wrap(<TaskAbandonModal taskId="T-1" onClose={() => undefined} />);
    expect(screen.getByTestId('task-abandon-modal')).toBeInTheDocument();
    expect(screen.getByTestId('task-abandon-reason')).toBeInTheDocument();
    expect(screen.getByTestId('task-abandon-message')).toBeInTheDocument();
  });

  it('submit disabled until both reason + message set', () => {
    wrap(<TaskAbandonModal taskId="T-1" onClose={() => undefined} />);
    const submit = screen.getByTestId('task-abandon-submit') as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    fireEvent.change(screen.getByTestId('task-abandon-reason'), {
      target: { value: 'obsolete' },
    });
    expect(submit.disabled).toBe(true);
    fireEvent.change(screen.getByTestId('task-abandon-message'), {
      target: { value: 'requirements changed' },
    });
    expect(submit.disabled).toBe(false);
  });

  it('POSTs reason + message to /api/tasks/{id}/abandon', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/tasks/T-1/abandon', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ task_id: 'T-1', event_id: 'E-1' });
      }),
    );
    const onClose = vi.fn();
    wrap(<TaskAbandonModal taskId="T-1" onClose={onClose} />);
    fireEvent.change(screen.getByTestId('task-abandon-reason'), {
      target: { value: 'obsolete' },
    });
    fireEvent.change(screen.getByTestId('task-abandon-message'), {
      target: { value: 'requirements changed' },
    });
    fireEvent.click(screen.getByTestId('task-abandon-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(received).toMatchObject({
      reason: 'obsolete',
      message: 'requirements changed',
    });
  });
});

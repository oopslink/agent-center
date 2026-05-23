import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { DeriveModal } from './DeriveModal';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('DeriveModal', () => {
  beforeEach(() => {
    server.use(
      http.post('/api/issues', () =>
        HttpResponse.json(
          {
            issue_id: 'IS-NEW',
            conversation_id: 'I-NEW',
            reference_count: 2,
            issue_event_id: 'E-i',
            carry_over_event_id: 'E-c',
          },
          { status: 201 },
        ),
      ),
      http.post('/api/tasks', () =>
        HttpResponse.json(
          {
            task_id: 'TS-NEW',
            conversation_id: 'T-NEW',
            reference_count: 1,
            task_event_id: 'E-t',
            carry_over_event_id: '',
          },
          { status: 201 },
        ),
      ),
    );
  });
  afterEach(() => cleanup());

  it('renders nothing when open=false', () => {
    wrap(
      <DeriveModal
        kind="issue"
        open={false}
        sourceConversationId="C1"
        sourceMessageIds={['M1']}
        onClose={() => undefined}
      />,
    );
    expect(screen.queryByTestId('derive-modal')).not.toBeInTheDocument();
  });

  it('issue happy path: submit → success pane → view link points to /issues/:id', async () => {
    const onClose = vi.fn();
    const onCreated = vi.fn();
    wrap(
      <DeriveModal
        kind="issue"
        open
        sourceConversationId="C1"
        sourceMessageIds={['M1', 'M2']}
        onClose={onClose}
        onCreated={onCreated}
      />,
    );
    await userEvent.type(screen.getByTestId('derive-title-input'), 'fix login');
    fireEvent.click(screen.getByTestId('derive-modal-submit'));
    await waitFor(() => expect(screen.getByTestId('derive-success')).toBeInTheDocument());
    expect(onCreated).toHaveBeenCalledWith('I-NEW');
    const link = screen.getByTestId('derive-success-link');
    expect(link).toHaveAttribute('href', '/issues/I-NEW');
    fireEvent.click(link);
    expect(onClose).toHaveBeenCalled();
  });

  it('task happy path: link points to /tasks/:id', async () => {
    const onCreated = vi.fn();
    wrap(
      <DeriveModal
        kind="task"
        open
        sourceConversationId="C1"
        sourceMessageIds={['M1']}
        onClose={() => undefined}
        onCreated={onCreated}
      />,
    );
    await userEvent.type(screen.getByTestId('derive-title-input'), 'rebuild');
    fireEvent.click(screen.getByTestId('derive-modal-submit'));
    await waitFor(() => expect(screen.getByTestId('derive-success-link')).toHaveAttribute('href', '/tasks/T-NEW'));
    expect(onCreated).toHaveBeenCalledWith('T-NEW');
  });

  it('cancel from form closes without submitting', () => {
    const onClose = vi.fn();
    wrap(
      <DeriveModal
        kind="issue"
        open
        sourceConversationId="C1"
        sourceMessageIds={['M1']}
        onClose={onClose}
      />,
    );
    fireEvent.click(screen.getByTestId('derive-modal-cancel'));
    expect(onClose).toHaveBeenCalled();
  });

  it('surfaces server error and keeps the form open', async () => {
    server.use(
      http.post('/api/issues', () =>
        HttpResponse.json({ error: 'invalid_input', message: 'title required' }, { status: 400 }),
      ),
    );
    wrap(
      <DeriveModal
        kind="issue"
        open
        sourceConversationId="C1"
        sourceMessageIds={['M1']}
        onClose={() => undefined}
      />,
    );
    await userEvent.type(screen.getByTestId('derive-title-input'), 'short');
    fireEvent.click(screen.getByTestId('derive-modal-submit'));
    await waitFor(() => expect(screen.getByTestId('derive-error')).toHaveTextContent(/title required/));
    expect(screen.queryByTestId('derive-success')).not.toBeInTheDocument();
  });

  it('submit disabled when title is empty', () => {
    wrap(
      <DeriveModal
        kind="issue"
        open
        sourceConversationId="C1"
        sourceMessageIds={['M1']}
        onClose={() => undefined}
      />,
    );
    expect(screen.getByTestId('derive-modal-submit')).toBeDisabled();
  });
});

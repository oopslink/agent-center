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

const projectFixture = [
  {
    id: 'p-demo',
    name: 'Demo Project',
    description: '',
    tags: ['coding'],
    version: 1,
    created_at: '2026-05-24T00:00:00Z',
    updated_at: '2026-05-24T00:00:00Z',
  },
  {
    id: 'p-other',
    name: 'Other Project',
    description: '',
    tags: [],
    version: 1,
    created_at: '2026-05-24T01:00:00Z',
    updated_at: '2026-05-24T01:00:00Z',
  },
];

describe('DeriveModal', () => {
  beforeEach(() => {
    server.use(
      http.get('/api/projects', () => HttpResponse.json({ projects: projectFixture })),
      http.post('/api/issues', async ({ request }) => {
        const body = (await request.json()) as { project_id?: string };
        // v2.1-A — project_id is required; if absent, simulate the
        // backend's 500 (project_not_found at service layer).
        if (!body.project_id) {
          return HttpResponse.json(
            { error: 'internal', message: 'project_id required' },
            { status: 400 },
          );
        }
        return HttpResponse.json(
          {
            issue_id: 'IS-NEW',
            conversation_id: 'I-NEW',
            reference_count: 2,
            issue_event_id: 'E-i',
            carry_over_event_id: 'E-c',
          },
          { status: 201 },
        );
      }),
      http.post('/api/tasks', async ({ request }) => {
        const body = (await request.json()) as { project_id?: string };
        if (!body.project_id) {
          return HttpResponse.json(
            { error: 'internal', message: 'project_id required' },
            { status: 400 },
          );
        }
        return HttpResponse.json(
          {
            task_id: 'TS-NEW',
            conversation_id: 'T-NEW',
            reference_count: 1,
            task_event_id: 'E-t',
            carry_over_event_id: '',
          },
          { status: 201 },
        );
      }),
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

  it('issue happy path: pick project + title → submit → success pane → /issues/:id link', async () => {
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
    // wait for project list to load
    await waitFor(() => expect(screen.getByTestId('derive-project-select')).toBeInTheDocument());
    fireEvent.change(screen.getByTestId('derive-project-select'), {
      target: { value: 'p-demo' },
    });
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
    await waitFor(() => expect(screen.getByTestId('derive-project-select')).toBeInTheDocument());
    fireEvent.change(screen.getByTestId('derive-project-select'), {
      target: { value: 'p-other' },
    });
    await userEvent.type(screen.getByTestId('derive-title-input'), 'rebuild');
    fireEvent.click(screen.getByTestId('derive-modal-submit'));
    await waitFor(() =>
      expect(screen.getByTestId('derive-success-link')).toHaveAttribute('href', '/tasks/T-NEW'),
    );
    expect(onCreated).toHaveBeenCalledWith('T-NEW');
  });

  it('cancel from form closes without submitting', async () => {
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
    await waitFor(() => expect(screen.getByTestId('derive-project-select')).toBeInTheDocument());
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
    await waitFor(() => expect(screen.getByTestId('derive-project-select')).toBeInTheDocument());
    fireEvent.change(screen.getByTestId('derive-project-select'), {
      target: { value: 'p-demo' },
    });
    await userEvent.type(screen.getByTestId('derive-title-input'), 'short');
    fireEvent.click(screen.getByTestId('derive-modal-submit'));
    await waitFor(() =>
      expect(screen.getByTestId('derive-error')).toHaveTextContent(/title required/),
    );
    expect(screen.queryByTestId('derive-success')).not.toBeInTheDocument();
  });

  it('submit disabled until BOTH project and title are filled', async () => {
    wrap(
      <DeriveModal
        kind="issue"
        open
        sourceConversationId="C1"
        sourceMessageIds={['M1']}
        onClose={() => undefined}
      />,
    );
    // After load — submit still disabled (no title yet)
    await waitFor(() => expect(screen.getByTestId('derive-project-select')).toBeInTheDocument());
    expect(screen.getByTestId('derive-modal-submit')).toBeDisabled();

    // Type title only → still disabled (no project selected)
    await userEvent.type(screen.getByTestId('derive-title-input'), 'a');
    expect(screen.getByTestId('derive-modal-submit')).toBeDisabled();

    // Pick project → now enabled
    fireEvent.change(screen.getByTestId('derive-project-select'), {
      target: { value: 'p-demo' },
    });
    expect(screen.getByTestId('derive-modal-submit')).toBeEnabled();
  });

  it('shows the no-projects empty state + does not render the picker', async () => {
    server.use(http.get('/api/projects', () => HttpResponse.json({ projects: [] })));
    wrap(
      <DeriveModal
        kind="issue"
        open
        sourceConversationId="C1"
        sourceMessageIds={['M1']}
        onClose={() => undefined}
      />,
    );
    await waitFor(() =>
      expect(screen.getByTestId('derive-no-projects')).toBeInTheDocument(),
    );
    expect(screen.queryByTestId('derive-project-select')).not.toBeInTheDocument();
    // submit still disabled — no project to pick
    expect(screen.getByTestId('derive-modal-submit')).toBeDisabled();
  });
});

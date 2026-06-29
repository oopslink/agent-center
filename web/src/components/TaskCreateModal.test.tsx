import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import { TaskCreateModal } from './TaskCreateModal';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe('TaskCreateModal', () => {
  afterEach(() => cleanup());

  it('renders title + description fields', () => {
    wrap(<TaskCreateModal projectId="proj-a" onClose={() => undefined} />);
    expect(screen.getByTestId('task-create-modal')).toBeInTheDocument();
    expect(screen.getByTestId('task-create-title')).toBeInTheDocument();
    expect(screen.getByTestId('task-create-description')).toBeInTheDocument();
  });

  it('closes on Escape (WAI-ARIA dialog — useModalA11y)', () => {
    const onClose = vi.fn();
    wrap(<TaskCreateModal projectId="proj-a" onClose={onClose} />);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('submit disabled until title set', () => {
    wrap(<TaskCreateModal projectId="proj-a" onClose={() => undefined} />);
    const submit = screen.getByTestId('task-create-submit') as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    fireEvent.change(screen.getByTestId('task-create-title'), {
      target: { value: 'fix it' },
    });
    expect(submit.disabled).toBe(false);
  });

  it('POSTs the nested task route with entered fields + calls onClose', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/projects/proj-a/tasks', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(
          {
            id: 'TS-NEW',
            project_id: 'proj-a',
            title: 'fix the bug',
            description: '',
            status: 'open',
            version: 1,
            created_at: 'x',
            updated_at: 'x',
          },
          { status: 201 },
        );
      }),
    );
    const onClose = vi.fn();
    wrap(<TaskCreateModal projectId="proj-a" onClose={onClose} />);
    fireEvent.change(screen.getByTestId('task-create-title'), {
      target: { value: 'fix the bug' },
    });
    fireEvent.click(screen.getByTestId('task-create-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(received).toMatchObject({ title: 'fix the bug' });
  });

  it('T566: required_capabilities (canonical) flow into the create body', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/projects/proj-a/tasks', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(
          { id: 'TS-NEW', project_id: 'proj-a', title: 'x', description: '', status: 'open', version: 1, created_at: 'x', updated_at: 'x' },
          { status: 201 },
        );
      }),
    );
    wrap(<TaskCreateModal projectId="proj-a" onClose={() => undefined} />);
    fireEvent.change(screen.getByTestId('task-create-title'), { target: { value: 'needs go' } });
    const caps = screen.getByTestId('task-create-caps-input');
    fireEvent.change(caps, { target: { value: ' Go ' } });
    fireEvent.keyDown(caps, { key: 'Enter' });
    fireEvent.click(screen.getByTestId('task-create-submit'));
    await waitFor(() => expect(received).toBeDefined());
    expect(received).toMatchObject({ title: 'needs go', required_capabilities: ['go'] });
  });
});

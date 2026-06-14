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
  status: 'open' as const,
  assignee: '',
  tags: ['alpha'],
};

// Capture the PATCH body and respond with a refreshed task.
function capturePatch(): { received: () => Record<string, unknown> | undefined } {
  let body: Record<string, unknown> | undefined;
  server.use(
    http.patch('/api/projects/proj-a/tasks/T-1', async ({ request }) => {
      body = (await request.json()) as Record<string, unknown>;
      return HttpResponse.json({
        id: 'T-1',
        project_id: 'proj-a',
        title: 'old',
        description: 'old desc',
        status: 'open',
        assignee: '',
        tags: [],
        version: 2,
        created_at: 'x',
        updated_at: 'x',
      });
    }),
  );
  return { received: () => body };
}

// Members for the assignee picker.
function mockMembers() {
  server.use(
    http.get('/api/members', () =>
      HttpResponse.json([
        {
          id: 'mem-1', organization_id: 'org-test', identity_id: 'user:hayang',
          kind: 'user', role: 'owner', status: 'joined', joined_at: 'x', display_name: 'Hayang',
        },
        {
          id: 'mem-2', organization_id: 'org-test', identity_id: 'agent:bot1',
          kind: 'agent', role: 'member', status: 'joined', joined_at: 'x', display_name: 'Bot One',
        },
      ]),
    ),
  );
}

describe('TaskEditModal', () => {
  afterEach(() => cleanup());

  it('renders prefilled fields from the task prop', () => {
    wrap(<TaskEditModal projectId="proj-a" task={baseTask} onClose={() => undefined} />);
    expect((screen.getByTestId('task-edit-title') as HTMLInputElement).value).toBe('old');
    expect((screen.getByTestId('task-edit-description') as HTMLTextAreaElement).value).toBe(
      'old desc',
    );
    expect((screen.getByTestId('task-edit-status') as HTMLSelectElement).value).toBe('open');
    expect(screen.getAllByTestId('task-edit-tag-chip')).toHaveLength(1);
    expect(screen.getByTestId('task-edit-tag-chip')).toHaveTextContent('alpha');
  });

  it('disables submit when title cleared', () => {
    wrap(<TaskEditModal projectId="proj-a" task={baseTask} onClose={() => undefined} />);
    fireEvent.change(screen.getByTestId('task-edit-title'), { target: { value: '' } });
    expect((screen.getByTestId('task-edit-submit') as HTMLButtonElement).disabled).toBe(true);
  });

  it('disables submit when nothing is dirty', () => {
    wrap(<TaskEditModal projectId="proj-a" task={baseTask} onClose={() => undefined} />);
    expect((screen.getByTestId('task-edit-submit') as HTMLButtonElement).disabled).toBe(true);
  });

  it('PATCHes with ONLY the changed (dirty) fields', async () => {
    const cap = capturePatch();
    const onClose = vi.fn();
    wrap(<TaskEditModal projectId="proj-a" task={baseTask} onClose={onClose} />);
    fireEvent.change(screen.getByTestId('task-edit-title'), { target: { value: 'new title' } });
    fireEvent.click(screen.getByTestId('task-edit-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    // ONLY title changed → body has exactly { title }, no description/status/etc.
    expect(cap.received()).toEqual({ title: 'new title' });
  });

  it('routes a real (re)assignment through the assign endpoint, not the batch PATCH (F-7)', async () => {
    mockMembers();
    const cap = capturePatch();
    // Capture the dedicated assign POST — this is the dispatching path the agent
    // needs (the batch PATCH does NOT dispatch, so assignee MUST go here).
    let assignBody: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/projects/proj-a/tasks/T-1/assign', async ({ request }) => {
        assignBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({
          id: 'T-1', project_id: 'proj-a', title: 'old', description: 'old desc',
          status: 'running', assignee: 'agent:bot1', tags: [], version: 3,
          created_at: 'x', updated_at: 'x',
        });
      }),
    );
    const onClose = vi.fn();
    wrap(<TaskEditModal projectId="proj-a" task={baseTask} onClose={onClose} />);
    await screen.findByText('Hayang (user)');
    fireEvent.change(screen.getByTestId('task-edit-status'), { target: { value: 'running' } });
    fireEvent.change(screen.getByTestId('task-edit-assignee'), { target: { value: 'agent:bot1' } });
    fireEvent.change(screen.getByTestId('task-edit-tags-input'), { target: { value: 'beta' } });
    fireEvent.keyDown(screen.getByTestId('task-edit-tags-input'), { key: 'Enter' });
    fireEvent.click(screen.getByTestId('task-edit-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    // status + tags go via the batch PATCH (assignee is NOT in the PATCH body)...
    expect(cap.received()).toEqual({ status: 'running', tags: ['alpha', 'beta'] });
    // ...and the assignment goes through the dedicated dispatching endpoint.
    expect(assignBody).toEqual({ assignee: 'agent:bot1' });
  });

  it('assignee "" (Unassigned) is sent to unassign', async () => {
    mockMembers();
    const cap = capturePatch();
    const onClose = vi.fn();
    const assigned = { ...baseTask, assignee: 'agent:bot1', tags: [] as string[] };
    wrap(<TaskEditModal projectId="proj-a" task={assigned} onClose={onClose} />);
    await screen.findByText('Bot One (agent)');
    fireEvent.change(screen.getByTestId('task-edit-assignee'), { target: { value: '' } });
    fireEvent.click(screen.getByTestId('task-edit-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(cap.received()).toEqual({ assignee: '' });
  });

  it('status select offers all TaskStatus values', () => {
    wrap(<TaskEditModal projectId="proj-a" task={baseTask} onClose={() => undefined} />);
    const opts = Array.from(
      (screen.getByTestId('task-edit-status') as HTMLSelectElement).options,
    ).map((o) => o.value);
    expect(opts).toEqual([
      'open', 'running', 'completed', 'discarded', 'reopened',
    ]);
  });

  it('assignee select lists Unassigned + each member from useMembers', async () => {
    mockMembers();
    wrap(<TaskEditModal projectId="proj-a" task={baseTask} onClose={() => undefined} />);
    await screen.findByText('Hayang (user)');
    const opts = Array.from(
      (screen.getByTestId('task-edit-assignee') as HTMLSelectElement).options,
    ).map((o) => ({ value: o.value, label: o.textContent }));
    expect(opts).toEqual([
      { value: '', label: 'Unassigned' },
      { value: 'user:hayang', label: 'Hayang (user)' },
      { value: 'agent:bot1', label: 'Bot One (agent)' },
    ]);
  });

  describe('tags chip input', () => {
    const noTags = { ...baseTask, tags: [] as string[] };

    it('adds a tag via Enter → chip appears', () => {
      wrap(<TaskEditModal projectId="proj-a" task={noTags} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('task-edit-tags-input'), { target: { value: 'red' } });
      fireEvent.keyDown(screen.getByTestId('task-edit-tags-input'), { key: 'Enter' });
      expect(screen.getByTestId('task-edit-tag-chip')).toHaveTextContent('red');
    });

    it('adds a tag via comma → chip appears', () => {
      wrap(<TaskEditModal projectId="proj-a" task={noTags} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('task-edit-tags-input'), { target: { value: 'blue' } });
      fireEvent.keyDown(screen.getByTestId('task-edit-tags-input'), { key: ',' });
      expect(screen.getByTestId('task-edit-tag-chip')).toHaveTextContent('blue');
    });

    it('trims whitespace on commit', () => {
      wrap(<TaskEditModal projectId="proj-a" task={noTags} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('task-edit-tags-input'), { target: { value: '  pad  ' } });
      fireEvent.keyDown(screen.getByTestId('task-edit-tags-input'), { key: 'Enter' });
      expect(screen.getByTestId('task-edit-tag-chip')).toHaveTextContent('pad');
    });

    it('dedups — committing an existing tag adds no new chip', () => {
      wrap(<TaskEditModal projectId="proj-a" task={baseTask} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('task-edit-tags-input'), { target: { value: 'alpha' } });
      fireEvent.keyDown(screen.getByTestId('task-edit-tags-input'), { key: 'Enter' });
      expect(screen.getAllByTestId('task-edit-tag-chip')).toHaveLength(1);
    });

    it('rejects a 17-RUNE ASCII tag with an inline error', () => {
      wrap(<TaskEditModal projectId="proj-a" task={noTags} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('task-edit-tags-input'), {
        target: { value: 'x'.repeat(17) },
      });
      fireEvent.keyDown(screen.getByTestId('task-edit-tags-input'), { key: 'Enter' });
      expect(screen.queryByTestId('task-edit-tag-chip')).toBeNull();
      expect(screen.getByTestId('task-edit-tag-error')).toHaveTextContent('Tag too long (max 16)');
    });

    it('rejects a 17-CJK tag (rune boundary, NOT tag.length)', () => {
      // 17 CJK chars = 17 runes but 51 UTF-16 code units; tag.length would wildly
      // over-count. Must be rejected at the 16-RUNE boundary.
      const cjk17 = '中'.repeat(17);
      wrap(<TaskEditModal projectId="proj-a" task={noTags} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('task-edit-tags-input'), { target: { value: cjk17 } });
      fireEvent.keyDown(screen.getByTestId('task-edit-tags-input'), { key: 'Enter' });
      expect(screen.queryByTestId('task-edit-tag-chip')).toBeNull();
      expect(screen.getByTestId('task-edit-tag-error')).toHaveTextContent('Tag too long (max 16)');
    });

    it('accepts a 16-CJK tag', () => {
      const cjk16 = '中'.repeat(16);
      wrap(<TaskEditModal projectId="proj-a" task={noTags} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('task-edit-tags-input'), { target: { value: cjk16 } });
      fireEvent.keyDown(screen.getByTestId('task-edit-tags-input'), { key: 'Enter' });
      expect(screen.getByTestId('task-edit-tag-chip')).toHaveTextContent(cjk16);
      expect(screen.queryByTestId('task-edit-tag-error')).toBeNull();
    });

    it('rejects the 11th tag', () => {
      const tenTags = { ...baseTask, tags: Array.from({ length: 10 }, (_, i) => `t${i}`) };
      wrap(<TaskEditModal projectId="proj-a" task={tenTags} onClose={() => undefined} />);
      expect(screen.getAllByTestId('task-edit-tag-chip')).toHaveLength(10);
      fireEvent.change(screen.getByTestId('task-edit-tags-input'), { target: { value: 'eleven' } });
      fireEvent.keyDown(screen.getByTestId('task-edit-tags-input'), { key: 'Enter' });
      expect(screen.getAllByTestId('task-edit-tag-chip')).toHaveLength(10);
      expect(screen.getByTestId('task-edit-tag-error')).toHaveTextContent('Max 10 tags');
    });

    it('× removes a chip', () => {
      wrap(<TaskEditModal projectId="proj-a" task={baseTask} onClose={() => undefined} />);
      expect(screen.getAllByTestId('task-edit-tag-chip')).toHaveLength(1);
      fireEvent.click(screen.getByTestId('task-edit-tag-remove'));
      expect(screen.queryByTestId('task-edit-tag-chip')).toBeNull();
    });

    it('save disabled while a too-long tag error is showing', () => {
      wrap(<TaskEditModal projectId="proj-a" task={noTags} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('task-edit-title'), { target: { value: 'changed' } });
      fireEvent.change(screen.getByTestId('task-edit-tags-input'), {
        target: { value: 'x'.repeat(17) },
      });
      fireEvent.keyDown(screen.getByTestId('task-edit-tags-input'), { key: 'Enter' });
      expect((screen.getByTestId('task-edit-submit') as HTMLButtonElement).disabled).toBe(true);
    });
  });

  it('400 response → modal stays open + shows error', async () => {
    server.use(
      http.patch('/api/projects/proj-a/tasks/T-1', () =>
        HttpResponse.json({ message: 'version conflict' }, { status: 400 }),
      ),
    );
    const onClose = vi.fn();
    wrap(<TaskEditModal projectId="proj-a" task={baseTask} onClose={onClose} />);
    fireEvent.change(screen.getByTestId('task-edit-title'), { target: { value: 'boom' } });
    fireEvent.click(screen.getByTestId('task-edit-submit'));
    await screen.findByTestId('task-edit-error');
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByTestId('task-edit-modal')).toBeInTheDocument();
  });
});

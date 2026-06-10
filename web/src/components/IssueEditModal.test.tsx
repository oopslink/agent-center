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
  status: 'open' as const,
  tags: ['alpha'],
};

// Capture the PATCH body and respond with a refreshed issue.
function capturePatch(): { received: () => Record<string, unknown> | undefined } {
  let body: Record<string, unknown> | undefined;
  server.use(
    http.patch('/api/projects/proj-a/issues/I-1', async ({ request }) => {
      body = (await request.json()) as Record<string, unknown>;
      return HttpResponse.json({
        id: 'I-1',
        project_id: 'proj-a',
        title: 'old title',
        description: 'old description',
        status: 'open',
        tags: [],
        created_by: 'user:hayang',
        version: 2,
        created_at: 'x',
        updated_at: 'x',
      });
    }),
  );
  return { received: () => body };
}

describe('IssueEditModal', () => {
  afterEach(() => cleanup());

  it('renders prefilled fields from the issue prop', () => {
    wrap(<IssueEditModal projectId="proj-a" issue={baseIssue} onClose={() => undefined} />);
    expect((screen.getByTestId('issue-edit-title') as HTMLInputElement).value).toBe('old title');
    expect((screen.getByTestId('issue-edit-description') as HTMLTextAreaElement).value).toBe(
      'old description',
    );
    expect((screen.getByTestId('issue-edit-status') as HTMLSelectElement).value).toBe('open');
    expect(screen.getAllByTestId('issue-edit-tag-chip')).toHaveLength(1);
    expect(screen.getByTestId('issue-edit-tag-chip')).toHaveTextContent('alpha');
  });

  it('has NO assignee field (Issues are not assignable)', () => {
    wrap(<IssueEditModal projectId="proj-a" issue={baseIssue} onClose={() => undefined} />);
    expect(screen.queryByTestId('issue-edit-assignee')).toBeNull();
  });

  it('status select offers all IssueStatus values', () => {
    wrap(<IssueEditModal projectId="proj-a" issue={baseIssue} onClose={() => undefined} />);
    const opts = Array.from(
      (screen.getByTestId('issue-edit-status') as HTMLSelectElement).options,
    ).map((o) => o.value);
    expect(opts).toEqual([
      'open', 'in_progress', 'resolved', 'closed', 'discarded', 'reopened',
    ]);
  });

  it('disables submit when title cleared', () => {
    wrap(<IssueEditModal projectId="proj-a" issue={baseIssue} onClose={() => undefined} />);
    fireEvent.change(screen.getByTestId('issue-edit-title'), { target: { value: '' } });
    const submit = screen.getByTestId('issue-edit-submit') as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
  });

  it('disables submit when nothing is dirty', () => {
    wrap(<IssueEditModal projectId="proj-a" issue={baseIssue} onClose={() => undefined} />);
    expect((screen.getByTestId('issue-edit-submit') as HTMLButtonElement).disabled).toBe(true);
  });

  it('PATCHes with ONLY the changed (dirty) title field', async () => {
    const cap = capturePatch();
    const onClose = vi.fn();
    wrap(<IssueEditModal projectId="proj-a" issue={baseIssue} onClose={onClose} />);
    fireEvent.change(screen.getByTestId('issue-edit-title'), { target: { value: 'new title' } });
    fireEvent.click(screen.getByTestId('issue-edit-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    // ONLY title changed → body has exactly { title }, no description/status/tags.
    expect(cap.received()).toEqual({ title: 'new title' });
  });

  it('batch-saves status + tags in one dirty-only PATCH (no assignee key)', async () => {
    const cap = capturePatch();
    const onClose = vi.fn();
    wrap(<IssueEditModal projectId="proj-a" issue={baseIssue} onClose={onClose} />);
    fireEvent.change(screen.getByTestId('issue-edit-status'), { target: { value: 'resolved' } });
    fireEvent.change(screen.getByTestId('issue-edit-tags-input'), { target: { value: 'beta' } });
    fireEvent.keyDown(screen.getByTestId('issue-edit-tags-input'), { key: 'Enter' });
    fireEvent.click(screen.getByTestId('issue-edit-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    // title/description untouched → absent; key name is "description"; NO assignee.
    expect(cap.received()).toEqual({
      status: 'resolved',
      tags: ['alpha', 'beta'],
    });
  });

  describe('tags chip input', () => {
    const noTags = { ...baseIssue, tags: [] as string[] };

    it('adds a tag via Enter → chip appears', () => {
      wrap(<IssueEditModal projectId="proj-a" issue={noTags} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('issue-edit-tags-input'), { target: { value: 'red' } });
      fireEvent.keyDown(screen.getByTestId('issue-edit-tags-input'), { key: 'Enter' });
      expect(screen.getByTestId('issue-edit-tag-chip')).toHaveTextContent('red');
    });

    it('adds a tag via comma → chip appears', () => {
      wrap(<IssueEditModal projectId="proj-a" issue={noTags} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('issue-edit-tags-input'), { target: { value: 'blue' } });
      fireEvent.keyDown(screen.getByTestId('issue-edit-tags-input'), { key: ',' });
      expect(screen.getByTestId('issue-edit-tag-chip')).toHaveTextContent('blue');
    });

    it('trims whitespace on commit', () => {
      wrap(<IssueEditModal projectId="proj-a" issue={noTags} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('issue-edit-tags-input'), { target: { value: '  pad  ' } });
      fireEvent.keyDown(screen.getByTestId('issue-edit-tags-input'), { key: 'Enter' });
      expect(screen.getByTestId('issue-edit-tag-chip')).toHaveTextContent('pad');
    });

    it('dedups — committing an existing tag adds no new chip', () => {
      wrap(<IssueEditModal projectId="proj-a" issue={baseIssue} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('issue-edit-tags-input'), { target: { value: 'alpha' } });
      fireEvent.keyDown(screen.getByTestId('issue-edit-tags-input'), { key: 'Enter' });
      expect(screen.getAllByTestId('issue-edit-tag-chip')).toHaveLength(1);
    });

    it('rejects a 17-RUNE ASCII tag with an inline error', () => {
      wrap(<IssueEditModal projectId="proj-a" issue={noTags} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('issue-edit-tags-input'), {
        target: { value: 'x'.repeat(17) },
      });
      fireEvent.keyDown(screen.getByTestId('issue-edit-tags-input'), { key: 'Enter' });
      expect(screen.queryByTestId('issue-edit-tag-chip')).toBeNull();
      expect(screen.getByTestId('issue-edit-tag-error')).toHaveTextContent('Tag too long (max 16)');
    });

    it('rejects a 17-CJK tag (rune boundary, NOT tag.length)', () => {
      const cjk17 = '中'.repeat(17);
      wrap(<IssueEditModal projectId="proj-a" issue={noTags} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('issue-edit-tags-input'), { target: { value: cjk17 } });
      fireEvent.keyDown(screen.getByTestId('issue-edit-tags-input'), { key: 'Enter' });
      expect(screen.queryByTestId('issue-edit-tag-chip')).toBeNull();
      expect(screen.getByTestId('issue-edit-tag-error')).toHaveTextContent('Tag too long (max 16)');
    });

    it('accepts a 16-CJK tag', () => {
      const cjk16 = '中'.repeat(16);
      wrap(<IssueEditModal projectId="proj-a" issue={noTags} onClose={() => undefined} />);
      fireEvent.change(screen.getByTestId('issue-edit-tags-input'), { target: { value: cjk16 } });
      fireEvent.keyDown(screen.getByTestId('issue-edit-tags-input'), { key: 'Enter' });
      expect(screen.getByTestId('issue-edit-tag-chip')).toHaveTextContent(cjk16);
      expect(screen.queryByTestId('issue-edit-tag-error')).toBeNull();
    });

    it('rejects the 11th tag', () => {
      const tenTags = { ...baseIssue, tags: Array.from({ length: 10 }, (_, i) => `t${i}`) };
      wrap(<IssueEditModal projectId="proj-a" issue={tenTags} onClose={() => undefined} />);
      expect(screen.getAllByTestId('issue-edit-tag-chip')).toHaveLength(10);
      fireEvent.change(screen.getByTestId('issue-edit-tags-input'), { target: { value: 'eleven' } });
      fireEvent.keyDown(screen.getByTestId('issue-edit-tags-input'), { key: 'Enter' });
      expect(screen.getAllByTestId('issue-edit-tag-chip')).toHaveLength(10);
      expect(screen.getByTestId('issue-edit-tag-error')).toHaveTextContent('Max 10 tags');
    });

    it('x removes a chip', () => {
      wrap(<IssueEditModal projectId="proj-a" issue={baseIssue} onClose={() => undefined} />);
      expect(screen.getAllByTestId('issue-edit-tag-chip')).toHaveLength(1);
      fireEvent.click(screen.getByTestId('issue-edit-tag-remove'));
      expect(screen.queryByTestId('issue-edit-tag-chip')).toBeNull();
    });
  });

  it('400 response → modal stays open + shows error', async () => {
    server.use(
      http.patch('/api/projects/proj-a/issues/I-1', () =>
        HttpResponse.json({ message: 'version conflict' }, { status: 400 }),
      ),
    );
    const onClose = vi.fn();
    wrap(<IssueEditModal projectId="proj-a" issue={baseIssue} onClose={onClose} />);
    fireEvent.change(screen.getByTestId('issue-edit-title'), { target: { value: 'boom' } });
    fireEvent.click(screen.getByTestId('issue-edit-submit'));
    await screen.findByTestId('issue-edit-error');
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByTestId('issue-edit-modal')).toBeInTheDocument();
  });
});

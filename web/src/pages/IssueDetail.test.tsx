import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import IssueDetail from './IssueDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/projects/:projectId/issues/:id" element={<IssueDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// v2.8.1 (@oopslink directive): IssueDetail status is READ-ONLY display.
// The inline transition controls are GONE — the SOLE edit entry is the Edit
// button → IssueEditModal, which batch-PATCHes title/description/status/tags.

describe('IssueDetail page', () => {
  afterEach(() => cleanup());

  it('renders header + description from the Issue projection', async () => {
    server.use(
      http.get('/api/projects/proj-a/issues/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          project_id: 'proj-a',
          title: 'login bug',
          description: 'cannot sign in',
          status: 'open',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T01:00:00Z',
        }),
      ),
    );
    wrap('/projects/proj-a/issues/IS-1');
    // Title is echoed by the #137 conversation owner banner; scope to heading.
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'login bug' })).toBeInTheDocument(),
    );
    expect(screen.getByTestId('issue-description')).toHaveTextContent('cannot sign in');
    // v2.8.1 sidebar-align: status drives the compact StatusBlock under a
    // "Status" label in the two-section IssueDetailSidebar.
    const statusBlock = screen.getByTestId('status-block');
    expect(statusBlock).toHaveAttribute('data-status', 'open');
    expect(statusBlock).toHaveTextContent(/open/i);
    // project + edit live in the right IssueDetailSidebar (mirror of TaskDetail).
    expect(screen.getByTestId('issue-detail-sidebar')).toBeInTheDocument();
    expect(screen.getByTestId('issue-project-link')).toHaveAttribute(
      'href',
      '/projects/proj-a',
    );
    // The bottom read-only section shows the "Details" header + Issue ID pill +
    // Created, and there is NO assignee section (Issues have none).
    expect(screen.getByText('Details')).toBeInTheDocument();
    expect(screen.getByTestId('issue-id-pill')).toBeInTheDocument();
    expect(screen.getByTestId('issue-created')).toBeInTheDocument();
    expect(screen.queryByText('Assignee')).toBeNull();
  });

  it('status is READ-ONLY: no inline transition controls are rendered', async () => {
    server.use(
      http.get('/api/projects/proj-a/issues/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          project_id: 'proj-a',
          title: 'open issue',
          description: '',
          status: 'open',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T01:00:00Z',
        }),
      ),
    );
    wrap('/projects/proj-a/issues/IS-1');
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'open issue' })).toBeInTheDocument(),
    );
    // The inline transition buttons (open → in_progress / discarded) are GONE.
    expect(screen.queryByTestId('issue-transition-in_progress')).toBeNull();
    expect(screen.queryByTestId('issue-transition-discarded')).toBeNull();
    expect(screen.queryByTestId('issue-transition-resolved')).toBeNull();
    // Status survives as read-only display via the StatusBlock.
    expect(screen.getByTestId('status-block')).toHaveAttribute('data-status', 'open');
    // The Edit-Issue pencil button (aria-label "Edit issue") is the sole edit entry.
    const editBtn = screen.getByTestId('issue-edit-button');
    expect(editBtn).toBeInTheDocument();
    expect(editBtn).toHaveAttribute('aria-label', 'Edit issue');
  });

  it('Edit button opens the modal covering all 4 fields (title/desc/status/tags)', async () => {
    server.use(
      http.get('/api/projects/proj-a/issues/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          project_id: 'proj-a',
          title: 'open issue',
          description: 'a desc',
          status: 'open',
          tags: ['alpha'],
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T01:00:00Z',
        }),
      ),
    );
    wrap('/projects/proj-a/issues/IS-1');
    await waitFor(() => expect(screen.getByTestId('issue-edit-button')).toBeInTheDocument());
    // Parity with TaskDetail: the sidebar shows the tags read-only (chips) BEFORE
    // any edit — Issue tags must be visible, not just editable.
    expect(screen.getByTestId('issue-tag-chip')).toHaveAttribute('data-tag', 'alpha');
    fireEvent.click(screen.getByTestId('issue-edit-button'));
    // Modal opens — all 4 editable fields present, NO assignee.
    expect(screen.getByTestId('issue-edit-modal')).toBeInTheDocument();
    expect(screen.getByTestId('issue-edit-title')).toBeInTheDocument();
    expect(screen.getByTestId('issue-edit-description')).toBeInTheDocument();
    expect(screen.getByTestId('issue-edit-status')).toBeInTheDocument();
    expect(screen.getByTestId('issue-edit-tags-input')).toBeInTheDocument();
    expect(screen.queryByTestId('issue-edit-assignee')).toBeNull();
  });

  it('surfaces issue lookup error', async () => {
    server.use(
      http.get('/api/projects/proj-a/issues/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such issue' }, { status: 404 }),
      ),
    );
    wrap('/projects/proj-a/issues/missing');
    await waitFor(() =>
      expect(screen.getByTestId('issue-not-found')).toHaveTextContent(/no such issue/),
    );
  });

  it('shows org_ref (I<n>) in the header + breadcrumb leaf when present (#245)', async () => {
    server.use(
      http.get('/api/projects/proj-a/issues/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          project_id: 'proj-a',
          title: 'login bug',
          description: 'x',
          status: 'open',
          org_ref: 'I42',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T01:00:00Z',
        }),
      ),
    );
    wrap('/projects/proj-a/issues/issue-01KT8DABCDEF');
    await waitFor(() => expect(screen.getByTestId('issue-org-ref')).toHaveTextContent('I42'));
    expect(screen.getByRole('heading', { name: /I42 · login bug/ })).toBeInTheDocument();
    expect(screen.getByTestId('breadcrumb')).toHaveTextContent('I42 - login bug');
  });
});

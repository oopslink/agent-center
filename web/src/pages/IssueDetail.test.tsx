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

// v2.7 ProjectManager BC: IssueDetail is nested under a project and is
// driven entirely by the Issue projection. No conversation/message
// thread; state changes go through the single transition endpoint.

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
    await waitFor(() => expect(screen.getByText('login bug')).toBeInTheDocument());
    expect(screen.getByTestId('issue-description')).toHaveTextContent('cannot sign in');
    expect(screen.getByTestId('issue-status')).toHaveTextContent('open');
    expect(screen.getByTestId('issue-project-link')).toHaveAttribute(
      'href',
      '/projects/proj-a',
    );
  });

  it('exposes valid transitions for the current status', async () => {
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
    await waitFor(() => expect(screen.getByText('open issue')).toBeInTheDocument());
    // open → {in_progress, withdrawn}
    expect(screen.getByTestId('issue-transition-in_progress')).toBeInTheDocument();
    expect(screen.getByTestId('issue-transition-withdrawn')).toBeInTheDocument();
  });

  it('posts the transition when an action is clicked', async () => {
    let received: Record<string, unknown> | undefined;
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
      http.post('/api/projects/proj-a/issues/IS-1/transition', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({
          id: 'IS-1',
          project_id: 'proj-a',
          title: 'open issue',
          description: '',
          status: 'in_progress',
          created_by: 'user:hayang',
          version: 2,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T02:00:00Z',
        });
      }),
    );
    wrap('/projects/proj-a/issues/IS-1');
    await waitFor(() => expect(screen.getByTestId('issue-transition-in_progress')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('issue-transition-in_progress'));
    await waitFor(() => expect(received).toMatchObject({ status: 'in_progress' }));
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
});

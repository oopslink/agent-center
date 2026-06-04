import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import { useAppStore } from '@/store/app';
import ProjectDetail from './ProjectDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/projects/:id" element={<ProjectDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const projectAlpha = {
  id: 'proj-a',
  organization_id: 'org-test',
  name: 'Project Alpha',
  description: 'the alpha project',
  status: 'active',
  created_by: 'user:hayang',
  version: 1,
  created_at: '2026-05-20T01:00:00Z',
  updated_at: '2026-05-20T01:00:00Z',
};

describe('ProjectDetail page', () => {
  afterEach(() => cleanup());

  it('renders header + per-project Issues / Tasks tabs + Fleet link', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/issues', () =>
        HttpResponse.json({
          issues: [
            {
              id: 'IS-1',
              project_id: 'proj-a',
              title: 'login bug',
              description: '',
              status: 'open',
              created_by: 'user:hayang',
              version: 1,
              created_at: '2026-05-24T01:00:00Z',
              updated_at: '2026-05-24T01:00:00Z',
            },
          ],
        }),
      ),
      http.get('/api/projects/proj-a/tasks', () =>
        HttpResponse.json({
          tasks: [
            {
              id: 'TS-1',
              project_id: 'proj-a',
              title: 'rebuild docs',
              description: '',
              status: 'open',
              version: 1,
              created_at: '2026-05-24T01:00:00Z',
              updated_at: '2026-05-24T01:00:00Z',
            },
          ],
        }),
      ),
    );
    wrap('/projects/proj-a');
    // #238: name appears in both the breadcrumb leaf and the header → scope to heading.
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Project Alpha' })).toBeInTheDocument());
    expect(screen.getByTestId('project-description')).toHaveTextContent('the alpha project');
    expect(screen.getByTestId('project-status-active')).toBeInTheDocument();
    // Issues tab is the default; the issue row shows.
    await waitFor(() => expect(screen.getByText('login bug')).toBeInTheDocument());
    // Switch to the Tasks tab to see the task row.
    fireEvent.click(screen.getByTestId('project-tab-tasks'));
    await waitFor(() => expect(screen.getByText('rebuild docs')).toBeInTheDocument());
    expect(screen.getByTestId('project-fleet-link')).toBeInTheDocument();
  });

  it('shows the per-project empty hint when both panels return []', async () => {
    server.use(
      http.get('/api/projects/:id', () =>
        HttpResponse.json({ ...projectAlpha, id: 'proj-empty', name: 'Empty Project', description: '' }),
      ),
      http.get('/api/projects/proj-empty/issues', () => HttpResponse.json({ issues: [] })),
      http.get('/api/projects/proj-empty/tasks', () => HttpResponse.json({ tasks: [] })),
    );
    wrap('/projects/proj-empty');
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Empty Project' })).toBeInTheDocument());
    await waitFor(() =>
      expect(screen.getByTestId('project-issues-panel')).toHaveTextContent(/No issues yet/),
    );
    fireEvent.click(screen.getByTestId('project-tab-tasks'));
    await waitFor(() =>
      expect(screen.getByTestId('project-tasks-panel')).toHaveTextContent(/No tasks yet/),
    );
  });

  it('surfaces a 404 with a friendly error + back link', async () => {
    server.use(
      http.get('/api/projects/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such project' }, { status: 404 }),
      ),
    );
    wrap('/projects/ghost');
    await waitFor(() =>
      expect(screen.getByTestId('project-not-found')).toHaveTextContent(/no such project/),
    );
    expect(screen.getByRole('link', { name: /back to projects/i })).toHaveAttribute(
      'href',
      '/projects',
    );
  });

  it('renders a loading skeleton while the project fetch is pending', () => {
    server.use(
      http.get('/api/projects/:id', async () => {
        await new Promise<void>(() => {});
        return HttpResponse.json({});
      }),
    );
    wrap('/projects/proj-a');
    expect(screen.getByTestId('page-ProjectDetail')).toBeInTheDocument();
  });

  it('renders Members + Code repos tabs reading the pm model (#135)', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/:pid/issues', () => HttpResponse.json({ issues: [] })),
      http.get('/api/projects/:pid/tasks', () => HttpResponse.json({ tasks: [] })),
      http.get('/api/projects/:pid/members', () =>
        HttpResponse.json({
          members: [
            { id: 'M-1', project_id: 'proj-a', identity_id: 'user:hayang', role: 'owner', added_by: 'user:hayang', created_at: '2026-05-20T01:00:00Z' },
          ],
        }),
      ),
      http.get('/api/projects/:pid/code-repos', () =>
        HttpResponse.json({
          code_repos: [
            { id: 'R-1', project_id: 'proj-a', url: 'https://example.com/repo.git', label: 'main repo', added_by: 'user:hayang', created_at: '2026-05-20T01:00:00Z' },
          ],
        }),
      ),
    );
    wrap('/projects/proj-a');
    await waitFor(() => expect(screen.getByTestId('project-work-tabs')).toBeInTheDocument());

    fireEvent.click(screen.getByTestId('project-tab-members'));
    await waitFor(() => expect(screen.getByTestId('member-row')).toBeInTheDocument());
    const memberRow = screen.getByTestId('member-row');
    expect(memberRow).toHaveAttribute('data-member-id', 'M-1');
    expect(memberRow).toHaveTextContent('user:hayang');
    expect(memberRow).toHaveTextContent('owner');

    fireEvent.click(screen.getByTestId('project-tab-repos'));
    await waitFor(() => expect(screen.getByTestId('repo-row')).toBeInTheDocument());
    const repoRow = screen.getByTestId('repo-row');
    expect(repoRow).toHaveAttribute('data-repo-id', 'R-1');
    expect(repoRow).toHaveTextContent('main repo');
  });

  it('owner sees Add + can Remove a non-owner member (#207)', async () => {
    useAppStore.setState({ currentUserId: 'user:hayang' });
    let removed: string | null = null;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/:pid/issues', () => HttpResponse.json({ issues: [] })),
      http.get('/api/projects/:pid/tasks', () => HttpResponse.json({ tasks: [] })),
      http.get('/api/projects/:pid/code-repos', () => HttpResponse.json({ code_repos: [] })),
      http.get('/api/projects/:pid/members', () =>
        HttpResponse.json({
          members: [
            { id: 'M-1', project_id: 'proj-a', identity_id: 'user:hayang', role: 'owner', added_by: 'user:hayang', created_at: '2026-05-20T01:00:00Z' },
            { id: 'M-2', project_id: 'proj-a', identity_id: 'user:bob', role: 'member', added_by: 'user:hayang', created_at: '2026-05-20T01:00:00Z' },
          ],
        }),
      ),
      http.delete('/api/projects/proj-a/members/:identity', ({ params }) => {
        removed = String(params.identity);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap('/projects/proj-a');
    await waitFor(() => expect(screen.getByTestId('project-work-tabs')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('project-tab-members'));
    await waitFor(() => expect(screen.getAllByTestId('member-row')).toHaveLength(2));

    // Owner sees the Add button; only the non-owner row exposes Remove.
    expect(screen.getByTestId('project-add-member-button')).toBeInTheDocument();
    const removeButtons = screen.getAllByTestId('project-member-remove');
    expect(removeButtons).toHaveLength(1);
    expect(removeButtons[0]).toHaveAttribute('data-identity', 'user:bob');

    fireEvent.click(removeButtons[0]);
    await screen.findByTestId('confirm-modal');
    await act(async () => {
      fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    });
    await waitFor(() => expect(removed).toMatch(/bob/));
  });
});

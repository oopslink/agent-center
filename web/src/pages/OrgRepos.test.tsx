// T575 (issue-f980c8de) — OrgRepos workspace page: list, add, delete (confirm),
// and the read-only remote viewer (commits/branches with graceful degrade).
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import OrgRepos from './OrgRepos';
import type { WorkspaceRepo } from '@/api/types';

const repoA: WorkspaceRepo = {
  id: 'repo-1', organization_id: 'org-1', label: 'agent-center', description: 'the monorepo',
  url: 'git@github.com:o/agent-center.git', provider: 'github', default_branch: 'main',
  has_credential: true, reference_count: 2, created_by: 'user:o', created_at: 'x', updated_at: 'x', version: 1,
};

function wrap(initialPath = '/repos') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initialPath]}><OrgRepos /></MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('OrgRepos (T575)', () => {
  it('lists workspace repos with provider badge + used-by count', async () => {
    server.use(http.get('/api/code-repos', () => HttpResponse.json({ repos: [repoA] })));
    wrap();
    const card = await screen.findByTestId('repo-card');
    expect(within(card).getByTestId('repo-card-label')).toHaveTextContent('agent-center');
    expect(within(card).getByTestId('repo-provider-badge')).toHaveTextContent(/github/i);
    expect(within(card).getByTestId('repo-card-usedby')).toHaveTextContent('used by 2 projects');
  });

  it('empty state when there are no repos', async () => {
    server.use(http.get('/api/code-repos', () => HttpResponse.json({ repos: [] })));
    wrap();
    expect(await screen.findByTestId('repos-empty')).toBeInTheDocument();
  });

  it('+ Add repo opens the form modal', async () => {
    server.use(http.get('/api/code-repos', () => HttpResponse.json({ repos: [] })));
    wrap();
    await screen.findByTestId('repos-empty');
    fireEvent.click(screen.getByTestId('repos-add-btn'));
    expect(screen.getByTestId('repo-form-modal')).toBeInTheDocument();
  });

  it('Delete asks for confirmation (mentions reference count) then DELETEs', async () => {
    let deleted = false;
    server.use(
      http.get('/api/code-repos', () => HttpResponse.json({ repos: [repoA] })),
      http.delete('/api/code-repos/repo-1', () => {
        deleted = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('repo-card-delete'));
    const confirm = await screen.findByTestId('confirm-modal');
    expect(confirm).toHaveTextContent(/2 projects/);
    fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(deleted).toBe(true));
  });

  it('View remote opens the viewer; commits render from the (live) API', async () => {
    server.use(
      http.get('/api/code-repos', () => HttpResponse.json({ repos: [repoA] })),
      http.get('/api/code-repos/repo-1/branches', () => HttpResponse.json({ branches: [{ name: 'main', is_default: true }] })),
      http.get('/api/code-repos/repo-1/commits', () => HttpResponse.json({ commits: [{ sha: 'abcdef1234', message: 'first', author: 'o', date: '' }] })),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('repo-card-view'));
    const viewer = await screen.findByTestId('repo-remote-viewer');
    await waitFor(() => expect(within(viewer).getByTestId('repo-remote-commits')).toBeInTheDocument());
    expect(within(viewer).getByTestId('repo-remote-commits')).toHaveTextContent('first');
    expect(within(viewer).getByTestId('repo-remote-commits')).toHaveTextContent('abcdef1');
  });

  it('deep-link /repos?repo=<id> auto-opens that repo\'s detail viewer', async () => {
    server.use(
      http.get('/api/code-repos', () => HttpResponse.json({ repos: [repoA] })),
      http.get('/api/code-repos/repo-1/branches', () => HttpResponse.json({ branches: [{ name: 'main', is_default: true }] })),
      http.get('/api/code-repos/repo-1/commits', () => HttpResponse.json({ commits: [{ sha: 'abcdef1234', message: 'first', author: 'o', date: '' }] })),
    );
    wrap('/repos?repo=repo-1');
    // without clicking View remote, the viewer opens for the linked repo
    const viewer = await screen.findByTestId('repo-remote-viewer');
    await waitFor(() => expect(within(viewer).getByTestId('repo-remote-commits')).toBeInTheDocument());
    expect(within(viewer).getByTestId('repo-remote-commits')).toHaveTextContent('first');
  });

  it('commit list renders the GitHub-style layout: day header, body toggle, copy + browse', async () => {
    const httpRepo: WorkspaceRepo = { ...repoA, url: 'https://github.com/o/agent-center' };
    server.use(
      http.get('/api/code-repos', () => HttpResponse.json({ repos: [httpRepo] })),
      http.get('/api/code-repos/repo-1/branches', () => HttpResponse.json({ branches: [{ name: 'main', is_default: true }] })),
      http.get('/api/code-repos/repo-1/commits', () =>
        HttpResponse.json({
          commits: [
            { sha: 'abcdef1234567', message: 'fix(web): tidy header\n\nbody line explaining why', author: 'agent-center-pd', date: '2026-06-30T03:00:00Z' },
          ],
        }),
      ),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('repo-card-view'));
    const list = await screen.findByTestId('repo-remote-commits');
    // grouped under a "Commits on …" day header
    expect(list).toHaveTextContent(/Commits on/);
    // subject shown, body hidden until toggled
    expect(list).toHaveTextContent('fix(web): tidy header');
    expect(within(list).queryByTestId('repo-commit-body')).toBeNull();
    fireEvent.click(within(list).getByTestId('repo-commit-body-toggle'));
    expect(within(list).getByTestId('repo-commit-body')).toHaveTextContent('body line explaining why');
    // short sha + copy affordance + browse link (http remote → /commit/<sha>)
    expect(list).toHaveTextContent('abcdef1');
    expect(within(list).getByTestId('repo-commit-copy')).toBeInTheDocument();
    expect(within(list).getByTestId('repo-commit-browse')).toHaveAttribute(
      'href',
      'https://github.com/o/agent-center/commit/abcdef1234567',
    );
  });

  it('omits the browse link for a non-http (scp-form) remote', async () => {
    server.use(
      http.get('/api/code-repos', () => HttpResponse.json({ repos: [repoA] })), // url = git@github.com:...
      http.get('/api/code-repos/repo-1/branches', () => HttpResponse.json({ branches: [{ name: 'main', is_default: true }] })),
      http.get('/api/code-repos/repo-1/commits', () =>
        HttpResponse.json({ commits: [{ sha: 'abcdef1234', message: 'first', author: 'o', date: '2026-06-30T03:00:00Z' }] }),
      ),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('repo-card-view'));
    const list = await screen.findByTestId('repo-remote-commits');
    expect(within(list).queryByTestId('repo-commit-browse')).toBeNull();
  });

  it('renders the remote viewer INSIDE its own card (attached, not a detached bottom panel)', async () => {
    server.use(
      http.get('/api/code-repos', () => HttpResponse.json({ repos: [repoA] })),
      http.get('/api/code-repos/repo-1/branches', () => HttpResponse.json({ branches: [{ name: 'main', is_default: true }] })),
      http.get('/api/code-repos/repo-1/commits', () => HttpResponse.json({ commits: [] })),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('repo-card-view'));
    const viewer = await screen.findByTestId('repo-remote-viewer');
    const card = screen.getByTestId('repo-card');
    expect(card).toContainElement(viewer);
  });

  it('opens two repo viewers at once — both stay visible independently', async () => {
    const repoB: WorkspaceRepo = { ...repoA, id: 'repo-2', label: 'other-repo' };
    server.use(
      http.get('/api/code-repos', () => HttpResponse.json({ repos: [repoA, repoB] })),
      http.get('/api/code-repos/repo-1/branches', () => HttpResponse.json({ branches: [{ name: 'main', is_default: true }] })),
      http.get('/api/code-repos/repo-1/commits', () => HttpResponse.json({ commits: [{ sha: 'aaaaaaa1', message: 'from one', author: 'o', date: '' }] })),
      http.get('/api/code-repos/repo-2/branches', () => HttpResponse.json({ branches: [{ name: 'main', is_default: true }] })),
      http.get('/api/code-repos/repo-2/commits', () => HttpResponse.json({ commits: [{ sha: 'bbbbbbb2', message: 'from two', author: 'o', date: '' }] })),
    );
    wrap();
    const views = await screen.findAllByTestId('repo-card-view');
    expect(views).toHaveLength(2);
    fireEvent.click(views[0]);
    fireEvent.click(views[1]);
    await waitFor(() => expect(screen.getAllByTestId('repo-remote-viewer')).toHaveLength(2));
    await waitFor(() => expect(screen.getByText('from one')).toBeInTheDocument());
    expect(screen.getByText('from two')).toBeInTheDocument();
  });

  it('remote viewer degrades gracefully when BE-2 viewing is unavailable', async () => {
    server.use(
      http.get('/api/code-repos', () => HttpResponse.json({ repos: [repoA] })),
      http.get('/api/code-repos/repo-1/branches', () => HttpResponse.json({ message: 'not wired' }, { status: 501 })),
      http.get('/api/code-repos/repo-1/commits', () => HttpResponse.json({ message: 'not wired' }, { status: 501 })),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('repo-card-view'));
    expect(await screen.findByTestId('repo-remote-unavailable')).toBeInTheDocument();
  });
});

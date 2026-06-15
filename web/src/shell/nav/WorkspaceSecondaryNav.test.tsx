import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import WorkspaceSecondaryNav from './WorkspaceSecondaryNav';

// v2.10.0 [T4] — the Workspace col② route-aware nav: top-level nav (Projects/
// Issues/Tasks/Plan) when not in a project, project sub-nav (Issues/Tasks/Work
// Board/Members/Code repos + back) when inside one. Rendered in isolation with
// an empty orgBase (matches the shell's isolated-test convention).
function renderAt(initial: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route path="*" element={<WorkspaceSecondaryNav orgBase="" />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('WorkspaceSecondaryNav (v2.10.0 [T4])', () => {
  afterEach(() => cleanup());

  it('top-level: shows Projects / Issues / Tasks / Plans links when not in a project', () => {
    renderAt('/projects');
    const nav = screen.getByTestId('workspace-nav-toplevel');
    expect(screen.queryByTestId('workspace-nav-project')).not.toBeInTheDocument();
    const expected: Array<[string, string]> = [
      ['Projects', '/projects'],
      ['Issues', '/issues'],
      ['Tasks', '/tasks'],
      // v2.10.2 [T142]: pluralized "Plan" → "Plans".
      ['Plans', '/plans'],
    ];
    for (const [label, href] of expected) {
      const link = within(nav)
        .getAllByRole('link')
        .find((a) => a.textContent?.trim() === label);
      expect(link, `link ${label}`).toBeDefined();
      expect(link).toHaveAttribute('href', href);
    }
  });

  it('inside a project: shows the project sub-nav (tabs as ?tab=, Work Board, back) + project name', async () => {
    server.use(
      http.get('/api/projects/proj-a', () =>
        HttpResponse.json({
          id: 'proj-a',
          organization_id: 'org-test',
          name: 'agent-center2',
          description: '',
          status: 'active',
          created_by: 'user:x',
          version: 1,
          created_at: '2026-06-01T00:00:00Z',
          updated_at: '2026-06-01T00:00:00Z',
        }),
      ),
    );
    renderAt('/projects/proj-a?tab=tasks');
    const nav = await screen.findByTestId('workspace-nav-project');
    expect(nav).toHaveAttribute('data-project-id', 'proj-a');
    expect(screen.queryByTestId('workspace-nav-toplevel')).not.toBeInTheDocument();
    // project name resolves from useProject.
    await waitFor(() => expect(nav).toHaveTextContent('agent-center2'));
    // sub-nav tab links carry ?tab=.
    expect(screen.getByTestId('project-subnav-issues')).toHaveAttribute('href', '/projects/proj-a?tab=issues');
    expect(screen.getByTestId('project-subnav-tasks')).toHaveAttribute('href', '/projects/proj-a?tab=tasks');
    expect(screen.getByTestId('project-subnav-members')).toHaveAttribute('href', '/projects/proj-a?tab=members');
    expect(screen.getByTestId('project-subnav-repos')).toHaveAttribute('href', '/projects/proj-a?tab=repos');
    // Work Board → the per-project plan board route.
    expect(screen.getByTestId('project-subnav-workboard')).toHaveAttribute('href', '/projects/proj-a/plans');
    // back → the Projects list.
    expect(screen.getByTestId('project-subnav-back')).toHaveAttribute('href', '/projects');
    // active tab (?tab=tasks) marked.
    expect(screen.getByTestId('project-subnav-tasks')).toHaveAttribute('aria-current', 'page');
    expect(screen.getByTestId('project-subnav-issues')).not.toHaveAttribute('aria-current', 'page');
  });

  it('inside a project on the Work Board route: marks Work Board active', async () => {
    server.use(
      http.get('/api/projects/proj-a', () =>
        HttpResponse.json({
          id: 'proj-a', organization_id: 'o', name: 'P', description: '', status: 'active',
          created_by: 'u', version: 1, created_at: '2026-06-01T00:00:00Z', updated_at: '2026-06-01T00:00:00Z',
        }),
      ),
    );
    renderAt('/projects/proj-a/plans');
    await screen.findByTestId('workspace-nav-project');
    expect(screen.getByTestId('project-subnav-workboard')).toHaveAttribute('aria-current', 'page');
    expect(screen.getByTestId('project-subnav-issues')).not.toHaveAttribute('aria-current', 'page');
  });

  it('the bare /projects list is NOT treated as inside a project', () => {
    renderAt('/projects');
    expect(screen.getByTestId('workspace-nav-toplevel')).toBeInTheDocument();
    expect(screen.queryByTestId('workspace-nav-project')).not.toBeInTheDocument();
  });
});

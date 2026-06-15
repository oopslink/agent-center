import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import Projects from './Projects';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Projects page', () => {
  afterEach(() => cleanup());

  it('renders project rows with name + status badge', async () => {
    server.use(
      http.get('/api/projects', () =>
        HttpResponse.json({
          projects: [
            {
              id: 'proj-a',
              organization_id: 'org-test',
              name: 'Project Alpha',
              description: 'first',
              status: 'active',
              created_by: 'user:hayang',
              version: 1,
              created_at: '2026-05-20T01:00:00Z',
              updated_at: '2026-05-20T01:00:00Z',
            },
            {
              id: 'proj-b',
              organization_id: 'org-test',
              name: 'Project Beta',
              description: 'second',
              status: 'archived',
              created_by: 'user:hayang',
              version: 1,
              created_at: '2026-05-21T01:00:00Z',
              updated_at: '2026-05-21T01:00:00Z',
            },
          ],
        }),
      ),
    );
    wrap(<Projects />);
    await waitFor(() => expect(screen.getAllByTestId('project-row')).toHaveLength(2));
    expect(screen.getByText('Project Alpha')).toBeInTheDocument();
    expect(screen.getByText('Project Beta')).toBeInTheDocument();
    expect(screen.getByTestId('project-status-active')).toBeInTheDocument();
    expect(screen.getByTestId('project-status-archived')).toBeInTheDocument();
    const links = screen.getAllByRole('link');
    expect(links.some((a) => a.getAttribute('href') === '/projects/proj-a')).toBe(true);
  });

  // v2.10.0 #T81 (§3.4.1, finding D1): list cards show per-project counts
  // (tasks/issues/plans/repos). Counts come from the LIST response; a card
  // whose counts are absent renders no count row (single-1 pluralization too).
  it('renders per-project task/issue/plan/repo counts', async () => {
    server.use(
      http.get('/api/projects', () =>
        HttpResponse.json({
          projects: [
            {
              id: 'proj-a',
              organization_id: 'org-test',
              name: 'Project Alpha',
              description: 'first',
              status: 'active',
              created_by: 'user:hayang',
              version: 1,
              created_at: '2026-05-20T01:00:00Z',
              updated_at: '2026-05-20T01:00:00Z',
              task_count: 12,
              issue_count: 1,
              plan_count: 4,
              repo_count: 0,
            },
            {
              // No counts (e.g. an older payload) → no count row on this card.
              id: 'proj-b',
              organization_id: 'org-test',
              name: 'Project Beta',
              description: 'second',
              status: 'active',
              created_by: 'user:hayang',
              version: 1,
              created_at: '2026-05-21T01:00:00Z',
              updated_at: '2026-05-21T01:00:00Z',
            },
          ],
        }),
      ),
    );
    wrap(<Projects />);
    await waitFor(() => expect(screen.getAllByTestId('project-row')).toHaveLength(2));

    // proj-a: all four counts, with singular/plural handling.
    expect(screen.getAllByTestId('project-counts')).toHaveLength(1); // only proj-a
    expect(screen.getByTestId('project-count-tasks')).toHaveTextContent('12 tasks');
    expect(screen.getByTestId('project-count-issues')).toHaveTextContent('1 issue');
    expect(screen.getByTestId('project-count-plans')).toHaveTextContent('4 plans');
    expect(screen.getByTestId('project-count-repos')).toHaveTextContent('0 repos');
  });

  it('shows the EmptyState when the list is empty', async () => {
    server.use(http.get('/api/projects', () => HttpResponse.json({ projects: [] })));
    wrap(<Projects />);
    await waitFor(() => expect(screen.getByTestId('projects-empty')).toBeInTheDocument());
    expect(screen.getByTestId('projects-empty')).toHaveTextContent(/Add Project/);
  });

  it('renders the Skeleton while loading', () => {
    server.use(
      http.get('/api/projects', async () => {
        // Never-resolving delay so the loading branch stays alive.
        await new Promise<void>(() => {});
        return HttpResponse.json({ projects: [] });
      }),
    );
    wrap(<Projects />);
    expect(screen.getByTestId('projects-loading')).toBeInTheDocument();
  });

  // v2.9 #298: the "已归档" / Archived group is collapsed by default; the
  // archived list must NOT be fetched until the toggle is expanded.
  it('archived group is collapsed by default and does not fetch archived', async () => {
    let archivedFetched = false;
    server.use(
      http.get('/api/projects', ({ request }) => {
        const status = new URL(request.url).searchParams.get('status');
        if (status === 'archived') {
          archivedFetched = true;
          return HttpResponse.json({ projects: [] });
        }
        return HttpResponse.json({
          projects: [
            { id: 'proj-a', name: 'Project Alpha', status: 'active', created_at: '2026-05-20T01:00:00Z' },
          ],
        });
      }),
    );
    wrap(<Projects />);
    await waitFor(() => expect(screen.getByTestId('projects-list')).toBeInTheDocument());
    // Toggle present, group body NOT rendered, archived endpoint untouched.
    expect(screen.getByTestId('archived-projects-toggle')).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByTestId('archived-projects-body')).not.toBeInTheDocument();
    // active list unaffected: exactly the one active row.
    expect(screen.getAllByTestId('project-row')).toHaveLength(1);
    expect(archivedFetched).toBe(false);
  });

  // Expanding the group fetches + lists the archived projects (read-only),
  // without disturbing the active list above it.
  it('expanding the archived group fetches and lists archived projects', async () => {
    server.use(
      http.get('/api/projects', ({ request }) => {
        const status = new URL(request.url).searchParams.get('status');
        if (status === 'archived') {
          return HttpResponse.json({
            projects: [
              { id: 'proj-z', name: 'Project Zeta', status: 'archived', created_at: '2026-04-01T01:00:00Z' },
            ],
          });
        }
        return HttpResponse.json({
          projects: [
            { id: 'proj-a', name: 'Project Alpha', status: 'active', created_at: '2026-05-20T01:00:00Z' },
          ],
        });
      }),
    );
    wrap(<Projects />);
    await waitFor(() => expect(screen.getByTestId('projects-list')).toBeInTheDocument());
    expect(screen.getAllByTestId('project-row')).toHaveLength(1);

    fireEvent.click(screen.getByTestId('archived-projects-toggle'));
    await waitFor(() =>
      expect(screen.getByTestId('archived-projects-list')).toBeInTheDocument(),
    );
    expect(screen.getByText('Project Zeta')).toBeInTheDocument();
    expect(screen.getByTestId('project-status-archived')).toBeInTheDocument();
    expect(screen.getByTestId('archived-projects-toggle')).toHaveAttribute('aria-expanded', 'true');
    // Active list (Alpha) still rendered, unaffected: 1 active + 1 archived row.
    expect(screen.getByText('Project Alpha')).toBeInTheDocument();
    expect(screen.getAllByTestId('project-row')).toHaveLength(2);
  });

  // Empty archived set → a quiet note, not a crash.
  it('expanding shows the empty note when no archived projects', async () => {
    server.use(
      http.get('/api/projects', ({ request }) => {
        const status = new URL(request.url).searchParams.get('status');
        if (status === 'archived') return HttpResponse.json({ projects: [] });
        return HttpResponse.json({
          projects: [
            { id: 'proj-a', name: 'Project Alpha', status: 'active', created_at: '2026-05-20T01:00:00Z' },
          ],
        });
      }),
    );
    wrap(<Projects />);
    await waitFor(() => expect(screen.getByTestId('projects-list')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('archived-projects-toggle'));
    await waitFor(() =>
      expect(screen.getByTestId('archived-projects-empty')).toBeInTheDocument(),
    );
    expect(screen.queryByTestId('archived-projects-list')).not.toBeInTheDocument();
  });

  it('surfaces API error', async () => {
    server.use(
      http.get('/api/projects', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap(<Projects />);
    await waitFor(() => expect(screen.getByTestId('projects-error')).toHaveTextContent(/db down/));
  });
});

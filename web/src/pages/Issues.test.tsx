import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import Issues from './Issues';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(ui: React.ReactElement, initialEntries: string[] = ['/issues']) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

// v2.3-5b cutover: page reads from the BC-native /api/issues endpoint
// with an OPTIONAL project_id query param. v2.5.15 (#68) made
// project_id optional server-side so the "All projects" filter shows
// the cross-project list instead of a "pick a project" nudge.

const issuesHandler = http.get('/api/issues', ({ request }) => {
  const url = new URL(request.url);
  const projectId = url.searchParams.get('project_id');
  const status = url.searchParams.get('status');
  const all = [
    {
      id: 'IS-1',
      project_id: 'proj-a',
      conversation_id: 'I-1',
      title: 'login bug',
      status: 'open',
      opened_at: '2026-05-24T01:00:00Z',
      opener: 'user:hayang',
    },
    {
      id: 'IS-2',
      project_id: 'proj-a',
      conversation_id: 'I-2',
      title: 'sso flake',
      status: 'withdrawn',
      opened_at: '2026-05-22T01:00:00Z',
      opener: 'user:hayang',
    },
    {
      id: 'IS-9',
      project_id: 'proj-b',
      conversation_id: 'I-9',
      title: 'other-project issue',
      status: 'open',
      opened_at: '2026-05-24T01:00:00Z',
      opener: 'user:hayang',
    },
  ];
  return HttpResponse.json(
    all.filter(
      (iss) =>
        (projectId === null || iss.project_id === projectId) &&
        (status === null || iss.status === status),
    ),
  );
});

const projectsHandler = http.get('/api/projects', () =>
  HttpResponse.json([
    {
      id: 'proj-a',
      name: 'Project Alpha',
      kind: 'coding',
      created_at: '2026-05-20T01:00:00Z',
      updated_at: '2026-05-20T01:00:00Z',
    },
    {
      id: 'proj-b',
      name: 'Project Beta',
      kind: 'coding',
      created_at: '2026-05-20T01:00:00Z',
      updated_at: '2026-05-20T01:00:00Z',
    },
  ]),
);

describe('Issues page', () => {
  afterEach(() => cleanup());

  // v2.5.15 (#68): no-project-picked view shows issues across every
  // project + a per-row project chip column, instead of the prior
  // "Pick a project" empty-state nudge. PM #agent-center:16013eff bug
  // B2.
  it('shows issues from every project when no project is selected', async () => {
    server.use(projectsHandler, issuesHandler);
    wrap(<Issues />);
    await waitFor(() => expect(screen.getAllByTestId('issue-row')).toHaveLength(3));
    expect(screen.getByText('login bug')).toBeInTheDocument();
    expect(screen.getByText('other-project issue')).toBeInTheDocument();
    const tags = screen.getAllByTestId('issue-row-project');
    const labels = tags.map((el) => el.textContent);
    expect(labels).toContain('Project Alpha');
    expect(labels).toContain('Project Beta');
  });

  it('renders the project issues when the project chip is selected via URL', async () => {
    server.use(projectsHandler, issuesHandler);
    wrap(<Issues />, ['/issues?project=proj-a']);
    await waitFor(() => expect(screen.getAllByTestId('issue-row')).toHaveLength(2));
    expect(screen.getByText('login bug')).toBeInTheDocument();
    expect(screen.getByText('sso flake')).toBeInTheDocument();
    // Cross-project issue must NOT appear — project chip filter is now
    // real, not cosmetic.
    expect(screen.queryByText('other-project issue')).not.toBeInTheDocument();
  });

  it('project chip click narrows the list to a different project', async () => {
    server.use(projectsHandler, issuesHandler);
    wrap(<Issues />, ['/issues?project=proj-a']);
    await waitFor(() => expect(screen.getAllByTestId('issue-row')).toHaveLength(2));
    fireEvent.click(screen.getByRole('tab', { name: /Project Beta/i }));
    await waitFor(() =>
      expect(screen.getByText('other-project issue')).toBeInTheDocument(),
    );
    expect(screen.queryByText('login bug')).not.toBeInTheDocument();
  });

  it('status tab narrows to a single status (server-side filter)', async () => {
    server.use(projectsHandler, issuesHandler);
    wrap(<Issues />, ['/issues?project=proj-a']);
    await waitFor(() => expect(screen.getAllByTestId('issue-row')).toHaveLength(2));
    fireEvent.click(screen.getByRole('tab', { name: /^withdrawn$/i }));
    await waitFor(() => expect(screen.getAllByTestId('issue-row')).toHaveLength(1));
    expect(screen.getByText('sso flake')).toBeInTheDocument();
  });

  it('empty state shows when status filter has no matches', async () => {
    server.use(projectsHandler, issuesHandler);
    wrap(<Issues />, ['/issues?project=proj-a']);
    await waitFor(() => expect(screen.getAllByTestId('issue-row')).toHaveLength(2));
    fireEvent.click(screen.getByRole('tab', { name: /^concluded$/i }));
    await waitFor(() => expect(screen.getByTestId('issues-empty')).toBeInTheDocument());
  });

  it('surfaces API error from the BC-native endpoint', async () => {
    server.use(
      projectsHandler,
      http.get('/api/issues', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap(<Issues />, ['/issues?project=proj-a']);
    await waitFor(() => expect(screen.getByTestId('issues-error')).toHaveTextContent(/db down/));
  });
});

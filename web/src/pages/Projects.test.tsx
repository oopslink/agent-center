import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
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

  it('renders project rows with name, kind chip, and agent CLI badge', async () => {
    server.use(
      http.get('/api/projects', () =>
        HttpResponse.json([
          {
            id: 'proj-a',
            name: 'Project Alpha',
            kind: 'coding',
            default_agent_cli: 'claudecode',
            description: 'first',
            created_at: '2026-05-20T01:00:00Z',
            updated_at: '2026-05-20T01:00:00Z',
          },
          {
            id: 'proj-b',
            name: 'Project Beta',
            kind: 'writing',
            description: 'second',
            created_at: '2026-05-21T01:00:00Z',
            updated_at: '2026-05-21T01:00:00Z',
          },
        ]),
      ),
    );
    wrap(<Projects />);
    await waitFor(() => expect(screen.getAllByTestId('project-row')).toHaveLength(2));
    expect(screen.getByText('Project Alpha')).toBeInTheDocument();
    expect(screen.getByText('Project Beta')).toBeInTheDocument();
    expect(screen.getByText('claudecode')).toBeInTheDocument();
    // Row Link wraps the content; check href on the anchor.
    const links = screen.getAllByRole('link');
    expect(links.some((a) => a.getAttribute('href') === '/projects/proj-a')).toBe(true);
  });

  it('shows the EmptyState when the list is empty', async () => {
    server.use(http.get('/api/projects', () => HttpResponse.json([])));
    wrap(<Projects />);
    await waitFor(() => expect(screen.getByTestId('projects-empty')).toBeInTheDocument());
    expect(screen.getByTestId('projects-empty')).toHaveTextContent(/Add Project/);
  });

  it('renders the Skeleton while loading', () => {
    server.use(
      http.get('/api/projects', async () => {
        // Never-resolving delay so the loading branch stays alive.
        await new Promise<void>(() => {});
        return HttpResponse.json([]);
      }),
    );
    wrap(<Projects />);
    expect(screen.getByTestId('projects-loading')).toBeInTheDocument();
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

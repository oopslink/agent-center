import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
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

describe('ProjectDetail page', () => {
  afterEach(() => cleanup());

  it('renders the header + Issues / Tasks / Fleet panels', async () => {
    server.use(
      http.get('/api/projects/:id', () =>
        HttpResponse.json({
          id: 'proj-a',
          name: 'Project Alpha',
          kind: 'coding',
          default_agent_cli: 'claudecode',
          description: 'the alpha project',
          created_at: '2026-05-20T01:00:00Z',
          updated_at: '2026-05-20T01:00:00Z',
        }),
      ),
      http.get('/api/conversations', ({ request }) => {
        const url = new URL(request.url);
        const kind = url.searchParams.get('kind');
        if (kind === 'issue') {
          return HttpResponse.json([
            { id: 'I-1', kind: 'issue', name: 'login bug', status: 'active' },
          ]);
        }
        if (kind === 'task') {
          return HttpResponse.json([
            { id: 'T-1', kind: 'task', name: 'rebuild docs', status: 'active' },
          ]);
        }
        return HttpResponse.json([]);
      }),
    );
    wrap('/projects/proj-a');
    await waitFor(() => expect(screen.getByText('Project Alpha')).toBeInTheDocument());
    expect(screen.getByTestId('project-description')).toHaveTextContent('the alpha project');
    expect(screen.getByTestId('project-default-agent-cli')).toHaveTextContent('claudecode');
    await waitFor(() => expect(screen.getByText('login bug')).toBeInTheDocument());
    expect(screen.getByText('rebuild docs')).toBeInTheDocument();
    expect(screen.getByTestId('project-fleet-link')).toBeInTheDocument();
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
});

import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import ProjectPlans from './ProjectPlans';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function stubMatchMedia(matches: boolean): void {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  }));
}

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/projects/:id/plans" element={<ProjectPlans />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Work Board mobile (v2.10.1 M5)', () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it('renders a mobile FAB that opens the New Plan modal', async () => {
    server.use(http.get('/api/projects/:id', () => HttpResponse.json({ id: 'proj-a', name: 'Project Alpha' })));
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());
    const fab = screen.getByTestId('workboard-fab');
    expect(fab).toBeInTheDocument();
    fireEvent.click(fab);
    expect(screen.getByTestId('plan-create-modal')).toBeInTheDocument();
  });

  it('the board scroll container is snap-enabled for portrait column browsing', async () => {
    server.use(http.get('/api/projects/:id', () => HttpResponse.json({ id: 'proj-a', name: 'Project Alpha' })));
    wrap('/projects/proj-a/plans');
    const board = await screen.findByTestId('work-board');
    expect(board.className).toContain('snap-mandatory');
    expect(board.className).toContain('md:snap-none');
  });

  it('shows the rotate-to-landscape hint in mobile portrait, dismissible', async () => {
    stubMatchMedia(true); // portrait & small screen
    server.use(http.get('/api/projects/:id', () => HttpResponse.json({ id: 'proj-a', name: 'Project Alpha' })));
    wrap('/projects/proj-a/plans');
    const hint = await screen.findByTestId('workboard-rotate-hint');
    expect(hint).toHaveTextContent(/rotate to landscape/i);
    fireEvent.click(screen.getByTestId('workboard-rotate-dismiss'));
    expect(screen.queryByTestId('workboard-rotate-hint')).toBeNull();
  });

  it('hides the rotate hint when not in portrait (e.g. desktop / landscape)', async () => {
    stubMatchMedia(false);
    server.use(http.get('/api/projects/:id', () => HttpResponse.json({ id: 'proj-a', name: 'Project Alpha' })));
    wrap('/projects/proj-a/plans');
    await screen.findByTestId('work-board');
    expect(screen.queryByTestId('workboard-rotate-hint')).toBeNull();
  });
});

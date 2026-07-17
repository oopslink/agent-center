// The mobile ⓘ → col④ bottom-sheet entry point, driven through the REAL pages
// inside the REAL shell (AppLayout).
//
// The sheet mechanism (BottomSheet + useContextPanelMobileTrigger) shipped with
// no caller: the detail pages mount their <ContextPanel> for mobile only, its
// content portals into a sheet that starts closed, and nothing ever opened it —
// so on a phone the participants/threads/files panel was unreachable. A test
// that renders its own harness page cannot catch that regression, so these
// mount the actual TaskDetail / IssueDetail / PlanDetail under AppLayout and
// drive the button a thumb would actually tap.
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import AppLayout from '../AppLayout';
import TaskDetail from './TaskDetail';
import IssueDetail from './IssueDetail';
import PlanDetail from './PlanDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

/** jsdom has no viewport media — opt the tree into the <768px branch. */
function stubViewport(mobile: boolean): void {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query.includes('max-width: 767px') ? mobile : false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  }));
}

function renderInShell(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/projects/:projectId/tasks/:id" element={<TaskDetail />} />
            <Route path="/projects/:projectId/issues/:id" element={<IssueDetail />} />
            <Route path="/projects/:id/plans/:planId" element={<PlanDetail />} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const TASK_PATH = '/projects/proj-a/tasks/TS-1';
const ISSUE_PATH = '/projects/proj-a/issues/IS-1';
const PLAN_PATH = '/projects/proj-a/plans/PL-1';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('mobile ⓘ opens the col④ context sheet — TaskDetail', () => {
  it('renders the ⓘ in the title row and reveals the conversation sidebar on tap', async () => {
    stubViewport(true);
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () =>
        HttpResponse.json({
          id: 'TS-1', project_id: 'proj-a', title: 'rebuild docs', description: 'regenerate',
          status: 'running', version: 1, org_ref: 'T7',
          created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T01:00:00Z',
        }),
      ),
    );
    renderInShell(TASK_PATH);

    // The ⓘ only exists once the page's conversation resolved (same gate as the
    // <ContextPanel>) — so this also proves the button isn't offering an empty sheet.
    const info = await screen.findByTestId('context-panel-mobile-open');
    expect(info).toBeInTheDocument();

    // Closed until tapped: the panel content has no host, so nothing is on screen.
    expect(screen.queryByTestId('mobile-context-panel-sheet')).toBeNull();
    expect(screen.queryByTestId('conversation-sidebar')).toBeNull();

    fireEvent.click(info);

    // The sheet opens and the page's own col④ content is inside it.
    const sheet = await screen.findByTestId('mobile-context-panel-sheet');
    await waitFor(() =>
      expect(within(sheet).getByTestId('conversation-sidebar')).toBeInTheDocument(),
    );
  });

  it('desktop (≥md): no ⓘ — col④ is a real column, the sheet is not used', async () => {
    stubViewport(false);
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () =>
        HttpResponse.json({
          id: 'TS-1', project_id: 'proj-a', title: 'rebuild docs', description: 'regenerate',
          status: 'running', version: 1, org_ref: 'T7',
          created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T01:00:00Z',
        }),
      ),
    );
    renderInShell(TASK_PATH);
    await screen.findByTestId('page-TaskDetail');
    expect(screen.queryByTestId('context-panel-mobile-open')).toBeNull();
  });
});

describe('mobile ⓘ opens the col④ context sheet — IssueDetail', () => {
  it('renders the ⓘ and reveals the conversation sidebar on tap', async () => {
    stubViewport(true);
    server.use(
      http.get('/api/projects/proj-a/issues/:id', () =>
        HttpResponse.json({
          id: 'IS-1', project_id: 'proj-a', title: 'flaky login', description: 'sometimes 500s',
          status: 'open', version: 1, org_ref: 'I3',
          created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T01:00:00Z',
        }),
      ),
    );
    renderInShell(ISSUE_PATH);

    const info = await screen.findByTestId('context-panel-mobile-open');
    expect(screen.queryByTestId('conversation-sidebar')).toBeNull();

    fireEvent.click(info);

    const sheet = await screen.findByTestId('mobile-context-panel-sheet');
    await waitFor(() =>
      expect(within(sheet).getByTestId('conversation-sidebar')).toBeInTheDocument(),
    );
  });
});

describe('mobile ⓘ opens the col④ context sheet — PlanDetail', () => {
  it('renders the ⓘ in the plan header and reveals the conversation sidebar on tap', async () => {
    stubViewport(true);
    renderInShell(PLAN_PATH);

    const info = await screen.findByTestId('context-panel-mobile-open');
    expect(screen.queryByTestId('conversation-sidebar')).toBeNull();

    fireEvent.click(info);

    const sheet = await screen.findByTestId('mobile-context-panel-sheet');
    await waitFor(() =>
      expect(within(sheet).getByTestId('conversation-sidebar')).toBeInTheDocument(),
    );
  });
});

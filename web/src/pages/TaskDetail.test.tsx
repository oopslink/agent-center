import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import TaskDetail from './TaskDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/tasks/:id" element={<TaskDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('TaskDetail page', () => {
  afterEach(() => cleanup());

  it('renders header + messages + composer + trace link', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'T-1',
          kind: 'task',
          name: 'rebuild docs',
          status: 'active',
          participants: [
            { identity_id: 'agent:bot-1', role: 'owner', joined_at: 'x', joined_by: 'agent:bot-1' },
          ],
        }),
      ),
      http.get('/api/conversations/:id/messages', () =>
        HttpResponse.json([
          {
            id: 'M-1',
            conversation_id: 'T-1',
            sender_identity_id: 'agent:bot-1',
            content_kind: 'text',
            content: 'starting work',
            direction: 'inbound',
            posted_at: '2026-05-24T01:00:00Z',
          },
        ]),
      ),
    );
    wrap('/tasks/T-1');
    await waitFor(() => expect(screen.getByText('starting work')).toBeInTheDocument());
    expect(screen.getByText('rebuild docs')).toBeInTheDocument();
    expect(screen.getByTestId('task-view-trace')).toHaveAttribute('href', '/tasks/T-1/trace');
    expect(screen.getByTestId('message-composer')).toBeInTheDocument();
  });

  it('surfaces task lookup error', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such task' }, { status: 404 }),
      ),
    );
    wrap('/tasks/missing');
    await waitFor(() => expect(screen.getByTestId('task-not-found')).toHaveTextContent(/no such task/));
  });

  // v2.1-B: cover the optional-description render of TaskDetail.tsx
  // (lines 57-58 — `conv.data.description && <p>...</p>`). F14 audit
  // logged as "🟡 worth covering — pass description in seed".
  it('renders the task description when set on the bound conversation', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'T-2',
          kind: 'task',
          name: 'investigate auth',
          description: 'auth flow occasionally returns 401 after refresh',
          status: 'active',
          participants: [
            { identity_id: 'agent:bot-1', role: 'owner', joined_at: 'x', joined_by: 'agent:bot-1' },
          ],
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
    );
    wrap('/tasks/T-2');
    await waitFor(() =>
      expect(screen.getByText(/auth flow occasionally returns 401/)).toBeInTheDocument(),
    );
    // Flip select mode so the truthy arm of the select-mode-toggle
    // ternary className (line 71) is exercised. F14 audit grouped this
    // tool-quirk branch with the description render.
    const toggle = screen.getByTestId('select-mode-toggle');
    toggle.click();
    await waitFor(() => expect(toggle).toHaveAttribute('aria-pressed', 'true'));
  });
});

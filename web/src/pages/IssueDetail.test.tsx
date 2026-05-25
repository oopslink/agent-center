import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import IssueDetail from './IssueDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/issues/:id" element={<IssueDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// v2.3-5b: route param is now the ISSUE_ID (Discussion BC), not the
// conversation_id. Detail page fetches the Issue projection first
// then uses its conversation_id to fetch the message thread from
// Conversation BC.

describe('IssueDetail page', () => {
  afterEach(() => cleanup());

  it('renders header from the Issue projection + messages from the bound conversation', async () => {
    server.use(
      http.get('/api/issues/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          project_id: 'proj-a',
          conversation_id: 'I-conv-1',
          title: 'login bug',
          status: 'open',
          opened_at: '2026-05-24T01:00:00Z',
          opener: 'user:hayang',
        }),
      ),
      http.get('/api/conversations/I-conv-1', () =>
        HttpResponse.json({
          id: 'I-conv-1',
          kind: 'issue',
          name: 'login bug',
          status: 'active',
          participants: [
            {
              identity_id: 'user:hayang',
              role: 'owner',
              joined_at: '2026-05-24T00:00:00Z',
              joined_by: 'user:hayang',
            },
          ],
        }),
      ),
      http.get('/api/conversations/I-conv-1/messages', () =>
        HttpResponse.json([
          {
            id: 'M-own',
            conversation_id: 'I-conv-1',
            sender_identity_id: 'user:hayang',
            content_kind: 'text',
            content: 'native discussion',
            direction: 'inbound',
            posted_at: '2026-05-24T01:00:00Z',
          },
        ]),
      ),
      http.get('/api/conversations/I-conv-1/refs', () => HttpResponse.json([])),
    );
    wrap('/issues/IS-1');
    await waitFor(() => expect(screen.getByText('native discussion')).toBeInTheDocument());
    expect(screen.getByText('login bug')).toBeInTheDocument();
    expect(screen.getByText(/opened/i)).toBeInTheDocument();
    expect(screen.getByTestId('issue-project-link')).toHaveAttribute(
      'href',
      '/projects/proj-a',
    );
    expect(screen.getByTestId('message-composer')).toBeInTheDocument();
    expect(screen.getByTestId('participants-panel')).toBeInTheDocument();
  });

  it('renders the carry-over divider when refs + source messages are present', async () => {
    server.use(
      http.get('/api/issues/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          project_id: 'proj-a',
          conversation_id: 'I-2',
          title: 'follow up',
          status: 'open',
          opened_at: '2026-05-24T01:00:00Z',
          opener: 'user:hayang',
        }),
      ),
      http.get('/api/conversations/:id', ({ params }) =>
        HttpResponse.json({
          id: params.id,
          kind: 'issue',
          name: 'follow up',
          status: 'active',
          participants: [],
        }),
      ),
      http.get('/api/conversations/:id/messages', ({ params }) => {
        if (params.id === 'I-2') {
          return HttpResponse.json([
            {
              id: 'M-child',
              conversation_id: 'I-2',
              sender_identity_id: 'user:hayang',
              content_kind: 'text',
              content: 'continuing in child',
              direction: 'inbound',
              posted_at: '2026-05-24T01:01:00Z',
            },
          ]);
        }
        // Source conv (C-SRC).
        return HttpResponse.json([
          {
            id: 'M-src',
            conversation_id: 'C-SRC',
            sender_identity_id: 'user:hayang',
            content_kind: 'text',
            content: 'carried snippet',
            direction: 'inbound',
            posted_at: '2026-05-24T00:30:00Z',
          },
        ]);
      }),
      http.get('/api/conversations/I-2/refs', () =>
        HttpResponse.json([
          {
            id: 'R-1',
            child_conversation_id: 'I-2',
            source_conversation_id: 'C-SRC',
            source_message_id: 'M-src',
            created_by: 'user:hayang',
            created_at: '2026-05-24T00:30:01Z',
          },
        ]),
      ),
    );
    wrap('/issues/IS-2');
    await waitFor(() => expect(screen.getByText('carried snippet')).toBeInTheDocument());
    expect(screen.getByTestId('carry-over-divider')).toBeInTheDocument();
    expect(screen.getByText('continuing in child')).toBeInTheDocument();
  });

  it('surfaces issue lookup error', async () => {
    server.use(
      http.get('/api/issues/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such issue' }, { status: 404 }),
      ),
    );
    wrap('/issues/missing');
    await waitFor(() =>
      expect(screen.getByTestId('issue-not-found')).toHaveTextContent(/no such issue/),
    );
  });
});

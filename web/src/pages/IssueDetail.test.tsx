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

describe('IssueDetail page', () => {
  afterEach(() => cleanup());

  it('renders header + messages + composer + participants', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'I-1',
          kind: 'issue',
          name: 'login bug',
          status: 'active',
          description: 'cookie not set',
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
      http.get('/api/conversations/I-1/messages', () =>
        HttpResponse.json([
          {
            id: 'M-own',
            conversation_id: 'I-1',
            sender_identity_id: 'user:hayang',
            content_kind: 'text',
            content: 'native discussion',
            direction: 'inbound',
            posted_at: '2026-05-24T01:00:00Z',
          },
        ]),
      ),
      http.get('/api/conversations/I-1/refs', () => HttpResponse.json([])),
    );
    wrap('/issues/I-1');
    await waitFor(() => expect(screen.getByText('native discussion')).toBeInTheDocument());
    expect(screen.getByText('login bug')).toBeInTheDocument();
    expect(screen.getByText(/cookie not set/)).toBeInTheDocument();
    expect(screen.getByTestId('message-composer')).toBeInTheDocument();
    expect(screen.getByTestId('participants-panel')).toBeInTheDocument();
  });

  it('renders the carry-over divider when refs + source messages are present', async () => {
    server.use(
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
    wrap('/issues/I-2');
    await waitFor(() => expect(screen.getByText('carried snippet')).toBeInTheDocument());
    expect(screen.getByTestId('carry-over-divider')).toBeInTheDocument();
    expect(screen.getByText('continuing in child')).toBeInTheDocument();
  });

  it('surfaces conversation lookup error', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such issue' }, { status: 404 }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
      http.get('/api/conversations/:id/refs', () => HttpResponse.json([])),
    );
    wrap('/issues/missing');
    await waitFor(() => expect(screen.getByTestId('issue-not-found')).toHaveTextContent(/no such issue/));
  });
});

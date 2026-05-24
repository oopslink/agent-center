import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import { useAppStore } from '@/store/app';
import DMDetail from './DMDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/dms/:id" element={<DMDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('DMDetail page', () => {
  beforeEach(() => {
    useAppStore.setState({ currentUserId: 'user:hayang' });
  });
  afterEach(() => cleanup());

  it('renders peer names + messages + composer when found', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-DM',
          kind: 'dm',
          name: '',
          status: 'active',
          participants: [
            { identity_id: 'user:hayang', role: 'owner', joined_at: '2026-05-24T00:00:00Z', joined_by: 'user:hayang' },
            { identity_id: 'agent:bot-1', role: 'member', joined_at: '2026-05-24T00:00:01Z', joined_by: 'user:hayang' },
          ],
        }),
      ),
      http.get('/api/conversations/:id/messages', () =>
        HttpResponse.json([
          {
            id: 'M1',
            conversation_id: 'C-DM',
            sender_identity_id: 'agent:bot-1',
            content_kind: 'text',
            content: 'hi from bot',
            direction: 'inbound',
            posted_at: '2026-05-24T01:00:00Z',
          },
        ]),
      ),
    );
    wrap('/dms/C-DM');
    await waitFor(() => expect(screen.getByText('hi from bot')).toBeInTheDocument());
    // Heading shows peer identity (current user excluded).
    expect(screen.getByTestId('dm-heading')).toHaveTextContent('agent:bot-1');
    expect(screen.getByTestId('message-composer')).toBeInTheDocument();
    // No participants panel for DM.
    expect(screen.queryByTestId('participants-panel')).not.toBeInTheDocument();
  });

  it('uses the conversation name when set', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-DM2',
          kind: 'dm',
          name: 'design huddle',
          status: 'active',
          participants: [
            { identity_id: 'user:hayang', role: 'owner', joined_at: 'x', joined_by: 'user:hayang' },
            { identity_id: 'agent:bot-1', role: 'member', joined_at: 'y', joined_by: 'user:hayang' },
          ],
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
    );
    wrap('/dms/C-DM2');
    await waitFor(() => expect(screen.getByTestId('dm-heading')).toHaveTextContent('design huddle'));
  });

  it('surfaces conversation lookup error', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such dm' }, { status: 404 }),
      ),
    );
    wrap('/dms/missing');
    await waitFor(() => expect(screen.getByTestId('dm-not-found')).toHaveTextContent(/no such dm/));
  });

  it('surfaces messages error inside the page', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-DM',
          kind: 'dm',
          name: '',
          status: 'active',
          participants: [
            { identity_id: 'user:hayang', role: 'owner', joined_at: 'x', joined_by: 'user:hayang' },
          ],
        }),
      ),
      http.get('/api/conversations/:id/messages', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap('/dms/C-DM');
    await waitFor(() => expect(screen.getByTestId('dm-messages-error')).toHaveTextContent(/db down/));
  });

  // v2.1-B: cover the "solo DM" branch of DMDetail.tsx (lines 70-72)
  // where peers.length === 0 because the only participant is the
  // current user. F14 audit logged as "🟡 worth covering".
  it('renders "solo DM" heading when current user is the only participant', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-SOLO',
          kind: 'dm',
          name: '',
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
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
    );
    wrap('/dms/C-SOLO');
    await waitFor(() => expect(screen.getByText(/solo DM/i)).toBeInTheDocument());
    // Flip select mode so the truthy arm of the select-mode-toggle
    // ternary className (line 81) is exercised. F14 audit listed this
    // alongside the solo DM branch.
    const toggle = screen.getByTestId('select-mode-toggle');
    toggle.click();
    await waitFor(() => expect(toggle).toHaveAttribute('aria-pressed', 'true'));
  });
});

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
          peer_identity_id: 'agent:bot-1',
          peer_display_name: 'Bot One',
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
    // v2.7.1 #215: heading shows the resolved peer as @name (raw id on hover).
    expect(screen.getByTestId('dm-heading')).toHaveTextContent('@Bot One');
    expect(screen.getByTestId('dm-heading')).toHaveAttribute('title', 'agent:bot-1');
    expect(screen.getByTestId('message-composer')).toBeInTheDocument();
    // No participants panel for DM.
    expect(screen.queryByTestId('participants-panel')).not.toBeInTheDocument();
  });

  it('shows "(deleted)" heading when the peer no longer resolves (#215/E1)', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-DM2',
          kind: 'dm',
          name: '',
          status: 'active',
          peer_identity_id: 'agent:gone',
          // peer_display_name omitted → deleted peer.
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
    );
    wrap('/dms/C-DM2');
    await waitFor(() => expect(screen.getByTestId('dm-heading')).toHaveTextContent('(deleted)'));
  });

  it('resolves the peer from participants − self on a direct load (#238)', async () => {
    // The detail GET does NOT enrich peer_display_name (only the list does), so a
    // direct DM URL load must derive the peer from participants + resolve its name.
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([{ identity_id: 'bot-9', display_name: 'Bot Nine', kind: 'agent', status: 'joined' }]),
      ),
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-DIRECT',
          kind: 'dm',
          name: '',
          status: 'active',
          // no peer_display_name / peer_identity_id — only participants (detail GET).
          participants: [
            { identity_id: 'user:hayang', role: 'member', joined_at: 'x', joined_by: 'user:hayang' },
            { identity_id: 'agent:bot-9', role: 'member', joined_at: 'x', joined_by: 'user:hayang' },
          ],
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
    );
    wrap('/dms/C-DIRECT');
    // heading + breadcrumb leaf both show @Bot Nine (not "Direct message").
    await waitFor(() => expect(screen.getByTestId('dm-heading')).toHaveTextContent('@Bot Nine'));
    expect(screen.getByTestId('breadcrumb')).toHaveTextContent('@Bot Nine');
    // raw peer ref on hover (#192).
    expect(screen.getByTestId('dm-heading')).toHaveAttribute('title', 'agent:bot-9');
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

  // v2.7.1 #215: a malformed DM with no resolved peer falls back to "Direct message".
  it('falls back to "Direct message" heading when there is no peer', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-SOLO',
          kind: 'dm',
          name: '',
          status: 'active',
          // no peer_identity_id → malformed/solo DM.
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
    );
    wrap('/dms/C-SOLO');
    await waitFor(() => expect(screen.getByTestId('dm-heading')).toHaveTextContent('Direct message'));
  });
});

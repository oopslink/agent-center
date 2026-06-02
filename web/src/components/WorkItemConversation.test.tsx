import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { WorkItemConversation } from './WorkItemConversation';

function wrap(ownerRef: string, bannerLabel: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <WorkItemConversation ownerRef={ownerRef} bannerLabel={bannerLabel} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const conv = {
  id: 'conv-1',
  kind: 'task',
  name: 'rebuild docs',
  status: 'active',
  owner_ref: 'pm://tasks/TS-1',
};

describe('WorkItemConversation (#137)', () => {
  afterEach(() => cleanup());

  it('renders the owner banner naming the bound task even before the conversation loads', async () => {
    server.use(
      http.get('/api/conversations', () => HttpResponse.json([conv])),
      http.get('/api/conversations/conv-1/messages', () => HttpResponse.json([])),
    );
    wrap('pm://tasks/TS-1', 'rebuild docs');
    const banner = await screen.findByTestId('conversation-owner-banner');
    expect(banner).toHaveAttribute('data-owner-ref', 'pm://tasks/TS-1');
    expect(banner).toHaveTextContent('rebuild docs');
  });

  it('fetches the conversation BY owner_ref (org-scoped list query)', async () => {
    let seenOwnerRef: string | null = null;
    server.use(
      http.get('/api/conversations', ({ request }) => {
        seenOwnerRef = new URL(request.url).searchParams.get('owner_ref');
        return HttpResponse.json([conv]);
      }),
      http.get('/api/conversations/conv-1/messages', () => HttpResponse.json([])),
    );
    wrap('pm://tasks/TS-1', 'rebuild docs');
    await waitFor(() => expect(seenOwnerRef).toBe('pm://tasks/TS-1'));
  });

  it('shows an empty hint (not an error) when no conversation is bound to the owner_ref', async () => {
    server.use(http.get('/api/conversations', () => HttpResponse.json([])));
    wrap('pm://tasks/NONE', 'orphan task');
    await waitFor(() =>
      expect(screen.getByTestId('conversation-empty')).toHaveTextContent(/No linked conversation/),
    );
  });

  it('splits messages into work-item segments with an "Unassociated work item" bucket before the first WI segment', async () => {
    server.use(
      http.get('/api/conversations', () => HttpResponse.json([conv])),
      http.get('/api/conversations/conv-1/messages', () =>
        HttpResponse.json([
          { id: '1', conversation_id: 'conv-1', sender_identity_id: 'user:hayang', content_kind: 'text', content: 'kickoff', direction: 'inbound', posted_at: '2026-05-30T01:00:00Z' },
          { id: '2', conversation_id: 'conv-1', sender_identity_id: 'agent:builder', content_kind: 'text', content: 'on it', direction: 'internal', posted_at: '2026-05-30T02:00:00Z', context_refs: { work_item_ref: 'agent://WI-1', task_ref: 'pm://tasks/TS-1', agent_ref: 'agent:builder' } },
        ]),
      ),
    );
    wrap('pm://tasks/TS-1', 'rebuild docs');
    await waitFor(() => expect(screen.getAllByTestId('message-segment')).toHaveLength(2));
    const segments = screen.getAllByTestId('message-segment');
    expect(segments[0]).toHaveAttribute('data-work-item-ref', '');
    expect(segments[0]).toHaveTextContent('Unassociated work item');
    expect(segments[1]).toHaveAttribute('data-work-item-ref', 'agent://WI-1');
    expect(segments[1]).toHaveTextContent('Work item agent://WI-1');
  });
});

import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, renderHook, waitFor, act } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import type { Message } from './types';
import { useThreadReplies, useSendMessage } from './conversations';

function reply(id: string, parent: string): Message {
  return {
    id,
    conversation_id: 'C1',
    sender_identity_id: 'user:hayang',
    content_kind: 'text',
    content: `reply ${id}`,
    direction: 'inbound',
    posted_at: '2026-06-12T00:00:00Z',
    parent_message_id: parent,
  };
}

describe('thread api hooks', () => {
  afterEach(() => cleanup());

  it('useThreadReplies stays idle when rootMessageId is undefined', () => {
    const { result } = renderHook(() => useThreadReplies('C1', undefined), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('useThreadReplies fetches the root message replies', async () => {
    server.use(
      http.get('/api/conversations/:id/messages/:mid/replies', ({ params }) => {
        expect(params.mid).toBe('M-root');
        return HttpResponse.json([reply('R1', 'M-root'), reply('R2', 'M-root')], { status: 200 });
      }),
    );
    const { result } = renderHook(() => useThreadReplies('C1', 'M-root'), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(2);
    expect(result.current.data?.[0].parent_message_id).toBe('M-root');
  });

  it('useSendMessage posts parent_message_id when replying in a thread', async () => {
    let seenParent: string | undefined;
    server.use(
      http.post('/api/conversations/:id/messages', async ({ request }) => {
        const body = (await request.json()) as { parent_message_id?: string };
        seenParent = body.parent_message_id;
        return HttpResponse.json({ message_id: 'M-NEW', event_id: 'E-1' }, { status: 201 });
      }),
    );
    const { result } = renderHook(() => useSendMessage(), { wrapper: makeWrapper() });
    act(() => {
      result.current.mutate({ conversationId: 'C1', content: 'hi', parent_message_id: 'M-root' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(seenParent).toBe('M-root');
  });

  it('useSendMessage omits parent_message_id for a normal top-level send', async () => {
    let hadParentKey = true;
    server.use(
      http.post('/api/conversations/:id/messages', async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        hadParentKey = 'parent_message_id' in body && body.parent_message_id !== undefined;
        return HttpResponse.json({ message_id: 'M-NEW', event_id: 'E-1' }, { status: 201 });
      }),
    );
    const { result } = renderHook(() => useSendMessage(), { wrapper: makeWrapper() });
    act(() => {
      result.current.mutate({ conversationId: 'C1', content: 'hi' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(hadParentKey).toBe(false);
  });
});

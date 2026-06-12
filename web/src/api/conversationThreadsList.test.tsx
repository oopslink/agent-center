import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, renderHook, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import type { Message, ThreadSummary } from './types';
import { useConversationThreads } from './conversations';

function root(id: string, content: string): Message {
  return {
    id,
    conversation_id: 'C1',
    sender_identity_id: 'user:hayang',
    content_kind: 'text',
    content,
    direction: 'inbound',
    posted_at: '2026-06-12T00:00:00Z',
  };
}

describe('useConversationThreads', () => {
  afterEach(() => cleanup());

  it('stays idle when conversationId is undefined', () => {
    const { result } = renderHook(() => useConversationThreads(undefined), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('fetches the conversation thread summaries', async () => {
    const summaries: ThreadSummary[] = [
      { root: root('M1', 'first thread'), reply_count: 2, thread_last_activity_at: '2026-06-12T01:00:00Z' },
      { root: root('M2', 'second thread'), reply_count: 0 },
    ];
    server.use(
      http.get('/api/conversations/:id/threads', () => HttpResponse.json(summaries, { status: 200 })),
    );
    const { result } = renderHook(() => useConversationThreads('C1'), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(2);
    expect(result.current.data?.[0].root.content).toBe('first thread');
    expect(result.current.data?.[0].reply_count).toBe(2);
  });
});

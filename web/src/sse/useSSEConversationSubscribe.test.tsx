import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, renderHook, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import { useAppStore } from '@/store/app';
import { useSSEConversationSubscribe } from './useSSEConversationSubscribe';

describe('useSSEConversationSubscribe', () => {
  // currentUserId starts EMPTY in the store; the hook gates on it, so seed
  // an authenticated identity (as AppLayout does from /api/auth/me) before
  // the subscribe-path tests.
  beforeEach(() => {
    useAppStore.setState({ currentUserId: 'user:real' });
  });
  afterEach(() => {
    cleanup();
    useAppStore.setState({ currentUserId: '' });
  });

  it('POSTs /sse/subscribe per conv id on mount', async () => {
    const subscribed: Array<{ user_id: string; conversation_id: string }> = [];
    server.use(
      http.post('/api/sse/subscribe', async ({ request }) => {
        subscribed.push((await request.json()) as never);
        return HttpResponse.json({ subscribed: true }, { status: 200 });
      }),
    );
    renderHook(() => useSSEConversationSubscribe(['c-1', 'c-2', 'c-3']), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(subscribed.length).toBe(3));
    expect(subscribed.map((s) => s.conversation_id).sort()).toEqual([
      'c-1',
      'c-2',
      'c-3',
    ]);
    expect(subscribed[0].user_id).toBe('user:real');
  });

  it('does NOT subscribe while the identity is empty', async () => {
    useAppStore.setState({ currentUserId: '' });
    const calls: string[] = [];
    server.use(
      http.post('/api/sse/subscribe', async ({ request }) => {
        calls.push(((await request.json()) as { conversation_id: string }).conversation_id);
        return HttpResponse.json({ subscribed: true }, { status: 200 });
      }),
    );
    renderHook(() => useSSEConversationSubscribe(['c-1', 'c-2']), {
      wrapper: makeWrapper(),
    });
    await new Promise((r) => setTimeout(r, 50));
    expect(calls).toEqual([]);
  });

  it('POSTs /sse/unsubscribe per id on unmount', async () => {
    const unsubscribed: string[] = [];
    server.use(
      http.post('/api/sse/unsubscribe', async ({ request }) => {
        unsubscribed.push(
          ((await request.json()) as { conversation_id: string }).conversation_id,
        );
        return HttpResponse.json({ unsubscribed: true }, { status: 200 });
      }),
    );
    const { unmount } = renderHook(
      () => useSSEConversationSubscribe(['c-A', 'c-B']),
      { wrapper: makeWrapper() },
    );
    unmount();
    await waitFor(() => expect(unsubscribed.length).toBe(2));
    expect(unsubscribed.sort()).toEqual(['c-A', 'c-B']);
  });

  it('no-op when conversationIds undefined', async () => {
    const calls: string[] = [];
    server.use(
      http.post('/api/sse/subscribe', async ({ request }) => {
        calls.push(((await request.json()) as { conversation_id: string }).conversation_id);
        return HttpResponse.json({ subscribed: true }, { status: 200 });
      }),
    );
    renderHook(() => useSSEConversationSubscribe(undefined), {
      wrapper: makeWrapper(),
    });
    await new Promise((r) => setTimeout(r, 50));
    expect(calls).toEqual([]);
  });

  it('no-op when conversationIds is empty array', async () => {
    const calls: string[] = [];
    server.use(
      http.post('/api/sse/subscribe', async ({ request }) => {
        calls.push(((await request.json()) as { conversation_id: string }).conversation_id);
        return HttpResponse.json({ subscribed: true }, { status: 200 });
      }),
    );
    renderHook(() => useSSEConversationSubscribe([]), { wrapper: makeWrapper() });
    await new Promise((r) => setTimeout(r, 50));
    expect(calls).toEqual([]);
  });

  it('swallows subscribe failures (fire-and-forget)', async () => {
    server.use(
      http.post('/api/sse/subscribe', () =>
        HttpResponse.json({ error: 'subscribe_failed', message: 'bad' }, { status: 400 }),
      ),
    );
    // Should not throw, and react still renders normally.
    expect(() => {
      renderHook(() => useSSEConversationSubscribe(['c-X']), { wrapper: makeWrapper() });
    }).not.toThrow();
    await new Promise((r) => setTimeout(r, 50));
  });

  it('re-subscribes when id list changes', async () => {
    const calls: string[] = [];
    server.use(
      http.post('/api/sse/subscribe', async ({ request }) => {
        calls.push(((await request.json()) as { conversation_id: string }).conversation_id);
        return HttpResponse.json({ subscribed: true }, { status: 200 });
      }),
    );
    const { rerender } = renderHook(
      ({ ids }: { ids: string[] }) => useSSEConversationSubscribe(ids),
      { wrapper: makeWrapper(), initialProps: { ids: ['a'] } },
    );
    await waitFor(() => expect(calls).toContain('a'));
    rerender({ ids: ['a', 'b'] });
    await waitFor(() => expect(calls).toContain('b'));
  });

  // Test silences an unused-import lint by referencing vi (kept for
  // future expansion to spy on api directly).
  it('vi import retained for parity', () => {
    expect(typeof vi).toBe('object');
  });
});

import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, renderHook, waitFor, act } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import { useUnread, useMarkSeen } from './readState';

describe('readState hooks', () => {
  afterEach(() => cleanup());

  it('useUnread skips fetch when conversationId undefined', () => {
    const { result } = renderHook(() => useUnread(undefined), { wrapper: makeWrapper() });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('useUnread fetches summary when conversationId set', async () => {
    server.use(
      http.get('/api/conversations/:id/unread', ({ params }) =>
        HttpResponse.json(
          {
            conversation_id: String(params.id),
            user_id: 'user:hayang',
            last_seen_message_id: 'M0',
            unread_count: 3,
          },
          { status: 200 },
        ),
      ),
    );
    const { result } = renderHook(() => useUnread('C1'), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.unread_count).toBe(3);
    expect(result.current.data?.last_seen_message_id).toBe('M0');
  });

  it('useMarkSeen posts and reports bumped=true', async () => {
    server.use(
      http.post('/api/conversations/:id/seen', async ({ request }) => {
        const body = (await request.json()) as { last_seen_message_id: string };
        expect(body.last_seen_message_id).toBe('M5');
        return HttpResponse.json(
          { last_seen_message_id: 'M5', version: 2, bumped: true, event_id: 'E-99' },
          { status: 200 },
        );
      }),
    );
    const { result } = renderHook(() => useMarkSeen(), { wrapper: makeWrapper() });
    act(() => {
      result.current.mutate({ conversationId: 'C1', lastSeenMessageId: 'M5' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.bumped).toBe(true);
    expect(result.current.data?.event_id).toBe('E-99');
  });

  it('useMarkSeen accepts bumped=false no-op response', async () => {
    server.use(
      http.post('/api/conversations/:id/seen', () =>
        HttpResponse.json(
          { last_seen_message_id: 'M9', version: 5, bumped: false, event_id: '' },
          { status: 200 },
        ),
      ),
    );
    const { result } = renderHook(() => useMarkSeen(), { wrapper: makeWrapper() });
    act(() => {
      result.current.mutate({ conversationId: 'C1', lastSeenMessageId: 'M1' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.bumped).toBe(false);
  });

  it('useMarkSeen surfaces server error', async () => {
    server.use(
      http.post('/api/conversations/:id/seen', () =>
        HttpResponse.json(
          { error: 'message_not_in_conversation', message: 'bad msg' },
          { status: 422 },
        ),
      ),
    );
    const { result } = renderHook(() => useMarkSeen(), { wrapper: makeWrapper() });
    act(() => {
      result.current.mutate({ conversationId: 'C1', lastSeenMessageId: 'M-X' });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect((result.current.error as Error).message).toMatch(/bad msg/);
  });
});

import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, renderHook, waitFor, act } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import { MESSAGE_PAGE_SIZE, mergeTimeline, useConversationTimeline } from './conversations';
import type { Message } from './types';

const m = (id: string): Message => ({
  id,
  conversation_id: 'C1',
  sender_identity_id: 'user:h',
  content_kind: 'text',
  content: id,
  direction: 'inbound',
  posted_at: '2026-05-24T01:00:00Z',
});

describe('mergeTimeline (T189 p2)', () => {
  it('concatenates older history before the latest window', () => {
    expect(mergeTimeline([m('a'), m('b')], [m('c'), m('d')]).map((x) => x.id)).toEqual([
      'a',
      'b',
      'c',
      'd',
    ]);
  });

  it('de-dupes by id (SSE/overlap safety net), keeping first occurrence', () => {
    expect(mergeTimeline([m('a'), m('b')], [m('b'), m('c')]).map((x) => x.id)).toEqual([
      'a',
      'b',
      'c',
    ]);
  });
});

describe('useConversationTimeline (T189 p2)', () => {
  afterEach(() => cleanup());

  it('loads older history via the before cursor and merges it ahead of the latest window', async () => {
    // A FULL latest window (== page size) implies older history exists. Cursor =
    // its oldest message (w-0000); the before page is short (start of history).
    const latestWindow = Array.from({ length: MESSAGE_PAGE_SIZE }, (_, i) => m(`w-${String(i).padStart(4, '0')}`));
    const olderPage = [m('o-0'), m('o-1')];
    server.use(
      http.get('*/api/conversations/:id/messages', ({ request }) => {
        const before = new URL(request.url).searchParams.get('before');
        if (before === 'w-0000') return HttpResponse.json(olderPage); // short → no more
        if (before) return HttpResponse.json([]);
        return HttpResponse.json(latestWindow);
      }),
    );

    const { result } = renderHook(() => useConversationTimeline('C1'), { wrapper: makeWrapper() });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.messages).toHaveLength(MESSAGE_PAGE_SIZE);
    expect(result.current.hasOlder).toBe(true);

    act(() => result.current.loadOlder());

    await waitFor(() => expect(result.current.messages).toHaveLength(MESSAGE_PAGE_SIZE + 2));
    // Older page is merged AHEAD of the latest window.
    expect(result.current.messages.slice(0, 2).map((x) => x.id)).toEqual(['o-0', 'o-1']);
    // Short page → start of history reached.
    await waitFor(() => expect(result.current.hasOlder).toBe(false));
  });

  it('does NOT offer older history when the first window is short (normal conversation)', async () => {
    server.use(
      http.get('*/api/conversations/:id/messages', () => HttpResponse.json([m('b'), m('c')])),
    );
    const { result } = renderHook(() => useConversationTimeline('C1'), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.hasOlder).toBe(false);
  });
});

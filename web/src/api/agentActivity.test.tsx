import { afterEach, describe, expect, it } from 'vitest';
import { renderHook, waitFor, cleanup, act } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import { useAgentActivity } from './agents';

// v2.8 #274: the Activity feed paginates via the cursor endpoint
// GET /agents/:id/activity?limit=50&before=<event_id> → { activity, next_cursor }.
// The frontend always sends an explicit limit=50 (self-documenting, never the
// absent→default path) and follows next_cursor until null (末页).
describe('useAgentActivity (#274 cursor pagination)', () => {
  afterEach(() => cleanup());

  it('paginates via cursor: explicit limit=50, before=next_cursor, stops at null', async () => {
    const calls: string[] = [];
    server.use(
      http.get('/api/agents/:id/activity', ({ request }) => {
        const u = new URL(request.url);
        calls.push(u.search);
        const before = u.searchParams.get('before');
        if (!before) {
          return HttpResponse.json({ activity: [{ id: 'E3' }, { id: 'E2' }], next_cursor: 'E2' });
        }
        return HttpResponse.json({ activity: [{ id: 'E1' }], next_cursor: null });
      }),
    );
    const { result } = renderHook(() => useAgentActivity('A1'), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    // page 1: explicit limit=50, NO before.
    expect(calls[0]).toContain('limit=50');
    expect(calls[0]).not.toContain('before=');
    expect(result.current.hasNextPage).toBe(true);
    expect(
      (result.current.data?.pages.flatMap((p) => p.activity) ?? []).map((e) => e.id),
    ).toEqual(['E3', 'E2']);

    // "Load older" → page 2 with before=<next_cursor>; next_cursor=null → 末页.
    await act(async () => {
      await result.current.fetchNextPage();
    });
    await waitFor(() => expect(result.current.hasNextPage).toBe(false));
    expect(calls[1]).toContain('before=E2');
    expect(
      (result.current.data?.pages.flatMap((p) => p.activity) ?? []).map((e) => e.id),
    ).toEqual(['E3', 'E2', 'E1']);
  });
});

import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, renderHook, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import { useTask, useTasksList } from './tasks';

// v2.3-5b BC-native Task read hooks. Mirrors issues.test.ts.

describe('tasks hooks', () => {
  afterEach(() => cleanup());

  it('useTasksList fetches when projectId is set', async () => {
    const { result } = renderHook(() => useTasksList({ projectId: 'proj-a' }), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.[0].id).toBe('TS-1');
    expect(result.current.data?.[0].priority).toBe('medium');
  });

  // v2.5.15 (#70): projectId is now optional — the hook fetches the
  // cross-project list rather than short-circuiting to idle.
  it('useTasksList fetches the cross-project list when projectId is missing', async () => {
    server.use(
      http.get('/api/tasks', ({ request }) => {
        const url = new URL(request.url);
        expect(url.searchParams.get('project_id')).toBeNull();
        return HttpResponse.json([
          {
            id: 'TS-CROSS',
            project_id: 'proj-x',
            conversation_id: 'T-X',
            title: 'cross-project task',
            status: 'open',
            priority: 'medium',
            created_at: '2026-05-24T01:00:00Z',
          },
        ]);
      }),
    );
    const { result } = renderHook(() => useTasksList({}), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.[0].id).toBe('TS-CROSS');
  });

  it('useTasksList forwards status filter as a query param', async () => {
    server.use(
      http.get('/api/tasks', ({ request }) => {
        const url = new URL(request.url);
        expect(url.searchParams.get('project_id')).toBe('proj-a');
        expect(url.searchParams.get('status')).toBe('done');
        return HttpResponse.json([
          {
            id: 'TS-77',
            project_id: 'proj-a',
            conversation_id: 'T-77',
            title: 'finished',
            status: 'done',
            priority: 'high',
            created_at: '2026-05-24T01:00:00Z',
          },
        ]);
      }),
    );
    const { result } = renderHook(
      () => useTasksList({ projectId: 'proj-a', status: 'done' }),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.[0].status).toBe('done');
  });

  it('useTask skips fetch when id is undefined', () => {
    const { result } = renderHook(() => useTask(undefined), { wrapper: makeWrapper() });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('useTask fetches when id is set', async () => {
    const { result } = renderHook(() => useTask('TS-1'), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.id).toBe('TS-1');
  });
});

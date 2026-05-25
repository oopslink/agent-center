import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, renderHook, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import { useIssue, useIssues } from './issues';

// v2.3-5b BC-native Issue read hooks. MSW round-trip + assertions
// mirror the conversations hook test pattern.

describe('issues hooks', () => {
  afterEach(() => cleanup());

  it('useIssues fetches the canned list when projectId is set', async () => {
    const { result } = renderHook(() => useIssues({ projectId: 'proj-a' }), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.[0].id).toBe('IS-1');
    expect(result.current.data?.[0].project_id).toBe('proj-a');
  });

  it('useIssues stays idle when projectId is missing', () => {
    const { result } = renderHook(() => useIssues({}), { wrapper: makeWrapper() });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('useIssues forwards status filter as a query param', async () => {
    server.use(
      http.get('/api/issues', ({ request }) => {
        const url = new URL(request.url);
        expect(url.searchParams.get('project_id')).toBe('proj-a');
        expect(url.searchParams.get('status')).toBe('open');
        return HttpResponse.json([
          {
            id: 'IS-99',
            project_id: 'proj-a',
            conversation_id: 'I-99',
            title: 'open issue',
            status: 'open',
            opened_at: '2026-05-24T01:00:00Z',
            opener: 'user:hayang',
          },
        ]);
      }),
    );
    const { result } = renderHook(
      () => useIssues({ projectId: 'proj-a', status: 'open' }),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.[0].status).toBe('open');
  });

  it('useIssues surfaces backend 400 when project_id missing on the server', async () => {
    server.use(
      http.get('/api/issues', () =>
        HttpResponse.json(
          { error: 'missing_project_id', message: 'project_id required' },
          { status: 400 },
        ),
      ),
    );
    const { result } = renderHook(() => useIssues({ projectId: 'proj-a' }), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect((result.current.error as Error).message).toMatch(/project_id required/);
  });

  it('useIssue skips fetch when id is undefined', () => {
    const { result } = renderHook(() => useIssue(undefined), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('useIssue fetches when id is set', async () => {
    const { result } = renderHook(() => useIssue('IS-1'), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.id).toBe('IS-1');
  });
});

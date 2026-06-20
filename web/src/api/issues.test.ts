import { createElement, type ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, renderHook, waitFor, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import { qk } from './queryKeys';
import {
  useCreateIssue,
  useIssue,
  useIssues,
  useUpdateIssue,
} from './issues';

// v2.7 ProjectManager BC — project-scoped Issue hooks.

describe('issues hooks', () => {
  afterEach(() => cleanup());

  it('useIssues unwraps the wrapped list under a project', async () => {
    const { result } = renderHook(() => useIssues('proj-a'), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.[0].id).toBe('IS-1');
    expect(result.current.data?.[0].project_id).toBe('proj-a');
  });

  it('useIssues stays idle when projectId is undefined', () => {
    const { result } = renderHook(() => useIssues(undefined), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('useIssues surfaces backend error', async () => {
    server.use(
      http.get('/api/projects/:pid/issues', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    const { result } = renderHook(() => useIssues('proj-a'), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect((result.current.error as Error).message).toMatch(/db down/);
  });

  it('useIssue skips fetch when ids are undefined', () => {
    const { result } = renderHook(() => useIssue(undefined, undefined), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('useIssue fetches the nested issue when ids are set', async () => {
    const { result } = renderHook(() => useIssue('proj-a', 'IS-1'), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.id).toBe('IS-1');
  });

  it('useCreateIssue POSTs to the nested route', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/projects/proj-a/issues', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(
          {
            id: 'IS-NEW',
            project_id: 'proj-a',
            title: 'x',
            description: '',
            status: 'open',
            created_by: 'user:hayang',
            version: 1,
            created_at: 'x',
            updated_at: 'x',
          },
          { status: 201 },
        );
      }),
    );
    const { result } = renderHook(() => useCreateIssue('proj-a'), {
      wrapper: makeWrapper(),
    });
    act(() => {
      result.current.mutate({ title: 'x' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(received).toMatchObject({ title: 'x' });
  });

  it('useCreateIssue invalidates BOTH the project list AND the org-aggregation list (T233)', async () => {
    // Regression: the OrgWorkItems page reads from qk.orgIssues (a separate cache
    // key); creating an issue only invalidated qk.issuesByProject, so the org
    // table didn't show the new issue until staleTime lapsed. onSuccess must now
    // also invalidate the qk.orgIssuesAll() prefix.
    server.use(
      http.post('/api/projects/proj-a/issues', () =>
        HttpResponse.json(
          { id: 'IS-NEW', project_id: 'proj-a', title: 'x', description: '', status: 'open', created_by: 'user:hayang', version: 1, created_at: 'x', updated_at: 'x' },
          { status: 201 },
        ),
      ),
    );
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: qc }, children);
    const { result } = renderHook(() => useCreateIssue('proj-a'), { wrapper });
    act(() => {
      result.current.mutate({ title: 'x' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    const invalidatedKeys = invalidateSpy.mock.calls.map((c) => JSON.stringify(c[0]?.queryKey));
    expect(invalidatedKeys).toContain(JSON.stringify(qk.issuesByProject('proj-a')));
    expect(invalidatedKeys).toContain(JSON.stringify(qk.orgIssuesAll()));
  });

  it('useUpdateIssue PATCHes the nested route with the dirty-only body', async () => {
    // Mirror of #251: {title?, description?, status?, tags?} — NO assignee.
    let received: Record<string, unknown> | undefined;
    server.use(
      http.patch('/api/projects/proj-a/issues/IS-1', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({
          id: 'IS-1',
          project_id: 'proj-a',
          title: 'x',
          description: '',
          status: 'resolved',
          tags: ['a'],
          created_by: 'user:hayang',
          version: 2,
          created_at: 'x',
          updated_at: 'x',
        });
      }),
    );
    const { result } = renderHook(() => useUpdateIssue('proj-a', 'IS-1'), {
      wrapper: makeWrapper(),
    });
    act(() => {
      result.current.mutate({ status: 'resolved', tags: ['a'] });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(received).toEqual({ status: 'resolved', tags: ['a'] });
  });
});

import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, renderHook, waitFor, act } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import {
  usePlans,
  usePlan,
  useCreatePlan,
  usePatchPlan,
  useAddTaskToPlan,
  useRemoveTaskFromPlan,
} from './plans';

// v2.9 #286 Plan orchestration — project-scoped Plan hooks. Verified against the
// LOCKED contract MSW handlers (base /api/projects/:pid/plans).

describe('plans hooks', () => {
  afterEach(() => cleanup());

  it('usePlans unwraps the wrapped parallel list under a project', async () => {
    const { result } = renderHook(() => usePlans('proj-a'), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(2);
    expect(result.current.data?.[0].id).toBe('PL-1');
    // derived fields present per contract
    expect(result.current.data?.[0].status).toBe('running');
    expect(result.current.data?.[0].has_failed).toBe(true);
    expect(result.current.data?.[0].progress).toEqual({ done: 2, total: 5 });
  });

  it('usePlans stays idle when projectId is undefined', () => {
    const { result } = renderHook(() => usePlans(undefined), { wrapper: makeWrapper() });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('usePlan fetches a single Plan with derived nodes', async () => {
    const { result } = renderHook(() => usePlan('proj-a', 'PL-1'), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.id).toBe('PL-1');
    expect(result.current.data?.nodes?.[0].task_id).toBe('TS-1');
    expect(result.current.data?.nodes?.[0].node_status).toBe('ready');
  });

  it('usePlan skips fetch when ids are undefined', () => {
    const { result } = renderHook(() => usePlan(undefined, undefined), { wrapper: makeWrapper() });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('useCreatePlan POSTs name/description/target_date to the nested route', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/projects/proj-a/plans', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ ...(received), id: 'PL-NEW', project_id: 'proj-a' }, { status: 201 });
      }),
    );
    const { result } = renderHook(() => useCreatePlan('proj-a'), { wrapper: makeWrapper() });
    act(() => {
      result.current.mutate({ name: 'New plan', description: 'goal', target_date: '2026-07-01T00:00:00Z' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(received).toMatchObject({ name: 'New plan', description: 'goal', target_date: '2026-07-01T00:00:00Z' });
    expect(result.current.data?.id).toBe('PL-NEW');
  });

  it('usePatchPlan PATCHes only the changed fields', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.patch('/api/projects/proj-a/plans/PL-1', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 'PL-1', project_id: 'proj-a', name: 'renamed' });
      }),
    );
    const { result } = renderHook(() => usePatchPlan('proj-a', 'PL-1'), { wrapper: makeWrapper() });
    act(() => {
      result.current.mutate({ name: 'renamed' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(received).toEqual({ name: 'renamed' });
  });

  it('useAddTaskToPlan POSTs { task_id } to /:id/tasks', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/tasks', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({
          id: 'PL-1',
          project_id: 'proj-a',
          name: 'x',
          status: 'draft',
          has_failed: false,
          progress: { done: 0, total: 1 },
          nodes: [{ task_id: 'TS-9', title: 't', assignee_ref: '', task_status: 'open', node_status: 'ready', depends_on: [] }],
        });
      }),
    );
    const { result } = renderHook(() => useAddTaskToPlan('proj-a', 'PL-1'), { wrapper: makeWrapper() });
    act(() => {
      result.current.mutate({ task_id: 'TS-9' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(received).toEqual({ task_id: 'TS-9' });
    expect(result.current.data?.nodes?.[0].task_id).toBe('TS-9');
  });

  it('useRemoveTaskFromPlan DELETEs /:id/tasks/:taskId', async () => {
    let hit = false;
    server.use(
      http.delete('/api/projects/proj-a/plans/PL-1/tasks/TS-9', () => {
        hit = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { result } = renderHook(() => useRemoveTaskFromPlan('proj-a', 'PL-1'), { wrapper: makeWrapper() });
    act(() => {
      result.current.mutate('TS-9');
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(hit).toBe(true);
  });
});

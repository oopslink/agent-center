import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, renderHook, waitFor, act } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import {
  usePlans,
  useAssignmentPool,
  usePlan,
  useCreatePlan,
  usePatchPlan,
  useAddTaskToPlan,
  useRemoveTaskFromPlan,
  useDeletePlan,
  useArchivePlan,
  useCommitPlanEvolution,
  useResolvePlanBlock,
  friendlyDestructivePlanError,
} from './plans';

// v2.9 #286 Plan orchestration — project-scoped Plan hooks. Verified against the
// LOCKED contract MSW handlers (base /api/projects/:pid/plans).

describe('plans hooks', () => {
  afterEach(() => cleanup());

  it('usePlans unwraps the wrapped parallel list under a project', async () => {
    const { result } = renderHook(() => usePlans('proj-a'), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    // AssignmentPool is fetched from its own endpoint, so Plans contains only Plans.
    expect(result.current.data).toHaveLength(2);
    const pl1 = result.current.data?.find((p) => p.id === 'PL-1');
    // derived fields present per contract
    expect(pl1?.status).toBe('running');
    expect(pl1?.has_failed).toBe(true);
    expect(pl1?.progress).toEqual({ done: 2, total: 5 });
    // AssignmentPool is not encoded as a special Plan row.
    const builtins = result.current.data?.filter((p) => p.is_builtin === true) ?? [];
    expect(builtins).toHaveLength(0);
    expect(result.current.data?.find((p) => p.id === 'PL-1')?.is_builtin).toBe(false);
  });

  it('useAssignmentPool reads the independent low-priority pull queue', async () => {
    const { result } = renderHook(() => useAssignmentPool('proj-a'), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.id).toBe('POOL-proj-a');
    expect(result.current.data?.scheduling_class).toBe('background');
    expect(result.current.data?.tasks.map((task) => task.id)).toContain('TS-CLAIM');
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
          status: 'pending',
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

  // v2.9 Stage B — destructive lifecycle hooks.
  it('useDeletePlan DELETEs /:id', async () => {
    let method = '';
    let url = '';
    server.use(
      http.delete('/api/projects/proj-a/plans/PL-1', ({ request }) => {
        method = request.method;
        url = new URL(request.url).pathname;
        return HttpResponse.json({ deleted: true });
      }),
    );
    const { result } = renderHook(() => useDeletePlan('proj-a', 'PL-1'), { wrapper: makeWrapper() });
    act(() => {
      result.current.mutate();
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(method).toBe('DELETE');
    expect(url).toBe('/api/projects/proj-a/plans/PL-1');
    expect(result.current.data).toEqual({ deleted: true });
  });

  it('useArchivePlan POSTs /:id/archive', async () => {
    let method = '';
    let url = '';
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/archive', ({ request }) => {
        method = request.method;
        url = new URL(request.url).pathname;
        return HttpResponse.json({ id: 'PL-1', status: 'done', archived_at: '2026-07-01T00:00:00Z' });
      }),
    );
    const { result } = renderHook(() => useArchivePlan('proj-a', 'PL-1'), { wrapper: makeWrapper() });
    act(() => {
      result.current.mutate();
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(method).toBe('POST');
    expect(url).toBe('/api/projects/proj-a/plans/PL-1/archive');
  });

  it('useResolvePlanBlock POSTs acknowledge/resume/pause/discard bodies to the block resolve route', async () => {
    const calls: Array<{ method: string; path: string; body: Record<string, unknown> }> = [];
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/blocks/:taskId/resolve', async ({ request }) => {
        calls.push({
          method: request.method,
          path: new URL(request.url).pathname,
          body: (await request.json()) as Record<string, unknown>,
        });
        return HttpResponse.json({ ok: true });
      }),
    );
    const { result } = renderHook(() => useResolvePlanBlock('proj-a', 'PL-1'), { wrapper: makeWrapper() });
    for (const action of [
      { task_id: 'TS-BLOCK', action: 'acknowledge' as const, note: 'ack', idempotency_key: 'ack-1' },
      { task_id: 'TS-BLOCK', action: 'resume_original' as const, note: 'resume', idempotency_key: 'resume-1' },
      { task_id: 'TS-BLOCK', action: 'pause_or_discard_plan' as const, disposition: 'pause' as const, idempotency_key: 'pause-1' },
      { task_id: 'TS-BLOCK', action: 'pause_or_discard_plan' as const, disposition: 'discard' as const, idempotency_key: 'discard-1' },
    ]) {
      await act(async () => {
        await result.current.mutateAsync(action);
      });
    }
    expect(calls.map((c) => c.method)).toEqual(['POST', 'POST', 'POST', 'POST']);
    expect(calls.every((c) => c.path === '/api/projects/proj-a/plans/PL-1/blocks/TS-BLOCK/resolve')).toBe(true);
    expect(calls.map((c) => c.body)).toEqual([
      { action: 'acknowledge', note: 'ack', idempotency_key: 'ack-1' },
      { action: 'resume_original', note: 'resume', idempotency_key: 'resume-1' },
      { action: 'pause_or_discard_plan', disposition: 'pause', idempotency_key: 'pause-1' },
      { action: 'pause_or_discard_plan', disposition: 'discard', idempotency_key: 'discard-1' },
    ]);
  });

  it('useCommitPlanEvolution POSTs replace/bypass generation diffs to /evolution', async () => {
    const calls: Array<{ method: string; path: string; body: Record<string, unknown> }> = [];
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/evolution', async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        calls.push({ method: request.method, path: new URL(request.url).pathname, body });
        return HttpResponse.json({
          ok: true,
          duplicate: false,
          active_generation_id: 'gen-next',
          version: 8,
          dispatched: [],
          generation: { id: 'gen-next', revision: 2, diff: body.diff, snapshot: { tasks: [], edges: [], dispatch_records: [] }, snapshot_progress: { done: 0, total: 0 } },
        });
      }),
    );
    const { result } = renderHook(() => useCommitPlanEvolution('proj-a', 'PL-1'), { wrapper: makeWrapper() });
    await act(async () => {
      await result.current.mutateAsync({
        parent_generation_id: 'gen-1',
        base_version: 7,
        reason: 'replace',
        evidence: 'blocked',
        idempotency_key: 'replace-1',
        diff: {
          node_decisions: [{ task_id: 'TS-BLOCK', action: 'supersede', reason: 'replace blocked node' }],
          tasks: [{ ref: 'replacement-TS-BLOCK', title: 'replacement', assignee_ref: 'agent:a', delivery_contract: 'ship replacement', detached: false }],
          edges: [{ from: 'replacement-TS-BLOCK', to: 'TS-UP', kind: 'seq' }],
        },
      });
      await result.current.mutateAsync({
        parent_generation_id: 'gen-1',
        base_version: 7,
        reason: 'bypass',
        evidence: 'blocked',
        idempotency_key: 'bypass-1',
        diff: {
          node_decisions: [{ task_id: 'TS-BLOCK', action: 'supersede', reason: 'bypass blocked node' }],
          tasks: [],
          edges: [{ from: 'TS-DOWN', to: 'TS-UP', kind: 'seq' }],
        },
      });
    });
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual([
      'POST /api/projects/proj-a/plans/PL-1/evolution',
      'POST /api/projects/proj-a/plans/PL-1/evolution',
    ]);
    expect(calls[0].body.diff).toMatchObject({ tasks: [expect.objectContaining({ delivery_contract: 'ship replacement', detached: false })] });
    expect(calls[1].body.diff).toMatchObject({ tasks: [], edges: [{ from: 'TS-DOWN', to: 'TS-UP', kind: 'seq' }] });
  });

  it('friendlyDestructivePlanError maps 409s by message substring (status-agnostic, no raw error)', () => {
    expect(friendlyDestructivePlanError(new Error('[409 plan_conflict] projectmanager: plan is running'))).toMatch(
      /already started/i,
    );
    expect(friendlyDestructivePlanError(new Error('[409 plan_conflict] plan already archived'))).toMatch(
      /already archived/i,
    );
    expect(friendlyDestructivePlanError(new Error('boom'))).toMatch(/try again/i);
    // never leaks the raw message
    expect(friendlyDestructivePlanError(new Error('projectmanager: plan is running'))).not.toMatch(/projectmanager/);
  });

  // v2.9 #299: ErrPlanHasRunningTasks ("…plan has running tasks…") must map to
  // the distinct "has running tasks" text, NOT the plan-is-running text. Its
  // "running tasks" substring also contains bare "running", so the order in the
  // mapper (running task → before → running) is what makes this correct.
  it('friendlyDestructivePlanError distinguishes ErrPlanHasRunningTasks from ErrPlanRunning', () => {
    const hasRunningTasks = friendlyDestructivePlanError(
      new Error(
        '[409 plan_conflict] projectmanager: plan has running tasks — complete or stop them before archiving',
      ),
    );
    expect(hasRunningTasks).toMatch(/has running tasks/i);
    expect(hasRunningTasks).not.toMatch(/already started/i);

    // ErrPlanRunning (plan-state) still maps to the plan-is-running text.
    const planRunning = friendlyDestructivePlanError(
      new Error('[409 plan_conflict] projectmanager: plan is running'),
    );
    expect(planRunning).toMatch(/already started/i);
    expect(planRunning).not.toMatch(/has running tasks/i);

    // already-archived fallback still works.
    expect(
      friendlyDestructivePlanError(new Error('[409 plan_conflict] plan already archived')),
    ).toMatch(/already archived/i);
  });
});

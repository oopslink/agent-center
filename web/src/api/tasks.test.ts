import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, renderHook, waitFor, act } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import {
  useAssignTask,
  useBlockTask,
  useCreateTask,
  useStartTask,
  useTask,
  useTasksList,
} from './tasks';

// v2.7 ProjectManager BC — project-scoped Task hooks.

describe('tasks hooks', () => {
  afterEach(() => cleanup());

  it('useTasksList unwraps the wrapped list under a project', async () => {
    const { result } = renderHook(() => useTasksList('proj-a'), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.[0].id).toBe('TS-1');
    expect(result.current.data?.[0].status).toBe('open');
  });

  it('useTasksList stays idle when projectId is undefined', () => {
    const { result } = renderHook(() => useTasksList(undefined), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('useTask skips fetch when ids are undefined', () => {
    const { result } = renderHook(() => useTask(undefined, undefined), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('useTask fetches the nested task when ids are set', async () => {
    const { result } = renderHook(() => useTask('proj-a', 'TS-1'), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.id).toBe('TS-1');
  });

  it('useCreateTask POSTs to the nested route', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/projects/proj-a/tasks', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(
          {
            id: 'TS-NEW',
            project_id: 'proj-a',
            title: 'x',
            description: '',
            status: 'open',
            version: 1,
            created_at: 'x',
            updated_at: 'x',
          },
          { status: 201 },
        );
      }),
    );
    const { result } = renderHook(() => useCreateTask('proj-a'), {
      wrapper: makeWrapper(),
    });
    act(() => {
      result.current.mutate({ title: 'x' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(received).toMatchObject({ title: 'x' });
  });

  it('useAssignTask POSTs the assignee (metadata only — status unchanged)', async () => {
    // v2.8.1 #5th: assignee is metadata, not a state. Assigning sets the
    // assignee and leaves status as-is (here: open, the pre-assign state).
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/projects/proj-a/tasks/TS-1/assign', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({
          id: 'TS-1',
          project_id: 'proj-a',
          title: 'x',
          description: '',
          status: 'open',
          assignee: 'agent:builder',
          version: 2,
          created_at: 'x',
          updated_at: 'x',
        });
      }),
    );
    const { result } = renderHook(() => useAssignTask('proj-a', 'TS-1'), {
      wrapper: makeWrapper(),
    });
    act(() => {
      result.current.mutate({ assignee: 'agent:builder' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(received).toMatchObject({ assignee: 'agent:builder' });
    expect(result.current.data?.status).toBe('open');
    expect(result.current.data?.assignee).toBe('agent:builder');
  });

  it('useStartTask transitions to running', async () => {
    const { result } = renderHook(() => useStartTask('proj-a', 'TS-1'), {
      wrapper: makeWrapper(),
    });
    act(() => {
      result.current.mutate();
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.status).toBe('running');
  });

  it('useBlockTask requires a reason payload', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/projects/proj-a/tasks/TS-1/block', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({
          id: 'TS-1',
          project_id: 'proj-a',
          title: 'x',
          description: '',
          status: 'blocked',
          blocked_reason: 'waiting',
          version: 2,
          created_at: 'x',
          updated_at: 'x',
        });
      }),
    );
    const { result } = renderHook(() => useBlockTask('proj-a', 'TS-1'), {
      wrapper: makeWrapper(),
    });
    act(() => {
      result.current.mutate({ reason: 'waiting' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(received).toMatchObject({ reason: 'waiting' });
  });
});

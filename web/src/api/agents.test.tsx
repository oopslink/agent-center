import { afterEach, describe, expect, it } from 'vitest';
import { act, renderHook, waitFor, cleanup } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import { useArchiveAgent, useBatchAgentLifecycle } from './agents';

// v2.8 #270 / #272: soft-archive is the only user-facing delete path. The hook
// POSTs the #272 endpoint and returns the refreshed (archived) agent.
describe('useArchiveAgent (#270/#272)', () => {
  afterEach(() => cleanup());

  it('POSTs /agents/:id/archive and returns the archived agent', async () => {
    let hitPath = '';
    server.use(
      http.post('/api/agents/:id/archive', ({ params }) => {
        hitPath = `archive:${params.id}`;
        return HttpResponse.json({ id: 'A1', lifecycle: 'archived' });
      }),
    );
    const { result } = renderHook(() => useArchiveAgent('A1'), { wrapper: makeWrapper() });
    result.current.mutate();
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(hitPath).toBe('archive:A1');
    expect(result.current.data?.lifecycle).toBe('archived');
  });
});

// T232: batch lifecycle fans out per-agent endpoints sequentially, tracking
// progress and tolerating per-agent failures (one 409 doesn't abort the rest).
describe('useBatchAgentLifecycle (T232)', () => {
  afterEach(() => cleanup());

  it('POSTs the action for every id and reports done/total progress', async () => {
    const hits: string[] = [];
    server.use(
      http.post('/api/agents/:id/start', ({ params }) => {
        hits.push(String(params.id));
        return HttpResponse.json({ id: params.id, lifecycle: 'running' });
      }),
    );
    const { result } = renderHook(() => useBatchAgentLifecycle(), { wrapper: makeWrapper() });
    await act(async () => {
      await result.current.run(['A1', 'A2', 'A3'], 'start');
    });
    expect(hits).toEqual(['A1', 'A2', 'A3']);
    expect(result.current.progress.running).toBe(false);
    expect(result.current.progress.done).toBe(3);
    expect(result.current.progress.total).toBe(3);
    expect(result.current.progress.results.every((r) => r.ok)).toBe(true);
  });

  it("reset sends the {scope:'all', confirm:true} body", async () => {
    let body: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/agents/:id/reset', async ({ request, params }) => {
        body = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: params.id, lifecycle: 'stopped' });
      }),
    );
    const { result } = renderHook(() => useBatchAgentLifecycle(), { wrapper: makeWrapper() });
    await act(async () => {
      await result.current.run(['A1'], 'reset');
    });
    expect(body).toMatchObject({ scope: 'all', confirm: true });
  });

  it('records per-agent failures without aborting the batch', async () => {
    server.use(
      http.post('/api/agents/:id/stop', ({ params }) =>
        params.id === 'A2'
          ? HttpResponse.json({ code: 'invalid_state', message: 'nope' }, { status: 409 })
          : HttpResponse.json({ id: params.id, lifecycle: 'stopped' }),
      ),
    );
    const { result } = renderHook(() => useBatchAgentLifecycle(), { wrapper: makeWrapper() });
    await act(async () => {
      await result.current.run(['A1', 'A2', 'A3'], 'stop');
    });
    expect(result.current.progress.done).toBe(3);
    const failed = result.current.progress.results.filter((r) => !r.ok);
    expect(failed).toHaveLength(1);
    expect(failed[0].id).toBe('A2');
  });

  it('run([], action) is a no-op (no progress change)', async () => {
    const { result } = renderHook(() => useBatchAgentLifecycle(), { wrapper: makeWrapper() });
    await act(async () => {
      await result.current.run([], 'start');
    });
    expect(result.current.progress.total).toBe(0);
    expect(result.current.progress.running).toBe(false);
  });
});

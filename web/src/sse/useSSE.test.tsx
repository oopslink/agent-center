import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest';
import { renderHook, act, cleanup } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type React from 'react';
import { useAppStore } from '@/store/app';
import { qk } from '@/api/queryKeys';
import {
  computeBackoff,
  dispatchToQueryClient,
  startSSE,
  useSSE,
  type SSEEvent,
} from './useSSE';
import { FakeEventSource } from './fakeEventSource';

function makeWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

function resetStore() {
  useAppStore.setState({
    currentUserId: 'user:test',
    sseStatus: 'idle',
    sseLastEventId: null,
  });
}

describe('computeBackoff', () => {
  it('grows exponentially, capped at 30s', () => {
    const rng = () => 0.5; // jitter centered → no swing
    expect(computeBackoff(0, rng)).toBe(1000);
    expect(computeBackoff(1, rng)).toBe(2000);
    expect(computeBackoff(2, rng)).toBe(4000);
    expect(computeBackoff(5, rng)).toBe(30_000); // 1000*2^5 = 32000 → capped
    expect(computeBackoff(10, rng)).toBe(30_000);
  });

  it('applies ±20% jitter', () => {
    const min = computeBackoff(2, () => 0); // (-0.2)
    const max = computeBackoff(2, () => 1); // (+0.2)
    const mid = computeBackoff(2, () => 0.5); // 0
    expect(min).toBe(Math.floor(4000 * 0.8));
    expect(max).toBe(Math.floor(4000 * 1.2));
    expect(mid).toBe(4000);
  });
});

describe('dispatchToQueryClient', () => {
  let qc: QueryClient;
  let invalidate: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    qc = new QueryClient();
    invalidate = vi.fn();
    qc.invalidateQueries = invalidate as unknown as typeof qc.invalidateQueries;
    resetStore();
  });

  const ev = (type: string, conversationId?: string): SSEEvent => ({
    event_type: type,
    conversation_id: conversationId,
  });

  it('conversation.opened invalidates list + detail', () => {
    dispatchToQueryClient(qc, ev('conversation.opened', 'C1'));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.conversations() });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.conversation('C1') });
  });

  it('conversation.message_added invalidates messages + unread + thread list', () => {
    dispatchToQueryClient(qc, ev('conversation.message_added', 'C1'));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.messages('C1') });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.unread('C1') });
    // v2.9.1 Threads F2: keep the Participants thread list live (new thread /
    // reply_count++ / re-sort) the same way the message badge is live.
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.conversationThreads('C1') });
  });

  it('conversation.archived / conversation.closed invalidate list + detail', () => {
    for (const t of ['conversation.archived', 'conversation.closed']) {
      dispatchToQueryClient(qc, ev(t, 'C1'));
    }
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.conversations() });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.conversation('C1') });
  });

  it('conversation.read_state.changed invalidates unread + thread badges', () => {
    dispatchToQueryClient(qc, ev('conversation.read_state.changed', 'C1'));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.unread('C1') });
    // v2.9.1 P3: read cursor moved → has-new-activity badges may clear, so the
    // thread list + the per-message thread badges must re-derive.
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.conversationThreads('C1') });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.messages('C1') });
  });

  it('workforce.agent_instance.* invalidates agents + fleet (note BC prefix)', () => {
    for (const t of [
      'workforce.agent_instance.created',
      'workforce.agent_instance.archived',
      'workforce.agent_instance.activated',
      'workforce.agent_instance.idle',
      'workforce.agent_instance.sleeping',
      'workforce.agent_instance.awakened',
      'workforce.agent_instance.config_updated',
    ]) {
      dispatchToQueryClient(qc, ev(t));
    }
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.agents() });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.fleet() });
  });

  it('workforce.worker.* invalidates fleet (note BC prefix)', () => {
    for (const t of [
      'workforce.worker.enrolled',
      'workforce.worker.config.updated',
      'workforce.worker.capability.updated',
      'workforce.worker.online',
      'workforce.worker.offline',
      'workforce.worker.renamed',
    ]) {
      dispatchToQueryClient(qc, ev(t));
    }
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.fleet() });
  });

  // v2.4-D-F3: workforce.worker.enrolled also dispatches a window
  // CustomEvent so the AddWorkerModal can transition State 2 → State 3.
  it('workforce.worker.enrolled dispatches agent-center:worker-enrolled CustomEvent', () => {
    const got: CustomEvent[] = [];
    const handler = (e: Event) => got.push(e as CustomEvent);
    window.addEventListener('agent-center:worker-enrolled', handler);
    try {
      dispatchToQueryClient(qc, {
        event_type: 'workforce.worker.enrolled',
        data: { worker_id: 'w-42', capabilities: ['claudecode'] },
      });
    } finally {
      window.removeEventListener('agent-center:worker-enrolled', handler);
    }
    expect(got).toHaveLength(1);
    expect((got[0].detail as { worker_id: string }).worker_id).toBe('w-42');
  });

  // Sibling worker.* events must NOT dispatch the custom event — only
  // enrolled is the bridge to AddWorkerModal.
  it('workforce.worker.config.updated does NOT dispatch agent-center:worker-enrolled', () => {
    const got: Event[] = [];
    const handler = (e: Event) => got.push(e);
    window.addEventListener('agent-center:worker-enrolled', handler);
    try {
      dispatchToQueryClient(qc, ev('workforce.worker.config.updated'));
    } finally {
      window.removeEventListener('agent-center:worker-enrolled', handler);
    }
    expect(got).toHaveLength(0);
  });

  it('secretmgmt.user_secret.* invalidates secrets (note BC prefix)', () => {
    for (const t of [
      'secretmgmt.user_secret.created',
      'secretmgmt.user_secret.rotated',
      'secretmgmt.user_secret.revoked',
    ]) {
      dispatchToQueryClient(qc, ev(t));
    }
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.secrets() });
  });

  it('task_execution.* + task.* variants invalidate fleet', () => {
    for (const t of [
      'task.created',
      'task.abandoned',
      'task.suspended',
      'task.done',
      'task_execution.submitted',
      'task_execution.dispatched',
      'task_execution.acked',
      'task_execution.nacked',
      'task_execution.failed',
      'task_execution.killed',
      'task_execution.kill_requested',
      'task_execution.dispatch_rejected',
    ]) {
      dispatchToQueryClient(qc, ev(t));
    }
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.fleet() });
  });

  // v2.3-5b: task lifecycle also invalidates the BC-native Task list
  // cache (qk.tasksList) so the Tasks page refreshes when an event
  // arrives without the user reloading the route.
  it('task.* lifecycle invalidates the BC-native tasks list cache', () => {
    for (const t of ['task.created', 'task.abandoned', 'task.suspended', 'task.done']) {
      dispatchToQueryClient(qc, ev(t));
    }
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.tasksList() });
  });

  // v2.3-5b: issue lifecycle invalidates the BC-native Issue list
  // cache (qk.issues). Mirrors the task path above.
  it('issue.* lifecycle invalidates the BC-native issues list cache', () => {
    for (const t of [
      'issue.opened',
      'issue.withdrawn',
      'issue.concluded',
      'issue.tasks_spawned',
      'issue.discussion_started',
    ]) {
      dispatchToQueryClient(qc, ev(t));
    }
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.issues() });
  });

  it('task_execution.input_required invalidates fleet', () => {
    dispatchToQueryClient(qc, ev('task_execution.input_required'));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.fleet() });
  });

  it('participant_joined / participant_left invalidate the affected conversation', () => {
    for (const t of ['conversation.participant_joined', 'conversation.participant_left']) {
      dispatchToQueryClient(qc, ev(t, 'C1'));
    }
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.conversation('C1') });
  });

  it('conversation.message_references_added invalidates refs + messages of child', () => {
    dispatchToQueryClient(qc, ev('conversation.message_references_added', 'CHILD'));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.refs('CHILD') });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.messages('CHILD') });
  });

  it('unknown event is no-op', () => {
    dispatchToQueryClient(qc, ev('something.brand_new'));
    expect(invalidate).not.toHaveBeenCalled();
  });
});

describe('startSSE', () => {
  beforeEach(() => {
    FakeEventSource.reset();
    resetStore();
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  function start() {
    const qc = new QueryClient();
    const ctrl = startSSE({
      userId: 'user:test',
      qc,
      store: useAppStore,
      factory: (url) => new FakeEventSource(url) as unknown as EventSource,
    });
    return { qc, ctrl };
  }

  it('opens an EventSource with user_id in query', () => {
    start();
    expect(FakeEventSource.last()?.url).toContain('user_id=user%3Atest');
    expect(useAppStore.getState().sseStatus).toBe('connecting');
  });

  it('transitions to open on EventSource onopen', () => {
    start();
    FakeEventSource.last()!.openConnection();
    expect(useAppStore.getState().sseStatus).toBe('open');
  });

  it('stores Last-Event-ID + sends it on reconnect', () => {
    start();
    const es1 = FakeEventSource.last()!;
    es1.openConnection();
    es1.emit('conversation.opened', { conversation_id: 'C1' }, '42');
    expect(useAppStore.getState().sseLastEventId).toBe('42');

    // Server drops the connection.
    es1.fail();
    expect(useAppStore.getState().sseStatus).toBe('reconnecting');

    // Advance timers past one backoff tick (max 1s * 1.2 = 1200ms).
    act(() => {
      vi.advanceTimersByTime(1300);
    });
    const es2 = FakeEventSource.last()!;
    expect(es2).not.toBe(es1);
    expect(es2.url).toContain('last_event_id=42');
  });

  it('exponential backoff increases on repeated failures', () => {
    start();
    const seen: number[] = [];
    for (let i = 0; i < 4; i++) {
      const es = FakeEventSource.last()!;
      es.fail();
      const before = FakeEventSource.instances.length;
      // advance enough for the largest possible delay at attempt i (max 30s)
      act(() => {
        vi.advanceTimersByTime(31_000);
      });
      const after = FakeEventSource.instances.length;
      seen.push(after - before);
    }
    // Each tick should produce exactly one new EventSource.
    expect(seen).toEqual([1, 1, 1, 1]);
  });

  it('heartbeat timeout forces reconnect when no event arrives', () => {
    start();
    const es1 = FakeEventSource.last()!;
    es1.openConnection();
    // No events arrive; advance just past HEARTBEAT_TIMEOUT (45s) but
    // BEFORE the backoff reconnect (~1s after) fires — the status should
    // be 'reconnecting' at this point. (Advancing further would tip into
    // 'connecting' as the reconnect timer takes over.)
    act(() => {
      vi.advanceTimersByTime(45_100);
    });
    expect(es1.closed).toBe(true);
    expect(useAppStore.getState().sseStatus).toBe('reconnecting');
  });

  // v2.5.13 (#71): a heartbeat message arriving from the server (real
  // data frame, not the prior `: ping` comment) must reset the
  // client-side watchdog so the connection stays in `open` instead of
  // cycling into `reconnecting` every backend heartbeat interval.
  it('heartbeat data message resets the watchdog and keeps status open', () => {
    start();
    const es1 = FakeEventSource.last()!;
    es1.openConnection();
    // 30s into the connection — well past the backend heartbeat
    // cadence (30s) — a heartbeat data frame lands.
    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    es1.onmessage?.call(
      es1 as unknown as EventSource,
      new MessageEvent('message', { data: '{"event_type":"sse.heartbeat"}' }),
    );
    // Another 30s passes; the watchdog should have been pushed out so
    // the connection is still alive (cumulative 60s > old 30s timeout,
    // but the heartbeat at 30s reset the 45s timer).
    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(es1.closed).toBe(false);
    expect(useAppStore.getState().sseStatus).toBe('open');
  });

  it('close() stops reconnect + marks closed', () => {
    const { ctrl } = start();
    const es1 = FakeEventSource.last()!;
    es1.fail();
    ctrl.close();
    expect(useAppStore.getState().sseStatus).toBe('closed');
    // Even if we advance the timer, no new EventSource is created.
    const before = FakeEventSource.instances.length;
    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    expect(FakeEventSource.instances.length).toBe(before);
  });

  it('ignores malformed JSON event payloads', () => {
    const { qc } = start();
    const invalidate = vi.fn();
    qc.invalidateQueries = invalidate as unknown as typeof qc.invalidateQueries;
    const es = FakeEventSource.last()!;
    es.openConnection();
    // Bypass emit() to send raw garbage.
    es.onmessage?.call(es as unknown as EventSource, new MessageEvent('message', { data: 'not json' }));
    expect(invalidate).not.toHaveBeenCalled();
  });
});

describe('useSSE', () => {
  beforeEach(() => {
    FakeEventSource.reset();
    resetStore();
  });
  afterEach(() => cleanup());

  it('opens + closes the EventSource over the lifecycle', () => {
    const qc = new QueryClient();
    const { unmount } = renderHook(
      () =>
        useSSE({
          factory: (url) => new FakeEventSource(url) as unknown as EventSource,
        }),
      { wrapper: makeWrapper(qc) },
    );
    expect(FakeEventSource.instances.length).toBe(1);
    expect(FakeEventSource.last()?.closed).toBe(false);
    unmount();
    expect(FakeEventSource.last()?.closed).toBe(true);
    expect(useAppStore.getState().sseStatus).toBe('closed');
  });

  // Regression: currentUserId starts EMPTY until AppLayout seeds it from
  // /api/auth/me. The hook must NOT connect under an unresolved identity —
  // otherwise it leaked a /api/sse?user_id=user:hayang request (the removed
  // hardcoded placeholder).
  it('does NOT open a connection while the identity is empty', () => {
    useAppStore.setState({ currentUserId: '' });
    const qc = new QueryClient();
    renderHook(
      () =>
        useSSE({
          factory: (url) => new FakeEventSource(url) as unknown as EventSource,
        }),
      { wrapper: makeWrapper(qc) },
    );
    expect(FakeEventSource.instances.length).toBe(0);
  });

  it('connects once the identity is seeded (e.g. from /api/auth/me)', () => {
    useAppStore.setState({ currentUserId: '' });
    const qc = new QueryClient();
    renderHook(
      () =>
        useSSE({
          factory: (url) => new FakeEventSource(url) as unknown as EventSource,
        }),
      { wrapper: makeWrapper(qc) },
    );
    // Empty identity → no connection yet.
    expect(FakeEventSource.instances.length).toBe(0);

    // AppLayout resolves the authenticated identity → the hook connects
    // under the real ref (not a placeholder).
    act(() => {
      useAppStore.setState({ currentUserId: 'user:real' });
    });
    expect(FakeEventSource.instances.length).toBeGreaterThan(0);
    expect(FakeEventSource.last()?.url).toContain('user_id=user%3Areal');
    expect(FakeEventSource.last()?.url).not.toContain('hayang');
  });
});

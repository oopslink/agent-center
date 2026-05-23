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
    navBadges: { inputRequests: 0 },
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

  it('conversation.message_added invalidates messages', () => {
    dispatchToQueryClient(qc, ev('conversation.message_added', 'C1'));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.messages('C1') });
  });

  it('input_request.created invalidates IRs + bumps badge', () => {
    dispatchToQueryClient(qc, ev('input_request.created'));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.inputRequests() });
    expect(useAppStore.getState().navBadges.inputRequests).toBe(1);
  });

  it('agent_instance.created invalidates agents list', () => {
    dispatchToQueryClient(qc, ev('agent_instance.created'));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.agents() });
  });

  it('user_secret.created invalidates secrets', () => {
    dispatchToQueryClient(qc, ev('user_secret.created'));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.secrets() });
  });

  it('task_execution.state_changed invalidates fleet', () => {
    dispatchToQueryClient(qc, ev('task_execution.state_changed'));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.fleet() });
  });

  it('participants_changed invalidates the affected conversation', () => {
    dispatchToQueryClient(qc, ev('conversation.participants_changed', 'C1'));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: qk.conversation('C1') });
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
    // No events arrive; advance past HEARTBEAT_TIMEOUT (30s) + a tick.
    act(() => {
      vi.advanceTimersByTime(31_000);
    });
    expect(es1.closed).toBe(true);
    expect(useAppStore.getState().sseStatus).toBe('reconnecting');
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
});

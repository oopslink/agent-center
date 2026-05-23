import { useEffect, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { qk } from '@/api/queryKeys';
import { useAppStore } from '@/store/app';

// Single app-wide EventSource. Subscribes the current user, manages
// exponential backoff w/ jitter reconnect, heartbeat-timeout detection,
// and dispatches typed events into react-query cache invalidations.
//
// Per F5 oversight:
//   - one connection per app, not per page
//   - backoff: base 1s, max 30s, ±20% jitter
//   - heartbeat: 30s no-event → force reconnect (Safari half-open guard)
//   - Last-Event-ID resumes from the backend ringbuffer (sent via
//     ?last_event_id= query param since EventSource cannot set headers)
//   - status pushed to Zustand sseStatus for the topbar indicator
//   - degraded mode: react-query stale data renders even while SSE down

const BASE_BACKOFF_MS = 1_000;
const MAX_BACKOFF_MS = 30_000;
const HEARTBEAT_TIMEOUT_MS = 30_000;
const JITTER = 0.2;

// computeBackoff returns the next reconnect delay in milliseconds with
// exponential growth (capped) plus ±20% jitter.
export function computeBackoff(attempt: number, rng: () => number = Math.random): number {
  const exp = Math.min(BASE_BACKOFF_MS * 2 ** attempt, MAX_BACKOFF_MS);
  const jitter = 1 + (rng() * 2 - 1) * JITTER; // [-0.2, +0.2]
  return Math.max(0, Math.floor(exp * jitter));
}

// SSEEvent matches the backend Event struct (bus.go).
export interface SSEEvent {
  id?: string | number;
  event_type: string;
  conversation_id?: string;
  data?: unknown;
  occurred_at?: string;
}

// EventSourceFactory is injectable for tests (fake EventSource).
export type EventSourceFactory = (url: string) => EventSource;

interface Controller {
  close(): void;
}

interface StartArgs {
  userId: string;
  qc: ReturnType<typeof useQueryClient>;
  store: typeof useAppStore;
  factory: EventSourceFactory;
  // Test seams:
  setTimeoutFn?: typeof setTimeout;
  clearTimeoutFn?: typeof clearTimeout;
}

// startSSE owns the connection lifecycle. Returns a stop handle that
// closes the current EventSource and cancels any pending reconnect /
// heartbeat timer. Exported for tests; useSSE wraps it.
export function startSSE(args: StartArgs): Controller {
  const setT = args.setTimeoutFn ?? setTimeout;
  const clearT = args.clearTimeoutFn ?? clearTimeout;
  let attempt = 0;
  let stopped = false;
  let es: EventSource | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let heartbeatTimer: ReturnType<typeof setTimeout> | null = null;

  const setStatus = (s: 'connecting' | 'open' | 'reconnecting' | 'closed') => {
    args.store.getState().setSSEStatus(s);
  };

  const resetHeartbeat = () => {
    if (heartbeatTimer) clearT(heartbeatTimer);
    heartbeatTimer = setT(() => {
      // Safari/iOS half-open guard: no event for HEARTBEAT_TIMEOUT_MS,
      // assume the socket is dead and force a reconnect.
      if (es) {
        es.close();
        es = null;
      }
      scheduleReconnect();
    }, HEARTBEAT_TIMEOUT_MS);
  };

  const scheduleReconnect = () => {
    if (stopped) return;
    setStatus('reconnecting');
    const delay = computeBackoff(attempt);
    attempt = Math.min(attempt + 1, 6); // cap exponent at 2^6 ~ 64s pre-cap
    reconnectTimer = setT(() => {
      reconnectTimer = null;
      connect();
    }, delay);
  };

  const handleEvent = (raw: MessageEvent) => {
    resetHeartbeat();
    if (raw.lastEventId) {
      args.store.getState().setSSELastEventId(raw.lastEventId);
    }
    let ev: SSEEvent;
    try {
      ev = JSON.parse(raw.data) as SSEEvent;
    } catch {
      return;
    }
    dispatchToQueryClient(args.qc, ev);
  };

  const connect = () => {
    if (stopped) return;
    setStatus('connecting');
    const params = new URLSearchParams({ user_id: args.userId });
    const lastEventId = args.store.getState().sseLastEventId;
    if (lastEventId) {
      params.set('last_event_id', lastEventId);
    }
    const url = `/api/sse?${params.toString()}`;
    es = args.factory(url);
    es.onopen = () => {
      attempt = 0;
      setStatus('open');
      resetHeartbeat();
    };
    es.onmessage = handleEvent;
    es.onerror = () => {
      if (es) {
        es.close();
        es = null;
      }
      if (heartbeatTimer) {
        clearT(heartbeatTimer);
        heartbeatTimer = null;
      }
      scheduleReconnect();
    };
  };

  connect();

  return {
    close() {
      stopped = true;
      if (es) {
        es.close();
        es = null;
      }
      if (reconnectTimer) clearT(reconnectTimer);
      if (heartbeatTimer) clearT(heartbeatTimer);
      setStatus('closed');
    },
  };
}

// dispatchToQueryClient maps SSE event types to react-query invalidations.
// Adding a new type? wire it here so any page subscribing to the affected
// query refetches.
export function dispatchToQueryClient(qc: ReturnType<typeof useQueryClient>, ev: SSEEvent): void {
  switch (ev.event_type) {
    case 'conversation.opened':
    case 'conversation.archived':
      void qc.invalidateQueries({ queryKey: qk.conversations() });
      if (ev.conversation_id) {
        void qc.invalidateQueries({ queryKey: qk.conversation(ev.conversation_id) });
      }
      return;
    case 'conversation.message_added':
      if (ev.conversation_id) {
        void qc.invalidateQueries({ queryKey: qk.messages(ev.conversation_id) });
      }
      return;
    case 'conversation.participants_changed':
      if (ev.conversation_id) {
        void qc.invalidateQueries({ queryKey: qk.conversation(ev.conversation_id) });
      }
      return;
    case 'input_request.created':
    case 'input_request.responded':
    case 'input_request.cancelled':
      // Invalidate the query so the sidebar's useInputRequests() (and the
      // inbox page) refetch + the badge recomputes from pending count.
      // We deliberately do NOT bump a Zustand counter here — the badge
      // reflects the actual server state, not the number of SSE pushes.
      void qc.invalidateQueries({ queryKey: qk.inputRequests() });
      return;
    case 'agent_instance.created':
    case 'agent_instance.archived':
      void qc.invalidateQueries({ queryKey: qk.agents() });
      return;
    case 'user_secret.created':
    case 'user_secret.revoked':
      void qc.invalidateQueries({ queryKey: qk.secrets() });
      return;
    case 'task_execution.state_changed':
      void qc.invalidateQueries({ queryKey: qk.fleet() });
      return;
    default:
      // Unknown event type — no-op (forwards-compatible with new server
      // event types).
      return;
  }
}

// useSSE is the React entry point: starts the connection on mount + cleans
// up on unmount. Returns nothing — the connection's data flows through
// the react-query cache via dispatchToQueryClient.
export function useSSE(opts?: { factory?: EventSourceFactory }): void {
  const qc = useQueryClient();
  const userId = useAppStore((s) => s.currentUserId);
  const ctrlRef = useRef<Controller | null>(null);

  useEffect(() => {
    const factory = opts?.factory ?? ((url: string) => new EventSource(url));
    ctrlRef.current = startSSE({ userId, qc, store: useAppStore, factory });
    return () => {
      ctrlRef.current?.close();
      ctrlRef.current = null;
    };
    // userId rarely changes; reconnect when it does so the new identity
    // subscribes correctly.
  }, [userId, qc, opts?.factory]);
}

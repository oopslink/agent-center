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
//
// Names are the LITERAL backend EventType strings (verified by F13 SSE
// wire-in audit — see docs/plans/sse-wiring-audit.md). The SSE Bus
// passes EventType through verbatim (sse/fanout.go:97 `EventType:
// string(e.Type())`), so BC-prefixed names like
// `workforce.agent_instance.created` are what arrives at the client —
// NOT the unprefixed `agent_instance.created` we used to assume.
//
// Adding a new event type? Find the literal `EventType: "..."` string
// via `rg '^\s*EventType:\s*"' internal/` and wire here.
export function dispatchToQueryClient(qc: ReturnType<typeof useQueryClient>, ev: SSEEvent): void {
  const invalidate = (key: readonly unknown[]) =>
    void qc.invalidateQueries({ queryKey: key as readonly unknown[] });

  switch (ev.event_type) {
    // Conversation lifecycle.
    case 'conversation.opened':
    case 'conversation.archived':
    case 'conversation.closed':
      // archived / closed change the row's status field on /channels +
      // /dms list and the detail page header; both queries refresh so
      // the user sees the new state without a manual reload.
      invalidate(qk.conversations());
      if (ev.conversation_id) {
        invalidate(qk.conversation(ev.conversation_id));
      }
      return;
    case 'conversation.message_added':
      if (ev.conversation_id) {
        invalidate(qk.messages(ev.conversation_id));
        // New message ticks the unread badge on every listener; the
        // focused tab will then auto-mark-seen which clears it again.
        invalidate(qk.unread(ev.conversation_id));
      }
      return;
    case 'conversation.read_state.changed':
      if (ev.conversation_id) {
        invalidate(qk.unread(ev.conversation_id));
      }
      return;
    case 'conversation.participant_joined':
    case 'conversation.participant_left':
      if (ev.conversation_id) {
        invalidate(qk.conversation(ev.conversation_id));
      }
      return;
    case 'conversation.message_references_added':
      if (ev.conversation_id) {
        invalidate(qk.refs(ev.conversation_id));
        invalidate(qk.messages(ev.conversation_id));
      }
      return;

    // Input request lifecycle.
    case 'input_request.requested':
    case 'input_request.responded':
    case 'input_request.canceled':
    case 'input_request.timed_out':
    case 'input_request.escalated':
      invalidate(qk.inputRequests());
      return;

    // Agent instance lifecycle. Backend emits BC-prefixed names.
    case 'workforce.agent_instance.created':
    case 'workforce.agent_instance.archived':
    case 'workforce.agent_instance.activated':
    case 'workforce.agent_instance.idle':
    case 'workforce.agent_instance.sleeping':
    case 'workforce.agent_instance.awakened':
    case 'workforce.agent_instance.config_updated':
      invalidate(qk.agents());
      invalidate(qk.fleet());
      return;

    // Worker lifecycle. Backend emits BC-prefixed names.
    case 'workforce.worker.enrolled':
    case 'workforce.worker.config.updated':
    case 'workforce.worker.capability.updated':
      invalidate(qk.fleet());
      return;

    // Secret lifecycle. Backend emits BC-prefixed names.
    case 'secretmgmt.user_secret.created':
    case 'secretmgmt.user_secret.rotated':
    case 'secretmgmt.user_secret.revoked':
      invalidate(qk.secrets());
      return;

    // Task lifecycle — fleet view + BC-native Task list/show
    // refresh on any move (v2.3-5b: qk.tasksList / qk.task were
    // added when the SPA stopped going through Conversation BC).
    case 'task.created':
    case 'task.abandoned':
    case 'task.suspended':
    case 'task.done':
      invalidate(qk.fleet());
      invalidate(qk.tasksList());
      return;
    case 'task_execution.submitted':
    case 'task_execution.dispatched':
    case 'task_execution.acked':
    case 'task_execution.nacked':
    case 'task_execution.failed':
    case 'task_execution.killed':
    case 'task_execution.kill_requested':
    case 'task_execution.dispatch_rejected':
      invalidate(qk.fleet());
      return;
    case 'task_execution.input_required':
      invalidate(qk.fleet());
      invalidate(qk.inputRequests());
      return;

    // Issue lifecycle — BC-native Issue list/show refresh on any
    // status move (v2.3-5b: qk.issues / qk.issue were added when
    // the SPA stopped going through Conversation BC).
    case 'issue.opened':
    case 'issue.withdrawn':
    case 'issue.concluded':
    case 'issue.tasks_spawned':
    case 'issue.discussion_started':
      invalidate(qk.issues());
      return;

    default:
      // Unknown event type — no-op (forwards-compatible with new server
      // event types added before the dispatch table catches up).
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

import { create } from 'zustand';

// AppStore — cross-page state only (per F4 oversight #3). Server data
// lives in react-query; component-local state in useState/useReducer.
//
// Holds:
//   - currentUserId: identity ref used as the default sender for
//     messages + responder for input requests. Starts EMPTY and is
//     seeded by AppLayout from /api/auth/me (the per-request JWT
//     session identity). It must NOT carry a hardcoded placeholder:
//     a seed value leaks into the initial SSE connection before auth
//     resolves (historically `user:hayang`, removed with the backend
//     DefaultActor in #155/#162). Consumers (useSSE, conversation
//     subscribe) gate on a non-empty value before acting.
//   - sseStatus: connection lifecycle for the SSE banner.
export type SSEStatus = 'idle' | 'connecting' | 'open' | 'reconnecting' | 'closed';

export interface AppState {
  currentUserId: string;
  sseStatus: SSEStatus;
  sseLastEventId: string | null;
  // True while the AddWorkerModal is mounted; lets the global
  // WorkerEnrolledToast suppress itself so we don't show the success
  // card and a toast at the same time (v2.4-D-F4 UI § 6 fallback).
  addWorkerModalOpen: boolean;

  setCurrentUserId: (id: string) => void;
  setSSEStatus: (s: SSEStatus) => void;
  setSSELastEventId: (id: string | null) => void;
  setAddWorkerModalOpen: (open: boolean) => void;
}

export const useAppStore = create<AppState>((set) => ({
  currentUserId: '',
  sseStatus: 'idle',
  sseLastEventId: null,
  addWorkerModalOpen: false,

  setCurrentUserId: (id) => set({ currentUserId: id }),
  setSSEStatus: (s) => set({ sseStatus: s }),
  setSSELastEventId: (id) => set({ sseLastEventId: id }),
  setAddWorkerModalOpen: (open) => set({ addWorkerModalOpen: open }),
}));

import { create } from 'zustand';

// AppStore — cross-page state only (per F4 oversight #3). Server data
// lives in react-query; component-local state in useState/useReducer.
//
// Holds:
//   - currentUserId: identity ref used as the default sender for
//     messages + responder for input requests. Loopback bind makes
//     this single-user; default is `user:hayang` matching the
//     backend's DefaultActor.
//   - sseStatus: connection lifecycle for the SSE banner.
//   - navBadges: counts that decorate the sidebar (unread IRs, etc.)
export type SSEStatus = 'idle' | 'connecting' | 'open' | 'reconnecting' | 'closed';

export interface NavBadges {
  inputRequests: number;
}

export interface AppState {
  currentUserId: string;
  sseStatus: SSEStatus;
  sseLastEventId: string | null;
  navBadges: NavBadges;

  setCurrentUserId: (id: string) => void;
  setSSEStatus: (s: SSEStatus) => void;
  setSSELastEventId: (id: string | null) => void;
  incInputRequestBadge: () => void;
  resetInputRequestBadge: () => void;
}

export const useAppStore = create<AppState>((set) => ({
  currentUserId: 'user:hayang',
  sseStatus: 'idle',
  sseLastEventId: null,
  navBadges: { inputRequests: 0 },

  setCurrentUserId: (id) => set({ currentUserId: id }),
  setSSEStatus: (s) => set({ sseStatus: s }),
  setSSELastEventId: (id) => set({ sseLastEventId: id }),
  incInputRequestBadge: () =>
    set((s) => ({ navBadges: { ...s.navBadges, inputRequests: s.navBadges.inputRequests + 1 } })),
  resetInputRequestBadge: () =>
    set((s) => ({ navBadges: { ...s.navBadges, inputRequests: 0 } })),
}));

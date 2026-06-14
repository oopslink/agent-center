import type React from 'react';
import { createContext, useContext, useState } from 'react';
import type { Message } from '@/api/types';
import { ThreadSidebar } from './ThreadSidebar';

// v2.9.1 Threads (P1): a tiny context that lifts the ONE ThreadSidebar's
// open-state to a surface boundary (mounted by ConversationView), so every
// per-message ThreadButton across the conversation drives the same single
// sidebar. Mirrors the SenderSidebarContext pattern.
//
// `useThreadSidebar()` returns the opener `(root) => void`, or null when there
// is no provider — MessageList PREFERS this opener and falls back to its own
// local sidebar when rendered standalone (its existing tests render it without
// the provider), so threads work in both wirings.

type OpenThread = (root: Message) => void;

const ThreadSidebarContext = createContext<OpenThread | null>(null);

export function ThreadSidebarProvider({
  children,
}: {
  children: React.ReactNode;
}): React.ReactElement {
  // Holds the clicked root message; null = closed.
  const [root, setRoot] = useState<Message | null>(null);
  return (
    <ThreadSidebarContext.Provider value={setRoot}>
      {children}
      <ThreadSidebar open={root !== null} rootMessage={root} onClose={() => setRoot(null)} />
    </ThreadSidebarContext.Provider>
  );
}

// useThreadSidebar returns the context opener, or null when there is no provider.
export function useThreadSidebar(): OpenThread | null {
  return useContext(ThreadSidebarContext);
}

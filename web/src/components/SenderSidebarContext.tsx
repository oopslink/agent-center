import type React from 'react';
import { createContext, useContext, useState } from 'react';
import { SenderDetailSidebar } from './SenderDetailSidebar';

// v2.8.1 #281 (mention-sidebar): the SenderDetailSidebar's open-state setter,
// lifted into a tiny context so MULTIPLE click entries can open the ONE existing
// kind-routed sidebar:
//   ① the DMDetail header peer (avatar + @name) — lives ABOVE the message list,
//   ② @mention tokens INSIDE message content (MarkdownMessage),
//   ③ the existing message sender name/avatar (MessageList) — unchanged path.
// All three call the same `openSender(ref)`. The provider owns the state and
// renders a SINGLE <SenderDetailSidebar> instance (the sidebar already fetches
// by ref + dispatches agent:/user: → AgentDetailBody/UserDetailBody, and already
// owns Esc/overlay close). No new sidebar, no new fetch.
//
// `openSender` is the v2.8.1 equivalent of MessageList's old local
// `setSidebarSender`. Components consume it via `useOpenSender()`, which returns
// a no-op-safe opener when there is NO provider — so MessageList still works
// standalone (its existing tests render it without this provider) by keeping its
// own local sidebar fallback.

type OpenSender = (ref: string) => void;

const SenderSidebarContext = createContext<OpenSender | null>(null);

// SenderSidebarProvider — owns the sidebar open state and renders the single
// SenderDetailSidebar for everything under it (a page/surface boundary, e.g.
// DMDetail). Children call `useOpenSender()` to open it.
export function SenderSidebarProvider({
  children,
}: {
  children: React.ReactNode;
}): React.ReactElement {
  // Holds the clicked identity ref (prefixed, e.g. "agent:A-1" / "user:hayang");
  // null = closed.
  const [senderRef, setSenderRef] = useState<string | null>(null);
  return (
    <SenderSidebarContext.Provider value={setSenderRef}>
      {children}
      <SenderDetailSidebar
        open={senderRef !== null}
        senderRef={senderRef}
        onClose={() => setSenderRef(null)}
      />
    </SenderSidebarContext.Provider>
  );
}

// useSenderSidebar returns the context opener, or null when there is no provider.
// MessageList uses this to PREFER the provider's single sidebar (so the header /
// mention / sender entries all drive one panel) and fall back to its own local
// sidebar when rendered standalone.
export function useSenderSidebar(): OpenSender | null {
  return useContext(SenderSidebarContext);
}

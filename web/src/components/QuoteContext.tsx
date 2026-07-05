import type React from 'react';
import { createContext, useContext, useState } from 'react';
import type { Message } from '@/api/types';

// 引用 (quote): a tiny context that lifts the "message being quoted" to a surface
// boundary (mounted by ConversationView), so the per-message Quote action in the
// MessageList and the quoting-bar in the MessageComposer share ONE piece of
// state. Mirrors the SenderSidebarContext / ThreadSidebarContext pattern.
//
// Unlike those (which expose only an opener), quoting is bidirectional: the list
// SETS the target when the user clicks Quote, while the composer READS it (to
// render the preview bar) and CLEARS it (x / after a successful send). So the
// context value carries both the current target and its setter.
//
// `useQuote()` returns the value, or null when there is no provider — the
// MessageList only renders the Quote action when a provider is present, and the
// MessageComposer simply omits the quoting bar; both keep working standalone
// (their existing tests render them without the provider).

interface QuoteContextValue {
  // The message currently being quoted, or null when nothing is queued.
  target: Message | null;
  // Set (or clear, with null) the quote target.
  setTarget: (target: Message | null) => void;
}

const QuoteContext = createContext<QuoteContextValue | null>(null);

// QuoteProvider — owns the quote target for everything under it (a
// conversation surface, mounted by ConversationView).
export function QuoteProvider({
  children,
}: {
  children: React.ReactNode;
}): React.ReactElement {
  const [target, setTarget] = useState<Message | null>(null);
  return (
    <QuoteContext.Provider value={{ target, setTarget }}>
      {children}
    </QuoteContext.Provider>
  );
}

// useQuote returns the quote target + setter, or null when there is no provider.
export function useQuote(): QuoteContextValue | null {
  return useContext(QuoteContext);
}

import { useRef } from 'react';
import type React from 'react';

// useTablistKeyboard adds WAI-ARIA keyboard support to a `role="tablist"` whose
// tabs are controlled (a key per tab + an active key). It powers AgentDetail
// (#228 back-write — it previously had click-only tabs) and WorkerDetail (#273),
// so arrow-nav is implemented once and correct in both (institutional, v2.8 #273
// Q1 lock). Unit-testing this hook once covers both surfaces.
//
// WAI-ARIA Tabs pattern:
//   - Roving tabindex: only the active tab is in the Tab order (tabIndex 0);
//     the rest are tabIndex -1. So Tab moves INTO the tablist once, then arrows
//     move BETWEEN tabs (not Tab through every tab).
//   - ArrowRight/Down → next, ArrowLeft/Up → previous (wraps); Home → first,
//     End → last. Each moves selection AND focus to that tab.
//
// Usage:
//   const { tablistRef, onKeyDown, tabIndexFor } = useTablistKeyboard({
//     keys: TAB_KEYS, active: tab, onActivate: setTab,
//   });
//   <nav role="tablist" aria-orientation="horizontal" ref={tablistRef} onKeyDown={onKeyDown}>
//     {TABS.map(t => <button role="tab" aria-selected={tab===t.key} tabIndex={tabIndexFor(t.key)} ...>)}
//   </nav>
export function useTablistKeyboard<K extends string>({
  keys,
  active,
  onActivate,
}: {
  keys: readonly K[];
  active: K;
  onActivate: (k: K) => void;
}) {
  const tablistRef = useRef<HTMLElement>(null);

  const onKeyDown = (e: React.KeyboardEvent) => {
    const idx = keys.indexOf(active);
    if (idx < 0) return;
    let next: number;
    switch (e.key) {
      case 'ArrowRight':
      case 'ArrowDown':
        next = (idx + 1) % keys.length;
        break;
      case 'ArrowLeft':
      case 'ArrowUp':
        next = (idx - 1 + keys.length) % keys.length;
        break;
      case 'Home':
        next = 0;
        break;
      case 'End':
        next = keys.length - 1;
        break;
      default:
        return; // not a tablist nav key — let it through
    }
    e.preventDefault();
    onActivate(keys[next]);
    // Move focus to the newly-selected tab (roving). Tabs render in `keys` order.
    const tabs = tablistRef.current?.querySelectorAll<HTMLElement>('[role="tab"]');
    tabs?.[next]?.focus();
  };

  const tabIndexFor = (k: K): 0 | -1 => (k === active ? 0 : -1);

  return { tablistRef, onKeyDown, tabIndexFor };
}

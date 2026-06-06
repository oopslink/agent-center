import { useRef, useState } from 'react';
import type React from 'react';

// useTablistKeyboard adds WAI-ARIA **manual-activation** keyboard support to a
// `role="tablist"` whose tabs are controlled (a key per tab + an active key). It
// powers AgentDetail (#228 back-write — tabs were click-only) and WorkerDetail
// (#273), so arrow-nav is implemented once and correct in both (v2.8 #273 Q1
// lock). Unit-testing this hook once covers both surfaces.
//
// MANUAL activation (PD + Dev2 locked, WAI-ARIA APG for async/heavy panels):
// arrow keys only MOVE FOCUS (roving), they do NOT switch the active tab — so
// arrow-scanning a tablist never fires the async fetches behind heavy tabs
// (WorkerDetail "Bound Agents" GET /api/agents?worker_id=, AgentDetail Activity
// #274 cursor). Activation is the tab `<button>`'s NATIVE behavior: Enter / Space
// / click fire its onClick → the consumer's setTab. So this hook is pure roving
// focus; the consumer keeps onClick for activation.
//
//   - Roving tabindex: the roving tab (focusedKey ?? active) is tabIndex 0, the
//     rest -1. Tab enters the tablist at the active tab; arrows move focus within.
//   - ArrowRight/Down → next, ArrowLeft/Up → previous (wraps); Home → first,
//     End → last. Each moves FOCUS only (not selection).
//   - On blur (focus leaves the tablist) the roving resets to the active tab, so
//     re-entering with Tab lands on the active tab, not the last-scanned one.
//
// Usage:
//   const tl = useTablistKeyboard({ keys: TAB_KEYS, active: tab });
//   <nav role="tablist" aria-orientation="horizontal" ref={tl.tablistRef}
//        onKeyDown={tl.onKeyDown} onBlur={tl.onBlur}>
//     {TABS.map(t => (
//       <button role="tab" aria-selected={tab===t.key} tabIndex={tl.tabIndexFor(t.key)}
//               aria-controls={`panel-${t.key}`} id={`tab-${t.key}`}
//               onClick={() => setTab(t.key)}>{t.label}</button>
//     ))}
//   </nav>
//   <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`} tabIndex={0}>…</div>
export function useTablistKeyboard<K extends string>({
  keys,
  active,
}: {
  keys: readonly K[];
  active: K;
}) {
  const tablistRef = useRef<HTMLElement>(null);
  // Which tab currently owns roving focus. null = follow `active` (the default /
  // post-blur state), so Tab always re-enters at the active tab.
  const [focusedKey, setFocusedKey] = useState<K | null>(null);
  const roving: K = focusedKey ?? active;

  const focusTab = (k: K) => {
    setFocusedKey(k);
    const i = keys.indexOf(k);
    if (i >= 0) {
      // tabIndex=-1 tabs are still programmatically focusable.
      tablistRef.current?.querySelectorAll<HTMLElement>('[role="tab"]')[i]?.focus();
    }
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    const idx = keys.indexOf(roving);
    if (idx < 0) return;
    const n = keys.length;
    switch (e.key) {
      case 'ArrowRight':
      case 'ArrowDown':
        e.preventDefault();
        focusTab(keys[(idx + 1) % n]);
        break;
      case 'ArrowLeft':
      case 'ArrowUp':
        e.preventDefault();
        focusTab(keys[(idx - 1 + n) % n]);
        break;
      case 'Home':
        e.preventDefault();
        focusTab(keys[0]);
        break;
      case 'End':
        e.preventDefault();
        focusTab(keys[n - 1]);
        break;
      default:
        // Enter / Space / other keys: let the native <button> handle activation.
        return;
    }
  };

  // Reset roving to the active tab when focus leaves the tablist entirely.
  const onBlur = (e: React.FocusEvent) => {
    if (!tablistRef.current?.contains(e.relatedTarget as Node | null)) {
      setFocusedKey(null);
    }
  };

  const tabIndexFor = (k: K): 0 | -1 => (k === roving ? 0 : -1);

  return { tablistRef, onKeyDown, onBlur, tabIndexFor };
}

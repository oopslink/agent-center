import { useEffect } from 'react';

// useKeyShortcuts — global keyboard shortcut registrar. Designed to be
// installed once at the top of the app (AppLayout) and given a small
// map. Modifier matches Cmd on macOS + Ctrl elsewhere — the common
// "primary modifier" pattern.
//
// Shortcuts are intentionally non-overlapping with browser defaults:
//   - Cmd/Ctrl + K   open command palette
//   - Cmd/Ctrl + B   toggle sidebar
//   - Cmd/Ctrl + D   toggle dark mode
//   - Cmd/Ctrl + 1..7 jump to top-level page
//
// The hook skips firing when the user is typing in an input / textarea
// so /1/2/etc. inside a message draft still type literally — except
// Cmd/Ctrl-prefixed ones always fire (those are platform-conventional
// "global" shortcuts).

export type ShortcutMap = Record<string, () => void>;

function isEditable(target: EventTarget | null): boolean {
  if (!target || !(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (target.isContentEditable) return true;
  return false;
}

export function useKeyShortcuts(map: ShortcutMap, enabled = true): void {
  useEffect(() => {
    if (!enabled) return;
    const handler = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;
      const key = e.key.toLowerCase();
      // Build a canonical chord string: "mod+k", "mod+1", etc.
      const chord = `${mod ? 'mod+' : ''}${key}`;
      const fn = map[chord];
      if (!fn) return;
      // Bare (non-mod) shortcuts are suppressed when the user is in
      // an editable field — typing 'k' shouldn't trigger palette.
      if (!mod && isEditable(e.target)) return;
      e.preventDefault();
      fn();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [map, enabled]);
}

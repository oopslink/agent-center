import { useCallback, useMemo, useState } from 'react';

// useSelection — local state for the derive UI's multi-select mode.
// Used by every conversation timeline (channel / dm / issue / task)
// so behaviour stays uniform. Selection state lives in the page
// component, not in Zustand, because it's strictly view-local.

export interface SelectionState {
  selectMode: boolean;
  selected: ReadonlySet<string>;
  count: number;
  toggleSelectMode: () => void;
  exitSelectMode: () => void;
  toggle: (id: string) => void;
  isSelected: (id: string) => boolean;
  clear: () => void;
}

export function useSelection(): SelectionState {
  const [selectMode, setSelectMode] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const toggleSelectMode = useCallback(() => {
    setSelectMode((prev) => {
      // Entering or exiting select mode both clear the current set so
      // partial selections from earlier sessions don't bleed through.
      setSelected(new Set());
      return !prev;
    });
  }, []);

  const exitSelectMode = useCallback(() => {
    setSelectMode(false);
    setSelected(new Set());
  }, []);

  const toggle = useCallback((id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const isSelected = useCallback(
    (id: string) => selected.has(id),
    [selected],
  );

  const clear = useCallback(() => setSelected(new Set()), []);

  return useMemo(
    () => ({
      selectMode,
      selected,
      count: selected.size,
      toggleSelectMode,
      exitSelectMode,
      toggle,
      isSelected,
      clear,
    }),
    [selectMode, selected, toggleSelectMode, exitSelectMode, toggle, isSelected, clear],
  );
}

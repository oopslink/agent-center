import type React from 'react';
import { useCallback, useEffect, useRef, useState } from 'react';

// useResizablePanel (v2.10.x desktop) — a small, surface-agnostic hook that owns
// a single panel's draggable width: pointer-drag from an edge handle, keyboard
// arrows for a11y, clamp to [minWidth, maxWidth], and localStorage persistence so
// the chosen width survives reloads. Shared by the ThreadSidebar (task-97c7600a)
// and the col④ context-panel column (AppLayout, T128) so the resize interaction is
// identical across both — one hook, one contract.
//
// Anchoring: both panels sit on the RIGHT of the screen, so the handle is on the
// panel's LEFT edge (edge: 'left') — dragging left (clientX decreasing) widens.
// `maxWidth` may be a function evaluated on every clamp, so a viewport-relative cap
// (e.g. 75vw for the Thread panel) re-clamps as the window resizes.

const STEP = 24; // px per arrow keypress (keyboard resize)

export interface ResizablePanelOptions {
  /** localStorage key the width (px) is persisted under. */
  storageKey: string;
  /** initial width (px) used when nothing is stored. */
  defaultWidth: number;
  /** smallest allowed width (px). */
  minWidth: number;
  /** largest allowed width — a fixed px number, or a function evaluated on every clamp. */
  maxWidth: number | (() => number);
  /**
   * which edge the drag handle sits on. 'left' (default) — handle on the panel's
   * left edge, dragging left widens (right-anchored panels). 'right' mirrors it.
   */
  edge?: 'left' | 'right';
}

export interface ResizablePanel {
  /** current width in px (already clamped + persisted). */
  width: number;
  /** true while a drag is in progress — drive a no-select / active-handle style. */
  resizing: boolean;
  /** spread onto the drag-handle element. */
  handleProps: {
    onMouseDown: (e: React.MouseEvent) => void;
    onKeyDown: (e: React.KeyboardEvent) => void;
  };
}

function resolveMax(maxWidth: number | (() => number)): number {
  return typeof maxWidth === 'function' ? maxWidth() : maxWidth;
}

function readStored(key: string): number | null {
  try {
    if (typeof localStorage === 'undefined' || typeof localStorage.getItem !== 'function') return null;
    const raw = localStorage.getItem(key);
    if (raw == null) return null;
    const n = Number(raw);
    return Number.isFinite(n) ? n : null;
  } catch {
    return null;
  }
}

function writeStored(key: string, value: number): void {
  try {
    if (typeof localStorage !== 'undefined' && typeof localStorage.setItem === 'function') {
      localStorage.setItem(key, String(Math.round(value)));
    }
  } catch {
    // ignore — width persistence is best-effort
  }
}

export function useResizablePanel(opts: ResizablePanelOptions): ResizablePanel {
  const { storageKey, defaultWidth, minWidth, maxWidth, edge = 'left' } = opts;

  const clamp = useCallback(
    (w: number) => Math.max(minWidth, Math.min(resolveMax(maxWidth), w)),
    [minWidth, maxWidth],
  );

  const [width, setWidthState] = useState<number>(() => clamp(readStored(storageKey) ?? defaultWidth));
  const [resizing, setResizing] = useState(false);

  const setWidth = useCallback(
    (w: number) => {
      const next = clamp(w);
      setWidthState(next);
      writeStored(storageKey, next);
    },
    [clamp, storageKey],
  );

  // Re-clamp when the viewport changes so a relative max (e.g. 75vw) can never be
  // exceeded after the window shrinks. State-only (no persist) — a viewport pull-in
  // shouldn't overwrite the user's intended width.
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const onResize = () => setWidthState((w) => clamp(w));
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, [clamp]);

  const drag = useRef<{ startX: number; startW: number } | null>(null);

  // While dragging, listen on the WINDOW (not the thin handle) so a fast drag that
  // outruns the cursor off the grip keeps resizing until mouseup anywhere.
  useEffect(() => {
    if (!resizing || typeof window === 'undefined') return;
    const onMove = (e: MouseEvent) => {
      const d = drag.current;
      if (!d) return;
      const dx = e.clientX - d.startX;
      // left-edge handle on a right-anchored panel: moving left (dx<0) widens.
      setWidth(d.startW + (edge === 'left' ? -dx : dx));
    };
    const onUp = () => {
      drag.current = null;
      setResizing(false);
    };
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
    return () => {
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };
  }, [resizing, edge, setWidth]);

  const onMouseDown = useCallback(
    (e: React.MouseEvent) => {
      drag.current = { startX: e.clientX, startW: width };
      setResizing(true);
      e.preventDefault(); // suppress text selection while dragging
    },
    [width],
  );

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      const grow = edge === 'left' ? 'ArrowLeft' : 'ArrowRight';
      const shrink = edge === 'left' ? 'ArrowRight' : 'ArrowLeft';
      if (e.key === grow) {
        setWidth(width + STEP);
        e.preventDefault();
      } else if (e.key === shrink) {
        setWidth(width - STEP);
        e.preventDefault();
      }
    },
    [edge, setWidth, width],
  );

  return {
    width,
    resizing,
    handleProps: { onMouseDown, onKeyDown },
  };
}

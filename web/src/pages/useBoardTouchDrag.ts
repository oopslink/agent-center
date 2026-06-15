import type React from 'react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { resolveTargetFromPoint, type DropTarget } from './boardDrop';

// useBoardTouchDrag (v2.10.1 M5) — long-press touch drag for the Work Board cards.
// HTML5 drag-and-drop does not start from touch on most phones, so on a touch
// pointer we run our own gesture: press-and-hold a card (~320ms) to pick it up,
// drag it over a column, release to drop. It reuses the board's existing
// `dragSource` state (set via onStart) so the columns' `data-droppable` validity
// is computed exactly as for mouse DnD, and the hit-test only accepts droppable
// columns. A mouse pointer is ignored here — it keeps the native HTML5 drag.
//
// A short pre-drag move (before the hold fires) is treated as a scroll and cancels
// the pickup, so horizontal column scrolling still works one-handed.

const LONG_PRESS_MS = 320;
const MOVE_CANCEL_PX = 10;

export interface TouchDragPreview {
  taskId: string;
  title: string;
  x: number;
  y: number;
}

interface Options {
  /** picked up — set the board's dragSource so columns light their drop zones. */
  onStart: (taskId: string, fromPlanId: string | null) => void;
  /** released/cancelled — clear the board's dragSource. */
  onEnd: () => void;
  /** released over a valid column — run the planning mutation. */
  onDrop: (taskId: string, fromPlanId: string | null, target: DropTarget) => void;
}

export function useBoardTouchDrag(opts: Options): {
  preview: TouchDragPreview | null;
  startLongPress: (
    e: React.PointerEvent,
    taskId: string,
    fromPlanId: string | null,
    title: string,
  ) => void;
} {
  const optsRef = useRef(opts);
  optsRef.current = opts;
  const [preview, setPreview] = useState<TouchDragPreview | null>(null);
  const st = useRef<{
    timer: number;
    sx: number;
    sy: number;
    info: { taskId: string; fromPlanId: string | null; title: string } | null;
    dragging: boolean;
  }>({ timer: 0, sx: 0, sy: 0, info: null, dragging: false });

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const onMove = (e: PointerEvent) => {
      const s = st.current;
      if (!s.info) return;
      if (!s.dragging) {
        // still in the press-and-hold window: a real move = a scroll → cancel.
        if (Math.hypot(e.clientX - s.sx, e.clientY - s.sy) > MOVE_CANCEL_PX) {
          window.clearTimeout(s.timer);
          s.info = null;
        }
        return;
      }
      e.preventDefault(); // we own the gesture now (suppress scroll)
      setPreview((p) => (p ? { ...p, x: e.clientX, y: e.clientY } : p));
    };
    const onUp = (e: PointerEvent) => {
      const s = st.current;
      if (s.timer) window.clearTimeout(s.timer);
      if (s.dragging && s.info) {
        const target = resolveTargetFromPoint(e.clientX, e.clientY);
        if (target) optsRef.current.onDrop(s.info.taskId, s.info.fromPlanId, target);
        optsRef.current.onEnd();
      }
      s.timer = 0;
      s.info = null;
      s.dragging = false;
      setPreview(null);
    };
    window.addEventListener('pointermove', onMove, { passive: false });
    window.addEventListener('pointerup', onUp);
    window.addEventListener('pointercancel', onUp);
    return () => {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      window.removeEventListener('pointercancel', onUp);
    };
  }, []);

  const startLongPress = useCallback(
    (e: React.PointerEvent, taskId: string, fromPlanId: string | null, title: string) => {
      // Mouse keeps the native HTML5 drag; only touch/pen use the long-press path.
      if (e.pointerType === 'mouse') return;
      const s = st.current;
      s.sx = e.clientX;
      s.sy = e.clientY;
      s.info = { taskId, fromPlanId, title };
      s.dragging = false;
      window.clearTimeout(s.timer);
      s.timer = window.setTimeout(() => {
        if (!s.info) return;
        s.dragging = true;
        optsRef.current.onStart(s.info.taskId, s.info.fromPlanId);
        setPreview({ taskId: s.info.taskId, title: s.info.title, x: s.sx, y: s.sy });
      }, LONG_PRESS_MS);
    },
    [],
  );

  return { preview, startLongPress };
}

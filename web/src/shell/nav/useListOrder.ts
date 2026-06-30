import type React from 'react';
import { useCallback, useMemo, useState } from 'react';
import { mergeOrder, moveId, readOrder, writeOrder } from './listOrder';

// Props spread onto a draggable row (<li>) to make a nav list reorderable.
export interface RowDragProps {
  draggable: true;
  onDragStart: (e: React.DragEvent) => void;
  onDragOver: (e: React.DragEvent) => void;
  onDragEnter: (e: React.DragEvent) => void;
  onDrop: (e: React.DragEvent) => void;
  onDragEnd: () => void;
  'data-dragging'?: string;
  'data-drop-over'?: string;
}

export interface ListOrder {
  /** current ids in the user's saved order (new ids appended, stale dropped). */
  orderedIds: string[];
  draggingId: string | null;
  overId: string | null;
  /** spread onto each row's <li> to wire HTML5 drag-reorder + state attributes. */
  rowProps: (id: string) => RowDragProps;
}

// useListOrder — manage a persisted manual order for one nav list and expose
// HTML5 drag handlers per row. `key` is the localStorage sub-key (callers scope
// it per org + list, e.g. `<orgBase>/channels`). `currentIds` is the live set of
// ids from the data; the returned orderedIds applies the saved order over them.
//
// Drag is mouse/pointer HTML5 DnD (no dependency). Touch reordering is not wired
// here yet — taps still navigate normally on touch.
export function useListOrder(key: string, currentIds: string[]): ListOrder {
  const [saved, setSaved] = useState<string[]>(() => readOrder(key));
  const [draggingId, setDraggingId] = useState<string | null>(null);
  const [overId, setOverId] = useState<string | null>(null);

  // currentIds is a fresh array each render; join for a stable memo dependency.
  const currentKey = currentIds.join(',');
  const orderedIds = useMemo(
    () => mergeOrder(saved, currentIds),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [saved, currentKey],
  );

  const commitDrop = useCallback(
    (targetId: string) => {
      setDraggingId((drag) => {
        if (drag && drag !== targetId) {
          const next = moveId(orderedIds, drag, targetId);
          writeOrder(key, next);
          setSaved(next);
        }
        return null;
      });
      setOverId(null);
    },
    [key, orderedIds],
  );

  const rowProps = useCallback(
    (id: string): RowDragProps => ({
      draggable: true,
      onDragStart: (e: React.DragEvent) => {
        setDraggingId(id);
        if (e.dataTransfer) {
          e.dataTransfer.effectAllowed = 'move';
          // Firefox requires data to be set for a drag to start.
          try {
            e.dataTransfer.setData('text/plain', id);
          } catch {
            /* jsdom / restricted dataTransfer */
          }
        }
      },
      onDragOver: (e: React.DragEvent) => {
        e.preventDefault();
        if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
        setOverId(id);
      },
      onDragEnter: (e: React.DragEvent) => {
        e.preventDefault();
        setOverId(id);
      },
      onDrop: (e: React.DragEvent) => {
        e.preventDefault();
        commitDrop(id);
      },
      onDragEnd: () => {
        setDraggingId(null);
        setOverId(null);
      },
      'data-dragging': draggingId === id ? '' : undefined,
      'data-drop-over': overId === id && draggingId !== id ? '' : undefined,
    }),
    [draggingId, overId, commitDrop],
  );

  return { orderedIds, draggingId, overId, rowProps };
}

// rowDragClass — shared visual feedback for a reorderable row: grab cursor, the
// dragged row dimmed, and a top accent line on the row being dropped onto.
export function rowDragClass(order: ListOrder, id: string): string {
  return [
    'cursor-grab',
    order.draggingId === id ? 'opacity-50' : '',
    order.overId === id && order.draggingId !== id ? 'rounded-none border-t-2 border-brand' : '',
  ]
    .filter(Boolean)
    .join(' ');
}

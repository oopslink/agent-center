import type React from 'react';
import { useId, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { useModalA11y } from '@/components/useModalA11y';

// Downward drag distance (px) past which releasing the grab handle closes the
// sheet. Below this threshold the drag is treated as a cancel (no visual
// drag-follow / spring-back animation — see mobile-redesign-nav-framework.md
// §4 for the "drag to close" requirement; a physics-based sheet is overkill).
const DRAG_CLOSE_THRESHOLD_PX = 80;

// ============================================================================
// v2.10.1 [M1] BottomSheet — the reusable mobile bottom-sheet primitive.
//
// The mobile IA (<768) reflows the desktop col④ context column and other
// secondary surfaces into a bottom sheet that slides up over the content
// (mockup `docs/design/v2.10.1/v2.10.1-mobile` — `.sheet`). This component owns
// only the *chrome*: the scrim, the rounded sheet surface anchored to the
// bottom edge, a grab handle, an optional title, and the dialog a11y wiring
// (focus-trap + Escape + focus restore via useModalA11y). Callers fill it with
// content (participants/files, an account menu, …).
//
// It is mobile-only by design (`md:hidden`): the desktop shell keeps its real
// columns. Render it conditionally — `<BottomSheet open={…}>` — and gate the
// trigger behind the same `md:hidden` chrome.
//
// Touch targets inside a sheet must stay ≥44px (the v2.10.1 touch baseline);
// the scrim itself is dismiss-on-tap.
// ============================================================================
export interface BottomSheetProps {
  open: boolean;
  onClose: () => void;
  /** Visible sheet heading (also wires aria-labelledby). */
  title?: React.ReactNode;
  /** Accessible name when there is no visible `title`. */
  ariaLabel?: string;
  /** testid for the role="dialog" sheet surface. */
  testId?: string;
  children: React.ReactNode;
}

export function BottomSheet({
  open,
  onClose,
  title,
  ariaLabel,
  testId,
  children,
}: BottomSheetProps): React.ReactElement | null {
  const containerRef = useModalA11y({ open, onClose });
  const titleId = useId();
  const { t } = useTranslation('common');
  const dragStartYRef = useRef<number | null>(null);
  const dragLastYRef = useRef<number | null>(null);

  const onHandlePointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    dragStartYRef.current = e.clientY;
    dragLastYRef.current = e.clientY;
    // Best-effort: pointer capture keeps the drag tracked even if the
    // pointer leaves the handle's bounds. Not implemented by jsdom (tests)
    // and unnecessary for correctness — pointerup/pointercancel still fire.
    try {
      e.currentTarget.setPointerCapture(e.pointerId);
    } catch {
      /* ignore — unsupported in this environment */
    }
  };
  const onHandlePointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (dragStartYRef.current === null) return;
    dragLastYRef.current = e.clientY;
  };
  const endDrag = (e: React.PointerEvent<HTMLDivElement>) => {
    const startY = dragStartYRef.current;
    const lastY = dragLastYRef.current ?? e.clientY;
    dragStartYRef.current = null;
    dragLastYRef.current = null;
    if (startY === null) return;
    const delta = lastY - startY;
    if (delta >= DRAG_CLOSE_THRESHOLD_PX) {
      onClose();
    }
  };

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-40 flex flex-col justify-end md:hidden">
      {/* Scrim — tap to dismiss. Not a focus stop (the trap lives in the sheet). */}
      <button
        type="button"
        aria-label={t('shell.bottomSheet.close')}
        tabIndex={-1}
        onClick={onClose}
        data-testid="bottom-sheet-scrim"
        className="absolute inset-0 bg-black/40 motion-safe:transition-opacity"
      />
      <div
        ref={containerRef}
        role="dialog"
        aria-modal="true"
        aria-label={title ? undefined : ariaLabel}
        aria-labelledby={title ? titleId : undefined}
        data-testid={testId}
        className="relative max-h-[70vh] overflow-y-auto rounded-t-2xl border-t border-border-strong bg-bg-elevated px-4 pb-[calc(env(safe-area-inset-bottom)+1rem)] pt-2 shadow-2"
      >
        {/* Generous invisible hit-target around the grab handle so a real
            touchscreen can grab it — the visible bar is only 4px tall.
            Pointer events (not touch-specific) cover mouse + touch alike. */}
        <div
          data-testid="bottom-sheet-drag-handle"
          className="-mx-4 flex cursor-grab touch-none justify-center py-3 active:cursor-grabbing"
          onPointerDown={onHandlePointerDown}
          onPointerMove={onHandlePointerMove}
          onPointerUp={endDrag}
          onPointerCancel={endDrag}
        >
          <div aria-hidden="true" className="mb-0 h-1 w-9 rounded-full bg-border-strong" />
        </div>
        {title && (
          <h2 id={titleId} className="mb-3 text-sm font-semibold text-text-primary">
            {title}
          </h2>
        )}
        {children}
      </div>
    </div>
  );
}

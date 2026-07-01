import type React from 'react';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { useModalA11y } from '@/components/useModalA11y';

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
        className="relative max-h-[85vh] overflow-y-auto rounded-t-2xl border-t border-border-strong bg-bg-elevated px-4 pb-[calc(env(safe-area-inset-bottom)+1rem)] pt-2 shadow-2"
      >
        <div aria-hidden="true" className="mx-auto mb-3 h-1 w-9 rounded-full bg-border-strong" />
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

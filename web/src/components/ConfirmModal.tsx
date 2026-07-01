import type React from 'react';
import { useTranslation } from 'react-i18next';
import { useModalA11y } from './useModalA11y';

interface ConfirmModalProps {
  open: boolean;
  title: string;
  /** Optional body. Multi-line strings render with their line breaks preserved. */
  message?: React.ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  /** Style the confirm button as a destructive action. */
  danger?: boolean;
  /** Disable both buttons while the confirmed action is in flight. */
  busy?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

// ConfirmModal — accessible replacement for native window.confirm (task #169).
// Renders a focus-trapped role="dialog" (Escape / Tab handled by useModalA11y),
// so confirmation flows match the rest of the app's modal UX instead of the
// browser's blocking, unstyled dialog. window.confirm/alert/prompt are banned
// via ESLint no-restricted-globals to prevent regressions.
export function ConfirmModal({
  open,
  title,
  message,
  confirmLabel,
  cancelLabel,
  danger = false,
  busy = false,
  onConfirm,
  onCancel,
}: ConfirmModalProps): React.ReactElement | null {
  const { t } = useTranslation('common');
  const containerRef = useModalA11y({ open, onClose: onCancel });
  if (!open) return null;

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-20 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="confirm-modal-title"
      data-testid="confirm-modal"
    >
      <div className="w-full max-w-sm rounded-xl border border-border-base bg-bg-elevated p-6 text-text-primary shadow-[var(--shadow-3)]">
        <h2 id="confirm-modal-title" className="text-lg font-semibold">
          {title}
        </h2>
        {message != null && (
          <div
            className="mt-2 whitespace-pre-line text-sm text-text-secondary"
            data-testid="confirm-modal-message"
          >
            {message}
          </div>
        )}
        <div className="mt-6 flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            disabled={busy}
            className="rounded px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle disabled:opacity-50"
            data-testid="confirm-modal-cancel"
          >
            {cancelLabel ?? t('confirmModal.cancel')}
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={busy}
            className={
              danger
                ? 'rounded bg-danger px-3 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50'
                : 'rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-medium text-btn-primary-fg hover:opacity-90 disabled:opacity-50'
            }
            data-testid="confirm-modal-confirm"
          >
            {confirmLabel ?? t('confirmModal.confirm')}
          </button>
        </div>
      </div>
    </div>
  );
}

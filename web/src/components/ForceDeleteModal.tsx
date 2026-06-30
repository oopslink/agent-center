import type React from 'react';
import { useState } from 'react';
import { Trans, useTranslation } from 'react-i18next';
import { useModalA11y } from './useModalA11y';

interface ForceDeleteModalProps {
  open: boolean;
  /** What is being force-deleted — drives the warning copy + labels. */
  entityKind: 'agent' | 'worker';
  /** Exact name the user must type to enable the destructive button. */
  entityName: string;
  /** Disable inputs/buttons while the force-delete request is in flight. */
  busy?: boolean;
  /** Server/error message to surface (keeps the modal open). */
  error?: string | null;
  onConfirm: () => void;
  onCancel: () => void;
}

// ForceDeleteModal (v2.8.1) — a GitHub-style typed-name confirmation for the
// destructive "force delete" admin action. Mirrors ConfirmModal's accessible
// shell (useModalA11y → focus-trap + Escape + focus-restore, role="dialog",
// the `bg-danger` danger button token, a `busy` in-flight state) and ADDS a
// typed-name gate: the confirm button stays disabled until the typed value
// matches `entityName` EXACTLY (and while busy). Force delete cleans the
// center's metadata only — the backend skips stop/active guards and does NOT
// kill the process; for a worker it also unbinds its agents.
export function ForceDeleteModal({
  open,
  entityKind,
  entityName,
  busy = false,
  error = null,
  onConfirm,
  onCancel,
}: ForceDeleteModalProps): React.ReactElement | null {
  const { t } = useTranslation('common');
  const containerRef = useModalA11y({ open, onClose: onCancel });
  const [typed, setTyped] = useState('');
  if (!open) return null;

  const matches = typed === entityName;

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-20 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="force-delete-modal-title"
      data-testid="force-delete-modal"
    >
      <div className="w-full max-w-md rounded-lg bg-bg-elevated p-6 text-text-primary shadow-lg">
        <h2 id="force-delete-modal-title" className="text-lg font-semibold">
          {t('forceDeleteModal.title', { entityKind })}
        </h2>
        <div className="mt-2 space-y-2 text-sm text-text-secondary" data-testid="force-delete-message">
          <p>
            <Trans
              i18nKey={
                entityKind === 'worker'
                  ? 'forceDeleteModal.bodyWorker'
                  : 'forceDeleteModal.bodyAgent'
              }
              values={{ entityKind, entityName }}
              components={{ strong: <strong /> }}
            />
          </p>
          <p>
            <Trans
              i18nKey="forceDeleteModal.typeToEnable"
              values={{ entityKind, entityName }}
              components={{ strong: <strong /> }}
            />
          </p>
        </div>

        <label
          htmlFor="force-delete-input"
          className="mt-4 mb-1 block text-xs font-medium text-text-primary"
        >
          {entityKind === 'worker'
            ? t('forceDeleteModal.workerNameLabel')
            : t('forceDeleteModal.agentNameLabel')}
        </label>
        <input
          id="force-delete-input"
          type="text"
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          disabled={busy}
          autoComplete="off"
          aria-label={t('forceDeleteModal.inputAriaLabel', { entityName })}
          className="block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary focus:border-accent disabled:opacity-50"
          data-testid="force-delete-input"
        />

        {error != null && error !== '' && (
          <p className="mt-2 text-xs text-danger" data-testid="force-delete-error">
            {error}
          </p>
        )}

        <div className="mt-6 flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            disabled={busy}
            className="rounded px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle disabled:opacity-50"
            data-testid="force-delete-cancel"
          >
            {t('forceDeleteModal.cancel')}
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={busy || !matches}
            className="rounded bg-danger px-3 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
            data-testid="force-delete-confirm"
          >
            {busy ? t('forceDeleteModal.deleting') : t('forceDeleteModal.confirm')}
          </button>
        </div>
      </div>
    </div>
  );
}

import type React from 'react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useCreateSecret } from '@/api/secrets';
import type { SecretKind } from '@/api/types';
import { useModalA11y } from './useModalA11y';

interface Props {
  open: boolean;
  onClose: () => void;
  onCreated?: (name: string) => void;
}

const KINDS: SecretKind[] = ['other', 'mcp', 'cloud_credential', 'repo_deploy_key'];

// SecretCreateModal — strict no-plaintext-echo per ADR-0026 § 5.
//   - value input is type=password + autocomplete=off
//   - on success the field is cleared BEFORE the success banner is
//     shown; the success banner contains only the secret name, never
//     the plaintext
//   - no "reveal", "show", or "edit" affordance anywhere
//   - if user wants to rotate they revoke + create new (matches
//     backend semantics: revoke is terminal, value never mutable)
export function SecretCreateModal({
  open,
  onClose,
  onCreated,
}: Props): React.ReactElement | null {
  const { t } = useTranslation('admin');
  const [name, setName] = useState('');
  const [kind, setKind] = useState<SecretKind>('other');
  const [value, setValue] = useState('');
  const [createdName, setCreatedName] = useState<string | null>(null);
  const create = useCreateSecret();
  const handleClose = () => {
    // Defensive: clear value even on cancel so memory snapshot is clean.
    setValue('');
    reset();
    onClose();
  };
  const containerRef = useModalA11y({ open, onClose: handleClose });
  if (!open) return null;

  function reset() {
    setName('');
    setKind('other');
    setValue('');
    setCreatedName(null);
  }

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || !value) return;
    try {
      const res = await create.mutateAsync({ name: name.trim(), kind, value });
      // Wipe plaintext from local state BEFORE rendering the success
      // banner so React doesn't keep it in the tree.
      setValue('');
      setCreatedName(res.name);
      onCreated?.(res.name);
    } catch {
      // Error renders below; value field stays populated so user can
      // retry without re-typing — but is still type=password so it's
      // never visible.
    }
  };

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-20 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="secret-create-title"
      data-testid="secret-create-modal"
    >
      <div className="w-full max-w-md rounded-lg bg-bg-elevated p-6 text-text-primary shadow-lg">
        <h2 id="secret-create-title" className="text-lg font-semibold">
          {t('secrets.modal.title')}
        </h2>

        {createdName === null ? (
          <form className="mt-4 space-y-3" onSubmit={submit} autoComplete="off">
            <div>
              <label htmlFor="secret-name-field" className="block text-xs font-medium text-text-primary">{t('secrets.modal.name')}</label>
              <input
                id="secret-name-field"
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t('secrets.modal.namePlaceholder')}
                autoFocus
                className="mt-1 w-full rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary placeholder:text-text-muted focus:border-accent"
                data-testid="secret-name-input"
              />
            </div>
            <div>
              <label htmlFor="secret-kind-field" className="block text-xs font-medium text-text-primary">{t('secrets.modal.kind')}</label>
              <select
                id="secret-kind-field"
                value={kind}
                onChange={(e) => setKind(e.target.value as SecretKind)}
                className="mt-1 w-full rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary focus:border-accent"
                data-testid="secret-kind-select"
              >
                {KINDS.map((k) => (
                  <option key={k} value={k}>
                    {k}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label htmlFor="secret-value-field" className="block text-xs font-medium text-text-primary">{t('secrets.modal.value')}</label>
              <input
                // type=password keeps the value invisible in the DOM render +
                // tells password managers to NOT autofill from history.
                id="secret-value-field"
                type="password"
                autoComplete="off"
                spellCheck={false}
                maxLength={4096}
                value={value}
                onChange={(e) => setValue(e.target.value)}
                placeholder={t('secrets.modal.valuePlaceholder')}
                className="mt-1 w-full rounded border border-border-base bg-bg-elevated px-2 py-1 font-mono text-sm text-text-primary placeholder:text-text-muted focus:border-accent"
                data-testid="secret-value-input"
              />
              <p className="mt-1 text-xs text-text-muted">
                {t('secrets.modal.valueHelp')}
              </p>
            </div>
            {create.isError && (
              <p className="text-xs text-danger" data-testid="secret-create-error">
                {(create.error as Error).message}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={handleClose}
                className="rounded px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
                data-testid="secret-create-cancel"
              >
                {t('secrets.modal.cancel')}
              </button>
              <button
                type="submit"
                disabled={!name.trim() || !value || create.isPending}
                className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-medium text-btn-primary-fg hover:opacity-90 disabled:bg-bg-subtle disabled:text-text-muted"
                data-testid="secret-create-submit"
              >
                {create.isPending ? t('secrets.modal.creating') : t('secrets.modal.create')}
              </button>
            </div>
          </form>
        ) : (
          <div className="mt-4 space-y-3" data-testid="secret-create-success">
            <p className="rounded bg-success/10 p-3 text-sm text-success">
              {t('secrets.success.before')}<span className="font-mono">{createdName}</span>{t('secrets.success.after')}
            </p>
            <div className="flex justify-end">
              <button
                type="button"
                onClick={handleClose}
                className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-medium text-btn-primary-fg hover:opacity-90"
                data-testid="secret-create-close"
              >
                {t('secrets.modal.done')}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

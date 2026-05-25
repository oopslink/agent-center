import type React from 'react';
import { useState } from 'react';
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
      className="fixed inset-0 z-20 flex items-center justify-center bg-slate-900/40 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="secret-create-title"
      data-testid="secret-create-modal"
    >
      <div className="w-full max-w-md rounded-lg bg-white p-6 shadow-lg">
        <h2 id="secret-create-title" className="text-lg font-semibold">
          New secret
        </h2>

        {createdName === null ? (
          <form className="mt-4 space-y-3" onSubmit={submit} autoComplete="off">
            <div>
              <label className="block text-xs font-medium text-slate-700">Name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="github-token"
                autoFocus
                className="mt-1 w-full rounded border border-slate-300 px-2 py-1 text-sm focus:border-accent"
                data-testid="secret-name-input"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-slate-700">Kind</label>
              <select
                value={kind}
                onChange={(e) => setKind(e.target.value as SecretKind)}
                className="mt-1 w-full rounded border border-slate-300 px-2 py-1 text-sm focus:border-accent"
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
              <label className="block text-xs font-medium text-slate-700">Value</label>
              <input
                // type=password keeps the value invisible in the DOM render +
                // tells password managers to NOT autofill from history.
                type="password"
                autoComplete="off"
                spellCheck={false}
                maxLength={4096}
                value={value}
                onChange={(e) => setValue(e.target.value)}
                placeholder="paste secret"
                className="mt-1 w-full rounded border border-slate-300 px-2 py-1 font-mono text-sm focus:border-accent"
                data-testid="secret-value-input"
              />
              <p className="mt-1 text-xs text-slate-500">
                Stored encrypted. Never displayed again — to rotate, revoke this
                secret and create a new one.
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
                className="rounded px-3 py-1.5 text-sm text-slate-700 hover:bg-slate-100"
                data-testid="secret-create-cancel"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={!name.trim() || !value || create.isPending}
                className="rounded bg-slate-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-slate-800 disabled:bg-slate-300"
                data-testid="secret-create-submit"
              >
                {create.isPending ? 'Creating…' : 'Create'}
              </button>
            </div>
          </form>
        ) : (
          <div className="mt-4 space-y-3" data-testid="secret-create-success">
            <p className="rounded bg-emerald-50 p-3 text-sm text-emerald-800">
              Secret <span className="font-mono">{createdName}</span> created.
            </p>
            <div className="flex justify-end">
              <button
                type="button"
                onClick={handleClose}
                className="rounded bg-slate-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-slate-800"
                data-testid="secret-create-close"
              >
                Done
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

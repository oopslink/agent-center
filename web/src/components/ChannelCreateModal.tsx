import type React from 'react';
import { useState } from 'react';
import { useCreateConversation } from '@/api/conversations';
import { useModalA11y } from './useModalA11y';

interface Props {
  open: boolean;
  onClose: () => void;
  onCreated?: (name: string) => void;
}

// ChannelCreateModal — minimal dialog (not headlessui yet; M2 keeps deps
// at the locked set). Renders nothing when closed. On submit, calls the
// F2 endpoint via useCreateConversation(kind=channel).
export function ChannelCreateModal({
  open,
  onClose,
  onCreated,
}: Props): React.ReactElement | null {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const create = useCreateConversation();
  const containerRef = useModalA11y({ open, onClose });
  if (!open) return null;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return;
    try {
      await create.mutateAsync({ kind: 'channel', name: trimmed, description });
      onCreated?.(trimmed);
      setName('');
      setDescription('');
      onClose();
    } catch {
      // error surfaces below; keep modal open so user can retry
    }
  };

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-10 flex items-center justify-center bg-slate-900/40 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="create-channel-title"
      data-testid="channel-create-modal"
    >
      <div className="w-full max-w-md rounded-lg bg-white p-6 shadow-lg">
        <h2 id="create-channel-title" className="text-lg font-semibold">
          New channel
        </h2>
        <form className="mt-4 space-y-3" onSubmit={submit}>
          <div>
            <label className="block text-xs font-medium text-slate-700">Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="alpha"
              autoFocus
              className="mt-1 w-full rounded border border-slate-300 px-2 py-1 text-sm focus:border-accent"
              data-testid="create-channel-name"
            />
          </div>
          <div>
            <label className="block text-xs font-medium text-slate-700">Description</label>
            <textarea
              rows={2}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="optional"
              className="mt-1 w-full resize-none rounded border border-slate-300 px-2 py-1 text-sm focus:border-accent"
              data-testid="create-channel-description"
            />
          </div>
          {create.isError && (
            <p className="text-xs text-danger" data-testid="create-channel-error">
              {(create.error as Error).message}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded px-3 py-1.5 text-sm text-slate-700 hover:bg-slate-100"
              data-testid="create-channel-cancel"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!name.trim() || create.isPending}
              className="rounded bg-slate-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-slate-800 disabled:bg-slate-300"
              data-testid="create-channel-submit"
            >
              {create.isPending ? 'Creating…' : 'Create'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

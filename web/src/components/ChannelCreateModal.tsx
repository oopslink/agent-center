import type React from 'react';
import { useState } from 'react';
import { useCreateConversation } from '@/api/conversations';
import { ApiError } from '@/api/client';
import { useModalA11y } from './useModalA11y';

// v2.7 #195: channel names are unique within an organization (composite
// UNIQUE(org_id,name)); a duplicate create returns 409 `already_exists`.
// Surface a friendly, org-scoped message instead of the raw envelope.
function channelCreateErrorMessage(err: unknown, name: string): string {
  if (err instanceof ApiError && err.code === 'already_exists') {
    return `A channel named "${name}" already exists in this organization.`;
  }
  return err instanceof Error ? err.message : 'Failed to create channel.';
}

interface Props {
  open: boolean;
  onClose: () => void;
  // v2.7.1 #247: yields the new channel's id (conversation_id) so callers can
  // navigate to the id-based URL (/channels/{id}); was the name pre-#247.
  onCreated?: (id: string) => void;
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
      const res = await create.mutateAsync({ kind: 'channel', name: trimmed, description });
      onCreated?.(res.conversation_id);
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
      className="fixed inset-0 z-10 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="create-channel-title"
      data-testid="channel-create-modal"
    >
      <div className="w-full max-w-md rounded-lg bg-bg-elevated p-6 text-text-primary shadow-lg">
        <h2 id="create-channel-title" className="text-lg font-semibold">
          New channel
        </h2>
        <form className="mt-4 space-y-3" onSubmit={submit}>
          <div>
            <label htmlFor="create-channel-name-input" className="block text-xs font-medium text-text-primary">Name</label>
            <input
              id="create-channel-name-input"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="alpha"
              autoFocus
              className="mt-1 w-full rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary placeholder:text-text-muted focus:border-accent"
              data-testid="create-channel-name"
            />
          </div>
          <div>
            <label htmlFor="create-channel-desc-input" className="block text-xs font-medium text-text-primary">Description</label>
            <textarea
              id="create-channel-desc-input"
              rows={2}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="optional"
              className="mt-1 w-full resize-none rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary placeholder:text-text-muted focus:border-accent"
              data-testid="create-channel-description"
            />
          </div>
          {create.isError && (
            <p className="text-xs text-danger" data-testid="create-channel-error">
              {channelCreateErrorMessage(create.error, name.trim())}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
              data-testid="create-channel-cancel"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!name.trim() || create.isPending}
              className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-medium text-btn-primary-fg hover:opacity-90 disabled:bg-bg-subtle disabled:text-text-muted"
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

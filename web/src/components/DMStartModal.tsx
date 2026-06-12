import type React from 'react';
import { useMemo, useState } from 'react';
import { useCreateConversation } from '@/api/conversations';
import { useMembers, normalizeIdentityRef, identityRefOf } from '@/api/members';
import { useAppStore } from '@/store/app';
import { useModalA11y } from './useModalA11y';

interface Props {
  open: boolean;
  onClose: () => void;
  onCreated?: (conversationId: string) => void;
}

// DMStartModal (v2.7.1 #215) — DMs are strict 1:1, so this is a single-select
// peer picker (search org members, agents + humans, excluding self). Group
// conversations are channels, so there's no multi-select here. Creating a DM
// is idempotent server-side: starting a DM with an existing peer returns the
// existing conversation, so navigation lands on the same DM (no duplicates).
export function DMStartModal({
  open,
  onClose,
  onCreated,
}: Props): React.ReactElement | null {
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState<string>('');
  const create = useCreateConversation();
  const members = useMembers();
  const me = useAppStore((s) => s.currentUserId);
  const containerRef = useModalA11y({ open, onClose });

  const meBare = me ? normalizeIdentityRef(me) : '';

  const candidates = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (members.data ?? [])
      .filter((m) => m.status === 'joined' && normalizeIdentityRef(m.identity_id) !== meBare)
      .filter((m) => {
        if (!q) return true;
        const name = (m.display_name ?? '').toLowerCase();
        return name.includes(q) || m.identity_id.toLowerCase().includes(q);
      });
  }, [members.data, meBare, query]);

  if (!open) return null;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selected) return;
    try {
      const res = await create.mutateAsync({ kind: 'dm', members: [selected] });
      onCreated?.(res.conversation_id);
      setQuery('');
      setSelected('');
      onClose();
    } catch {
      // error renders below; keep modal open for retry
    }
  };

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-10 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="start-dm-title"
      data-testid="dm-start-modal"
    >
      <div className="w-full max-w-md rounded-lg bg-bg-elevated p-6 text-text-primary shadow-lg">
        <h2 id="start-dm-title" className="text-lg font-semibold">
          Start a DM
        </h2>
        <p className="mt-1 text-xs text-text-muted">
          Pick one person or agent. Use channels for group conversations.
        </p>
        <form className="mt-4 space-y-3" onSubmit={submit}>
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search people or agents…"
            autoFocus
            className="w-full rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary placeholder:text-text-muted focus:border-accent"
            data-testid="dm-peer-search"
          />
          <ul className="max-h-64 space-y-1 overflow-y-auto" data-testid="dm-peer-candidates">
            {candidates.length === 0 && (
              <li className="px-1 py-2 text-xs italic text-text-muted" data-testid="dm-peer-empty">
                No matching people or agents.
              </li>
            )}
            {candidates.map((m) => {
              const ref = identityRefOf(m);
              const active = selected === ref;
              return (
                <li key={ref}>
                  <button
                    type="button"
                    onClick={() => setSelected(ref)}
                    data-testid="dm-peer-candidate"
                    data-ref={ref}
                    aria-pressed={active}
                    className={`flex w-full items-center justify-between gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-bg-subtle ${
                      active ? 'bg-bg-subtle font-medium text-brand' : 'text-text-primary'
                    }`}
                  >
                    <span className="truncate">{m.display_name || m.identity_id}</span>
                    <span className="shrink-0 rounded bg-bg-subtle px-1.5 text-[0.625rem] uppercase text-text-muted">
                      {m.kind === 'agent' ? 'Agent' : 'Human'}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
          {create.isError && (
            <p className="text-xs text-danger" data-testid="dm-start-error">
              {(create.error as Error).message}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
              data-testid="dm-start-cancel"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!selected || create.isPending}
              className="rounded bg-text-primary px-3 py-1.5 text-sm font-medium text-bg-elevated hover:opacity-90 disabled:bg-bg-subtle disabled:text-text-muted"
              data-testid="dm-start-submit"
            >
              {create.isPending ? 'Starting…' : 'Start DM'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

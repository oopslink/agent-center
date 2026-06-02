import type React from 'react';
import { useMemo, useState } from 'react';
import { useInviteParticipant } from '@/api/conversations';
import { useMembers, normalizeIdentityRef, type MemberResult } from '@/api/members';
import type { Participant } from '@/api/types';

interface Props {
  conversationId: string;
  participants: Participant[]; // current (active + left) — all excluded from candidates
  onClose: () => void;
}

// v2.7 #167: search + multi-select invite. Replaces the raw-ref text input —
// the channel owner searches org members by name, sees Human/Agent tags, picks
// several, and confirms a batch invite. Candidates exclude anyone already in the
// conversation (active OR previously removed — §-1: a deliberately removed
// participant is not re-surfaced here). Org member lists are small, so search is
// a client-side filter over /api/members (which carries display_name + kind).
export function MemberInviteModal({ conversationId, participants, onClose }: Props): React.ReactElement {
  const members = useMembers();
  const invite = useInviteParticipant();
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [errorMsg, setErrorMsg] = useState('');

  // identity refs already in the conversation (active OR left), normalized to the
  // bare id so a prefixed participant ref ("user:user-x") matches a bare member
  // identity_id ("user-x").
  const existing = useMemo(
    () => new Set(participants.map((p) => normalizeIdentityRef(p.identity_id))),
    [participants],
  );

  const candidates = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (members.data ?? [])
      .filter((m) => m.status === 'joined' && !existing.has(normalizeIdentityRef(m.identity_id)))
      .filter((m) => {
        if (!q) return true;
        const name = (m.display_name ?? '').toLowerCase();
        return name.includes(q) || m.identity_id.toLowerCase().includes(q);
      });
  }, [members.data, existing, query]);

  // Build the invite ref ("<kind>:<id>") the backend expects from a bare member.
  const refOf = (m: MemberResult) => (m.kind === 'agent' ? 'agent:' : 'user:') + m.identity_id;

  const toggle = (ref: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(ref)) next.delete(ref);
      else next.add(ref);
      return next;
    });

  const confirm = async () => {
    if (selected.size === 0) return;
    setErrorMsg('');
    try {
      // Batch invite — one mutation per selected member (org lists are small).
      for (const ref of selected) {
        await invite.mutateAsync({ conversationId, identityId: ref, role: 'member' });
      }
      onClose();
    } catch (e) {
      setErrorMsg((e as Error).message);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
      role="dialog"
      aria-modal="true"
      aria-label="Invite members"
      data-testid="member-invite-modal"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-sm rounded-lg border border-border bg-bg-elevated p-4 shadow-[var(--shadow-3)]">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-text-primary">Invite members</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label="Close"
            data-testid="invite-modal-close"
          >
            X
          </button>
        </div>
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search members…"
          autoFocus
          className="mb-3 w-full rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary placeholder:text-text-muted focus:border-accent"
          data-testid="invite-search"
        />
        <ul className="max-h-64 space-y-1 overflow-y-auto" data-testid="invite-candidates">
          {candidates.length === 0 && (
            <li className="px-1 py-2 text-xs italic text-text-muted" data-testid="invite-no-candidates">
              No matching members.
            </li>
          )}
          {candidates.map((m) => {
            const ref = refOf(m);
            return (
              <li key={ref}>
                <label className="flex cursor-pointer items-center gap-2 rounded px-1 py-1 text-sm hover:bg-bg-subtle" data-testid="invite-candidate">
                  <input
                    type="checkbox"
                    checked={selected.has(ref)}
                    onChange={() => toggle(ref)}
                    data-testid="invite-candidate-check"
                    data-ref={ref}
                  />
                  <span className="flex-1 truncate text-text-primary">{m.display_name || m.identity_id}</span>
                  <span
                    className="rounded bg-bg-subtle px-1.5 text-[0.625rem] uppercase text-text-muted"
                    data-testid="invite-candidate-kind"
                  >
                    {m.kind === 'agent' ? 'Agent' : 'Human'}
                  </span>
                </label>
              </li>
            );
          })}
        </ul>
        {errorMsg && (
          <p className="mt-2 text-xs text-danger" data-testid="invite-modal-error">
            {errorMsg}
          </p>
        )}
        <div className="mt-3 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded px-3 py-1 text-xs text-text-secondary hover:bg-bg-subtle"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={confirm}
            disabled={selected.size === 0 || invite.isPending}
            className="rounded bg-text-primary px-3 py-1 text-xs font-medium text-bg-elevated hover:opacity-90 disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="invite-confirm"
          >
            {invite.isPending ? 'Inviting…' : `Invite${selected.size > 0 ? ` (${selected.size})` : ''}`}
          </button>
        </div>
      </div>
    </div>
  );
}

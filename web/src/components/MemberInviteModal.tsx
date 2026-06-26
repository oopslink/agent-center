import type React from 'react';
import { useMemo, useState } from 'react';
import { useInviteParticipant } from '@/api/conversations';
import { useMembers, normalizeIdentityRef, identityRefOf } from '@/api/members';
import type { Participant } from '@/api/types';
import { useModalA11y } from './useModalA11y';

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
  // a11y: Escape closes + focus-trap (rendered = open).
  const containerRef = useModalA11y({ open: true, onClose });

  // identity refs already in the conversation (active OR left), normalized to the
  // bare id so a prefixed participant ref ("user:user-x") matches a bare member
  // identity_id ("user-x").
  // Only CURRENTLY-active participants are excluded. A member kicked from this
  // channel (left_at set) is re-invitable, so they stay a candidate (§-1: this
  // is the channel-kick case, distinct from org-level removal which the
  // status==='joined' filter handles).
  const activeIds = useMemo(
    () =>
      new Set(
        participants
          .filter((p) => !p.left_at)
          .map((p) => normalizeIdentityRef(p.identity_id)),
      ),
    [participants],
  );

  const candidates = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (members.data ?? [])
      .filter((m) => m.status === 'joined' && !activeIds.has(normalizeIdentityRef(m.identity_id)))
      .filter((m) => {
        if (!q) return true;
        const name = (m.display_name ?? '').toLowerCase();
        return name.includes(q) || m.identity_id.toLowerCase().includes(q);
      });
  }, [members.data, activeIds, query]);

  const toggle = (ref: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(ref)) next.delete(ref);
      else next.add(ref);
      return next;
    });

  // Select-all over the currently-visible (search-filtered) candidates.
  const visibleRefs = useMemo(() => candidates.map(identityRefOf), [candidates]);
  const allVisibleSelected =
    visibleRefs.length > 0 && visibleRefs.every((r) => selected.has(r));
  const toggleAll = () =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (allVisibleSelected) visibleRefs.forEach((r) => next.delete(r));
      else visibleRefs.forEach((r) => next.add(r));
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
      ref={containerRef}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      role="dialog"
      aria-modal="true"
      aria-labelledby="member-invite-title"
      data-testid="member-invite-modal"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-sm rounded-lg border border-border bg-bg-elevated p-4 shadow-[var(--shadow-3)]">
        <div className="mb-3 flex items-center justify-between">
          <h2 id="member-invite-title" className="text-sm font-semibold text-text-primary">Invite members</h2>
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
          aria-label="Search members"
          autoFocus
          className="mb-3 w-full rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary placeholder:text-text-muted focus:border-accent"
          data-testid="invite-search"
        />
        <div className="mb-2 flex items-center justify-between px-1 text-xs text-text-muted">
          <span data-testid="invite-selected-count">{selected.size} selected</span>
          {visibleRefs.length > 0 && (
            <button
              type="button"
              onClick={toggleAll}
              className="font-medium text-accent hover:underline"
              data-testid="invite-select-all"
            >
              {allVisibleSelected ? 'Clear' : 'Select all'}
            </button>
          )}
        </div>
        <ul className="max-h-64 space-y-1 overflow-y-auto" data-testid="invite-candidates">
          {candidates.length === 0 && (
            <li className="px-1 py-2 text-xs italic text-text-muted" data-testid="invite-no-candidates">
              No matching members.
            </li>
          )}
          {candidates.map((m) => {
            const ref = identityRefOf(m);
            return (
              <li key={ref}>
                <button
                  type="button"
                  onClick={() => toggle(ref)}
                  aria-pressed={selected.has(ref)}
                  className={`flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm ${
                    selected.has(ref)
                      ? 'bg-bg-subtle text-text-primary ring-1 ring-inset ring-accent'
                      : 'text-text-secondary hover:bg-bg-subtle'
                  }`}
                  data-testid="invite-candidate"
                  data-ref={ref}
                  data-selected={selected.has(ref) ? 'true' : 'false'}
                >
                  <span className="flex-1 truncate">{m.display_name || m.identity_id}</span>
                  <span
                    className="rounded bg-bg-subtle px-1.5 text-[0.625rem] uppercase text-text-muted"
                    data-testid="invite-candidate-kind"
                  >
                    {m.kind === 'agent' ? 'Agent' : 'Human'}
                  </span>
                  <span className="flex h-4 w-4 shrink-0 items-center justify-center text-accent" aria-hidden="true">
                    {selected.has(ref) && <CheckIcon />}
                  </span>
                </button>
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
            className="rounded bg-btn-primary-bg px-3 py-1 text-xs font-medium text-btn-primary-fg hover:opacity-90 disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="invite-confirm"
          >
            {invite.isPending ? 'Inviting…' : `Invite${selected.size > 0 ? ` (${selected.size})` : ''}`}
          </button>
        </div>
      </div>
    </div>
  );
}

// inline check (no-emoji UX rule — single-stroke SVG), shown on selected rows.
function CheckIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="2" aria-hidden="true">
      <path d="M5 10.5l3.5 3.5L15 6.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

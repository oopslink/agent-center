import type React from 'react';
import { useEffect, useState } from 'react';
import { useRemoveParticipant } from '@/api/conversations';
import { useDisplayNameResolver } from '@/api/members';
import { useAppStore } from '@/store/app';
import type { Participant } from '@/api/types';
import { MemberInviteModal } from './MemberInviteModal';

interface Props {
  conversationId: string;
  participants: Participant[];
}

const COLLAPSE_KEY = 'ac.participants.collapsed';

function readCollapsed(): boolean {
  try {
    if (typeof localStorage === 'undefined' || typeof localStorage.getItem !== 'function') return false;
    return localStorage.getItem(COLLAPSE_KEY) === '1';
  } catch {
    return false;
  }
}

// ParticipantsPanel — list participants + role + invite/remove controls.
// Only the channel owner (or supervisor) gets the action buttons; non-
// owners see a read-only list. v2.7 #167: invite is a search+multi-select
// modal (MemberInviteModal), and the panel can collapse/expand.
export function ParticipantsPanel({
  conversationId,
  participants,
}: Props): React.ReactElement {
  const me = useAppStore((s) => s.currentUserId);
  const displayName = useDisplayNameResolver();
  const isOwner = participants.some(
    (p) => p.identity_id === me && p.role === 'owner' && !p.left_at,
  );
  const active = participants.filter((p) => !p.left_at);
  const remove = useRemoveParticipant();
  const [inviteOpen, setInviteOpen] = useState(false);
  const [collapsed, setCollapsed] = useState<boolean>(readCollapsed);

  useEffect(() => {
    try {
      if (typeof localStorage !== 'undefined' && typeof localStorage.setItem === 'function') {
        localStorage.setItem(COLLAPSE_KEY, collapsed ? '1' : '0');
      }
    } catch {
      // ignore
    }
  }, [collapsed]);

  if (collapsed) {
    return (
      <aside
        className="flex w-10 flex-shrink-0 flex-col items-center border-l border-border-base bg-bg-subtle py-3"
        aria-label="participants"
        data-testid="participants-panel"
        data-collapsed="true"
      >
        <button
          type="button"
          onClick={() => setCollapsed(false)}
          aria-label="Expand participants"
          title={`Participants (${active.length})`}
          data-testid="participants-expand"
          className="rounded p-1 text-text-secondary hover:bg-bg-base hover:text-text-primary"
        >
          ‹ {active.length}
        </button>
      </aside>
    );
  }

  return (
    <aside
      className="w-64 flex-shrink-0 border-l border-border-base bg-bg-subtle p-4"
      aria-label="participants"
      data-testid="participants-panel"
      data-collapsed="false"
    >
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-sm font-semibold text-text-primary">
          Participants ({active.length})
        </h3>
        <button
          type="button"
          onClick={() => setCollapsed(true)}
          aria-label="Collapse participants"
          data-testid="participants-collapse"
          className="rounded p-1 text-xs text-text-muted hover:bg-bg-base hover:text-text-primary"
        >
          ›
        </button>
      </div>
      <ul className="space-y-2">
        {active.map((p) => (
          <li
            key={p.identity_id}
            className="flex items-center justify-between text-sm"
            data-testid="participant-row"
            data-identity={p.identity_id}
          >
            <span>
              <span className="text-xs" title={p.identity_id}>{displayName(p.identity_id)}</span>
              <span className="ml-2 text-xs uppercase text-text-muted">{p.role}</span>
            </span>
            {isOwner && p.role !== 'owner' && (
              <button
                type="button"
                className="text-xs text-danger hover:underline"
                onClick={() => remove.mutate({ conversationId, identityId: p.identity_id })}
                data-testid="participant-remove"
              >
                remove
              </button>
            )}
          </li>
        ))}
      </ul>

      {isOwner && (
        <>
          <button
            type="button"
            onClick={() => setInviteOpen(true)}
            className="mt-4 w-full rounded bg-text-primary px-2 py-1 text-xs font-medium text-bg-elevated hover:opacity-90"
            data-testid="invite-open"
          >
            Invite
          </button>
          {inviteOpen && (
            <MemberInviteModal
              conversationId={conversationId}
              participants={participants}
              onClose={() => setInviteOpen(false)}
            />
          )}
        </>
      )}
    </aside>
  );
}

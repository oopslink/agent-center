import type React from 'react';
import { useState } from 'react';
import {
  useInviteParticipant,
  useRemoveParticipant,
} from '@/api/conversations';
import { useDisplayNameResolver } from '@/api/members';
import { useAppStore } from '@/store/app';
import type { Participant } from '@/api/types';

interface Props {
  conversationId: string;
  participants: Participant[];
}

// ParticipantsPanel — list participants + role + invite/remove controls.
// Only the channel owner (or supervisor) gets the action buttons; non-
// owners see a read-only list.
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
  const [inviteId, setInviteId] = useState('');
  const invite = useInviteParticipant();
  const remove = useRemoveParticipant();

  const handleInvite = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!inviteId.trim()) return;
    try {
      await invite.mutateAsync({
        conversationId,
        identityId: inviteId.trim(),
        role: 'member',
      });
      setInviteId('');
    } catch {
      // Error surfaces below.
    }
  };

  return (
    <aside
      className="w-64 flex-shrink-0 border-l border-border-base bg-bg-subtle p-4"
      aria-label="participants"
      data-testid="participants-panel"
    >
      <h3 className="mb-3 text-sm font-semibold text-text-primary">
        Participants ({active.length})
      </h3>
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
        <form className="mt-4 space-y-2" onSubmit={handleInvite}>
          <label className="block text-xs text-text-secondary">Invite identity</label>
          <input
            type="text"
            value={inviteId}
            onChange={(e) => setInviteId(e.target.value)}
            placeholder="agent:bot-1 or user:alice"
            className="w-full rounded border border-border-base bg-bg-elevated px-2 py-1 text-xs text-text-primary placeholder:text-text-muted focus:border-accent"
            data-testid="invite-input"
          />
          <button
            type="submit"
            disabled={!inviteId.trim() || invite.isPending}
            className="w-full rounded bg-text-primary px-2 py-1 text-xs font-medium text-bg-elevated hover:opacity-90 disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="invite-submit"
          >
            {invite.isPending ? 'Inviting…' : 'Invite'}
          </button>
          {invite.isError && (
            <span className="block text-xs text-danger" data-testid="invite-error">
              {(invite.error as Error).message}
            </span>
          )}
        </form>
      )}
    </aside>
  );
}

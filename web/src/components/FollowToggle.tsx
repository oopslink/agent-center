import type React from 'react';
import { useFollowConversation } from '@/api/conversations';

// v2.8 #264 P1 / #176 §4: a conversation-header follow/unfollow control.
// Toggle button (aria-pressed reflects the current follow-state); click flips
// it via POST/DELETE /:id/follow. Following a thread resumes unread/badge
// delivery; unfollowing suppresses it (mirrors `slock thread unfollow`).
// Icon + text label (no emoji), disabled while the mutation is in flight.
export function FollowToggle({
  conversationId,
  followed,
}: {
  conversationId: string;
  followed: boolean;
}): React.ReactElement {
  const follow = useFollowConversation();
  const label = followed ? 'Following' : 'Follow';
  const aria = followed ? 'Unfollow this conversation' : 'Follow this conversation';
  return (
    <button
      type="button"
      data-testid="follow-toggle"
      aria-pressed={followed}
      aria-label={aria}
      title={aria}
      disabled={follow.isPending}
      onClick={() => follow.mutate({ conversationId, follow: !followed })}
      className={[
        'inline-flex items-center gap-1 rounded border px-2 py-1 text-xs font-medium motion-safe:transition-colors disabled:opacity-60',
        followed
          ? 'border-brand bg-brand/10 text-brand'
          : 'border-border-base text-text-secondary hover:bg-bg-subtle',
      ].join(' ')}
    >
      <span aria-hidden="true" className="inline-flex h-3.5 w-3.5">
        <FollowIcon filled={followed} />
      </span>
      {label}
    </button>
  );
}

// Bell outline (not-following) / filled bell (following) — single-stroke SVG,
// no emoji (a11y icon rule §1.2).
function FollowIcon({ filled }: { filled: boolean }): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill={filled ? 'currentColor' : 'none'} stroke="currentColor" strokeWidth="1.5" className="h-full w-full">
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M10 3a4 4 0 0 0-4 4v2.5L4.5 13h11L14 9.5V7a4 4 0 0 0-4-4Zm-1.5 12a1.5 1.5 0 0 0 3 0"
      />
    </svg>
  );
}

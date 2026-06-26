import type React from 'react';

interface Props {
  /** Direct reply count on this message's thread; 0/undefined → no count chip. */
  replyCount?: number;
  /** Whether the thread has recent activity → render the dot indicator. */
  hasActivity?: boolean;
  onClick: () => void;
}

// ThreadButton — the per-message thread affordance shown beside each top-level
// message. Always clickable (open the thread to reply, even when empty); shows a
// reply-count chip when there are replies and a small activity dot when the
// thread has recent activity.
//
// AA discipline (day-0): solid theme tokens only — NO alpha-tint
// (bg-{token}/{opacity}). The count chip reuses the ParticipantsPanel chip
// treatment (bg-bg-subtle + text-text-secondary, AA in both modes); the activity
// dot is a solid bg-accent disc (non-text ≥3:1 against the elevated surface).
export function ThreadButton({ replyCount = 0, hasActivity = false, onClick }: Props): React.ReactElement {
  const hasReplies = replyCount > 0;
  const label = hasReplies
    ? `Open thread — ${replyCount} ${replyCount === 1 ? 'reply' : 'replies'}`
    : 'Reply in thread';
  return (
    <button
      type="button"
      onClick={onClick}
      data-testid="thread-button"
      aria-label={label}
      title={label}
      className="inline-flex items-center gap-1 rounded-lg md:rounded px-3 py-1.5 md:px-1.5 md:py-0.5 text-xs font-medium text-text-secondary bg-bg-subtle/50 md:bg-transparent hover:bg-bg-subtle hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent"
    >
      {/* speech-bubble glyph (no emoji-icon per the a11y guardrail). */}
      <svg
        viewBox="0 0 20 20"
        fill="none"
        className="h-3.5 w-3.5 stroke-current"
        strokeWidth="1.5"
        aria-hidden="true"
      >
        <path
          d="M4 4h12a1 1 0 0 1 1 1v8a1 1 0 0 1-1 1H8l-4 3v-3H4a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1z"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
      {hasReplies && (
        <span
          className="inline-flex min-w-[1.25rem] items-center justify-center rounded-full bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-semibold text-text-secondary"
          data-testid="thread-reply-count"
        >
          {replyCount}
        </span>
      )}
      {hasActivity && (
        <span
          className="h-2 w-2 shrink-0 rounded-full bg-accent"
          data-testid="thread-activity-dot"
          aria-hidden="true"
        />
      )}
    </button>
  );
}

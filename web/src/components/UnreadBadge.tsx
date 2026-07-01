import type React from 'react';
import { useTranslation } from 'react-i18next';

// v2.8 #264 P1 / #176 unread-badge contract. PROP-DRIVEN from the Conversation
// row's embedded counts (GET /conversations / GET /:id carry unread_count +
// mention_count per requesting human, #176 §2) — no more N standalone /unread
// fetches. Render rules (#176 §5, a11y §A "not color-only"):
//   - 0 unread + 0 mention → null (clean row).
//   - mention > 0 → red badge with the PRECISE mention number (99+ cap); the
//     directed @-mention is the high-signal state, so it shows a number.
//   - unread > 0, no mention → neutral dot (the row also goes bold elsewhere,
//     so the state is not conveyed by color alone).
//   - SR aria-label always spells out "(N unread, M mention(s))" so the count
//     is not color/shape-only for assistive tech.
const MaxMentionDisplay = 99;

export function UnreadBadge({
  unreadCount = 0,
  mentionCount = 0,
}: {
  unreadCount?: number;
  mentionCount?: number;
}): React.ReactElement | null {
  const { t } = useTranslation('common');
  const unread = Math.max(0, unreadCount);
  const mention = Math.max(0, mentionCount);

  if (mention > 0) {
    const label = mention > MaxMentionDisplay ? `${MaxMentionDisplay}+` : String(mention);
    return (
      <span
        className="inline-flex min-w-[1.25rem] items-center justify-center rounded-full bg-danger px-1.5 text-xs font-semibold text-white tabular-nums"
        data-testid="conversation-mention-badge"
        data-mention-count={mention}
        aria-label={t('unreadBadge.unreadMention', { unread, count: mention })}
      >
        {label}
      </span>
    );
  }

  if (unread > 0) {
    return (
      <span
        className="inline-flex items-center"
        data-testid="conversation-unread-dot"
        data-unread-count={unread}
        aria-label={t('unreadBadge.unread', { unread })}
      >
        <span aria-hidden="true" className="h-2 w-2 rounded-full bg-brand" />
      </span>
    );
  }

  return null;
}

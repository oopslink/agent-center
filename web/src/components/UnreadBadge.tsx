import type React from 'react';
import { useUnread } from '@/api/readState';

// MaxUnreadDisplay matches the backend MaxUnreadCount cap. When the
// service returns 999 we render "999+" to signal overflow.
const MaxUnreadDisplay = 999;

// UnreadBadge renders a small pill with the per-conversation unread
// count. Returns null when count is zero (or while the request is
// loading / errored) so the row stays clean.
export function UnreadBadge({
  conversationId,
}: {
  conversationId: string;
}): React.ReactElement | null {
  const { data } = useUnread(conversationId);
  if (!data || data.unread_count <= 0) {
    return null;
  }
  const label = data.unread_count >= MaxUnreadDisplay ? `${MaxUnreadDisplay}+` : String(data.unread_count);
  return (
    <span
      className="inline-flex min-w-[1.25rem] items-center justify-center rounded-full bg-blue-600 px-1.5 text-xs font-medium text-white"
      data-testid="unread-badge"
      data-unread-count={data.unread_count}
    >
      {label}
    </span>
  );
}

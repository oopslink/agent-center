import { useEffect } from 'react';
import { api } from '@/api/client';
import { useAppStore } from '@/store/app';

// useSSEConversationSubscribe registers + tears down per-conversation
// SSE subscriptions for the current user.
//
// Why this exists: the backend Bus.Publish filters events with a
// non-empty conversation_id against the per-user subscription set.
// Without explicit /api/sse/subscribe calls, the set is empty and
// every conv-scoped event (conversation.message_added,
// conversation.read_state.changed, conversation.archived/closed,
// conversation.message_references_added) gets dropped before reaching
// the EventSource. Cold-open + mutation invalidations still work via
// react-query, but background tabs never see updates.
//
// Usage:
//   - List pages (Channels / DMs): pass every visible conv id so
//     badges auto-tick on new messages.
//   - Detail pages (ChannelDetail / DMDetail): pass the single
//     focused conv id so live messages flow in.
//
// Behavior:
//   - Fires POST /api/sse/subscribe per id on mount + when the id
//     list changes (effect re-fires).
//   - Fires POST /api/sse/unsubscribe per id on unmount + before
//     re-running with a new id set.
//   - Failures are swallowed (fire-and-forget): the user already has
//     stale-fallback via react-query, so a flaky subscribe doesn't
//     break the page.
export function useSSEConversationSubscribe(conversationIds: string[] | undefined): void {
  const userId = useAppStore((s) => s.currentUserId);
  const idsKey = conversationIds?.join(',') ?? '';

  useEffect(() => {
    if (!conversationIds || conversationIds.length === 0) return;
    const ids = [...conversationIds];
    ids.forEach((convId) => {
      void api
        .post<{ subscribed: boolean }>('/sse/subscribe', {
          user_id: userId,
          conversation_id: convId,
        })
        .catch(() => {
          // ignore — see hook docstring
        });
    });
    return () => {
      ids.forEach((convId) => {
        void api
          .post<{ unsubscribed: boolean }>('/sse/unsubscribe', {
            user_id: userId,
            conversation_id: convId,
          })
          .catch(() => {
            // ignore
          });
      });
    };
    // idsKey is the stable dep — `conversationIds` may be a fresh
    // array on each render but if the ids haven't changed we don't
    // want to re-fire the subscribe cycle.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [userId, idsKey]);
}

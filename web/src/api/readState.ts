import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

// UnreadSummary mirrors GET /api/conversations/{id}/unread.
export interface UnreadSummary {
  conversation_id: string;
  user_id: string;
  last_seen_message_id: string;
  unread_count: number;
}

// MarkSeenResult mirrors POST /api/conversations/{id}/seen.
//
// `bumped: false` is a normal no-op response — the server's
// only-forward rule means a stale cursor doesn't regress. UI should
// not treat it as an error.
export interface MarkSeenResult {
  last_seen_message_id: string;
  version: number;
  bumped: boolean;
  event_id: string;
  // v2.8 #268 (#176 §3): the /seen response now also returns the RECOMPUTED
  // counts (precise N−K partial-read, not a forced 0), so a caller can reflect
  // the new badge state without a refetch. Optional — older payloads omit them.
  unread_count?: number;
  mention_count?: number;
}

export function useUnread(conversationId: string | undefined) {
  return useQuery({
    queryKey: qk.unread(conversationId ?? ''),
    queryFn: () => api.get<UnreadSummary>(`/conversations/${conversationId}/unread`),
    enabled: !!conversationId,
    staleTime: 5_000,
  });
}

export function useMarkSeen() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      conversationId,
      lastSeenMessageId,
    }: {
      conversationId: string;
      lastSeenMessageId: string;
    }) =>
      api.post<MarkSeenResult>(`/conversations/${conversationId}/seen`, {
        last_seen_message_id: lastSeenMessageId,
      }),
    onSuccess: (_, vars) => {
      void qc.invalidateQueries({ queryKey: qk.unread(vars.conversationId) });
      // v2.8 #264 P1 / #176 §3: counts are now embedded per-row in the
      // conversation list + detail, so refresh both → the sidebar/list badge
      // clears once the read cursor advances (the user views a conversation).
      void qc.invalidateQueries({ queryKey: qk.conversations() });
      void qc.invalidateQueries({ queryKey: qk.conversation(vars.conversationId) });
    },
  });
}

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
    },
  });
}

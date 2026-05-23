import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type {
  Conversation,
  ConversationKind,
  ConversationMessageReference,
  ConversationStatus,
  CreateConversationInput,
  CreateConversationResult,
  Message,
  SendMessageInput,
  SendMessageResult,
} from './types';

export function useConversations(filter?: { kind?: ConversationKind; status?: ConversationStatus }) {
  const search = new URLSearchParams();
  if (filter?.kind) search.set('kind', filter.kind);
  if (filter?.status) search.set('status', filter.status);
  const qs = search.toString();
  return useQuery({
    queryKey: qk.conversations(filter?.kind),
    queryFn: () => api.get<Conversation[]>(`/conversations${qs ? `?${qs}` : ''}`),
  });
}

export function useConversation(id: string | undefined) {
  return useQuery({
    queryKey: qk.conversation(id ?? ''),
    queryFn: () => api.get<Conversation>(`/conversations/${id}`),
    enabled: !!id,
  });
}

export function useMessages(conversationId: string | undefined) {
  return useQuery({
    queryKey: qk.messages(conversationId ?? ''),
    queryFn: () => api.get<Message[]>(`/conversations/${conversationId}/messages`),
    enabled: !!conversationId,
  });
}

export function useConversationRefs(conversationId: string | undefined) {
  return useQuery({
    queryKey: qk.refs(conversationId ?? ''),
    queryFn: () =>
      api.get<ConversationMessageReference[]>(`/conversations/${conversationId}/refs`),
    enabled: !!conversationId,
  });
}

export function useCreateConversation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateConversationInput) =>
      api.post<CreateConversationResult>('/conversations', input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.conversations() });
    },
  });
}

export function useSendMessage() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ conversationId, ...body }: SendMessageInput) =>
      api.post<SendMessageResult>(`/conversations/${conversationId}/messages`, body),
    onSuccess: (_, vars) => {
      void qc.invalidateQueries({ queryKey: qk.messages(vars.conversationId) });
    },
  });
}

export function useArchiveConversation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, version, archivedBy }: { id: string; version: number; archivedBy?: string }) =>
      api.post<{ event_id: string }>(`/conversations/${id}/archive`, {
        version,
        archived_by: archivedBy,
      }),
    onSuccess: (_, vars) => {
      void qc.invalidateQueries({ queryKey: qk.conversation(vars.id) });
      void qc.invalidateQueries({ queryKey: qk.conversations() });
    },
  });
}

export function useInviteParticipant() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ conversationId, identityId, role }: { conversationId: string; identityId: string; role?: string }) =>
      api.post<{ event_id: string }>(`/conversations/${conversationId}/participants`, {
        identity_id: identityId,
        role: role ?? 'member',
      }),
    onSuccess: (_, vars) => {
      void qc.invalidateQueries({ queryKey: qk.conversation(vars.conversationId) });
    },
  });
}

export function useRemoveParticipant() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ conversationId, identityId }: { conversationId: string; identityId: string }) =>
      api.del<{ event_id: string }>(
        `/conversations/${conversationId}/participants/${encodeURIComponent(identityId)}`,
      ),
    onSuccess: (_, vars) => {
      void qc.invalidateQueries({ queryKey: qk.conversation(vars.conversationId) });
    },
  });
}

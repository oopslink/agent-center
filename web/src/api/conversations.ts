import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, withOrgSlug } from './client';
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

interface CreateUploadResult {
  file_uri: string;
  transfer_uri: string;
  transfer_id: string;
}

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

// useConversationByOwnerRef fetches the single task/issue conversation pinned
// to an owner_ref (pm://tasks|issues/{id}). The list endpoint is org-scoped by
// construction, so a cross-org owner_ref returns no rows (fail-closed, no
// leak). Returns the matching conversation or null. v2.7 #137.
export function useConversationByOwnerRef(ownerRef: string | undefined) {
  return useQuery({
    queryKey: qk.conversationByOwner(ownerRef ?? ''),
    queryFn: async () => {
      const list = await api.get<Conversation[]>(
        `/conversations?owner_ref=${encodeURIComponent(ownerRef as string)}`,
      );
      return list[0] ?? null;
    },
    enabled: !!ownerRef,
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

export async function uploadMessageAttachment(file: File) {
  const contentType = file.type || 'application/octet-stream';
  const created = await api.post<CreateUploadResult>('/files', {
    content_type: contentType,
    size: file.size,
  });
  const putPath = `/api${withOrgSlug(`/files/transfer/${encodeURIComponent(created.transfer_id)}`)}`;
  const putResp = await fetch(putPath, {
    method: 'PUT',
    headers: { 'Content-Type': contentType },
    body: file,
  });
  if (!putResp.ok) {
    throw new Error(`upload failed: ${putResp.status}`);
  }
  await api.post(`/files/transfer/${encodeURIComponent(created.transfer_id)}/complete`, {
    size: file.size,
  });
  return {
    uri: created.file_uri,
    filename: file.name,
    mime_type: contentType,
    size: file.size,
  };
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

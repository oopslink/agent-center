import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ApiError, api, withOrgSlug } from './client';
import { qk } from './queryKeys';
import type {
  Conversation,
  ConversationKind,
  ConversationMessageReference,
  ConversationStatus,
  CreateConversationInput,
  CreateConversationResult,
  Message,
  MessageAttachment,
  SendMessageInput,
  SendMessageResult,
  ThreadSummary,
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

// useArchivedChannels fetches the ARCHIVED-only channel list (v2.9.1 task-169c598d).
// The backend default-EXCLUDES archived from the active list (useConversations →
// /conversations); this fetches them explicitly via ?status=archived, under its own
// cache key so it never collides with the active list. Lazy via `enabled` so the
// collapsed "Archived" group on the Channels page only fetches once expanded.
// Mirrors useArchivedProjects (#298/#317).
export function useArchivedChannels(enabled = true) {
  return useQuery({
    queryKey: qk.conversationsArchived('channel'),
    queryFn: () => api.get<Conversation[]>('/conversations?kind=channel&status=archived'),
    enabled,
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

// v2.9.1 Threads: fetch the replies of one root (top-level) message. The root
// message itself is NOT included — the caller already holds it from the main
// list; this returns only the child messages (parent_message_id == rootMessageId),
// chronological. Gated on rootMessageId so a closed thread sidebar fires nothing.
export function useThreadReplies(
  conversationId: string | undefined,
  rootMessageId: string | undefined,
) {
  return useQuery({
    queryKey: qk.threadReplies(conversationId ?? '', rootMessageId ?? ''),
    queryFn: () =>
      api.get<Message[]>(`/conversations/${conversationId}/messages/${rootMessageId}/replies`),
    enabled: !!conversationId && !!rootMessageId,
  });
}

// v2.9.1 Threads P2: list every thread (root message) in a conversation, with
// reply count + last-activity, for the Participants-sidebar thread list. Gated on
// conversationId. Sorting/presentation is the caller's concern.
export function useConversationThreads(conversationId: string | undefined) {
  return useQuery({
    queryKey: qk.conversationThreads(conversationId ?? ''),
    queryFn: () => api.get<ThreadSummary[]>(`/conversations/${conversationId}/threads`),
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
      // Always refresh the main list (a top-level send appends; a reply bumps
      // the root's reply_count + activity dot).
      void qc.invalidateQueries({ queryKey: qk.messages(vars.conversationId) });
      // v2.9.1 Threads: a reply also refreshes its own thread so the new reply
      // appears in the open ThreadSidebar immediately.
      if (vars.parent_message_id) {
        void qc.invalidateQueries({
          queryKey: qk.threadReplies(vars.conversationId, vars.parent_message_id),
        });
      }
    },
  });
}

// v2.9.2 composer polish: per-transfer upload progress + cancellation.
export interface UploadProgress {
  loaded: number;
  total: number;
}

export interface UploadAttachmentOptions {
  // Called with byte progress during the PUT transfer (only fires when the
  // transfer length is computable). 0..total.
  onProgress?: (progress: UploadProgress) => void;
  // Abort the in-flight transfer (e.g. user removes the attachment mid-upload).
  signal?: AbortSignal;
}

// uploadMessageAttachment runs the three-step blob upload: create transfer →
// PUT bytes → complete. The PUT goes through XHR (not fetch) so the transfer can
// report upload progress and be aborted — fetch exposes neither. The create and
// complete bookends stay on the JSON api client.
export async function uploadMessageAttachment(
  file: File,
  opts: UploadAttachmentOptions = {},
): Promise<MessageAttachment> {
  const { onProgress, signal } = opts;
  const contentType = file.type || 'application/octet-stream';
  const created = await api.post<CreateUploadResult>('/files', {
    content_type: contentType,
    size: file.size,
  });
  const putPath = `/api${withOrgSlug(`/files/transfer/${encodeURIComponent(created.transfer_id)}`)}`;
  await putWithProgress(putPath, contentType, file, onProgress, signal);
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

// putWithProgress PUTs the blob via XMLHttpRequest, surfacing upload.onprogress
// and honoring an AbortSignal. Resolves on a 2xx, rejects otherwise — an aborted
// transfer rejects with an AbortError so callers can distinguish cancel from
// failure.
function putWithProgress(
  url: string,
  contentType: string,
  file: File,
  onProgress?: (progress: UploadProgress) => void,
  signal?: AbortSignal,
): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException('Upload aborted', 'AbortError'));
      return;
    }
    const xhr = new XMLHttpRequest();
    xhr.open('PUT', url);
    xhr.setRequestHeader('Content-Type', contentType);
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress({ loaded: e.loaded, total: e.total });
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve();
      else reject(new Error(`upload failed: ${xhr.status}`));
    };
    xhr.onerror = () => reject(new Error('upload failed: network error'));
    xhr.onabort = () => reject(new DOMException('Upload aborted', 'AbortError'));
    if (signal) {
      signal.addEventListener('abort', () => xhr.abort(), { once: true });
    }
    xhr.send(file);
  });
}

// useDeleteConversation hard-deletes a conversation and its messages +
// read-state in one tx (v2.7 #198). The backend rejects channel conversations
// with 400 use_archive (channels are archived, not deleted) and unauthorized
// callers with 403 — both surface to the caller as ApiError for display.
export function useDeleteConversation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/conversations/${id}`),
    onSuccess: (_, id) => {
      void qc.invalidateQueries({ queryKey: qk.conversations() });
      void qc.removeQueries({ queryKey: qk.conversation(id) });
    },
  });
}

// v2.7 #198 / v2.8.1 deleted-DM cleanup: shared friendly copy for DM hard-delete
// failures so every delete entry point avoids raw backend codes.
export function conversationDeleteErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === 'not_a_participant') return 'Only a participant can delete this DM.';
    if (err.code === 'use_archive') return 'Channels are archived, not deleted.';
    if (err.code === 'not_found') return 'This DM no longer exists.';
  }
  return err instanceof Error ? err.message : 'Delete failed, please try again.';
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
      // v2.9.1 (task-169c598d): the just-archived channel leaves the active list
      // and joins the archived group — refresh both.
      void qc.invalidateQueries({ queryKey: qk.conversationsArchived('channel') });
    },
  });
}

// v2.8 #264 P1 / #176 §4: thread follow / unfollow. `follow=true` →
// POST /:id/follow, `follow=false` → DELETE /:id/follow; both return the
// resulting `{ followed }`. Invalidate the conversation (header toggle) +
// the list (sidebar badges suppress/resume with follow-state) so the UI
// reflects the new state. human-only (agents skip-write).
export function useFollowConversation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ conversationId, follow }: { conversationId: string; follow: boolean }) =>
      follow
        ? api.post<{ followed: boolean }>(`/conversations/${conversationId}/follow`, {})
        : api.del<{ followed: boolean }>(`/conversations/${conversationId}/follow`),
    onSuccess: (_, vars) => {
      void qc.invalidateQueries({ queryKey: qk.conversation(vars.conversationId) });
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

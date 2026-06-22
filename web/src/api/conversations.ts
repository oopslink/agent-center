import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
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
  UnreadConversationRow,
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

// I23 (T332): the cross-source unread-conversations digest — every conversation
// (channel / dm / task / issue / plan) the logged-in human has unread in, with a
// pre-resolved navigable route. Drives the main-sidebar "未读会话" region. Empty
// array when nothing is unread (the region then hides). 15s staleTime mirrors the
// list; SSE invalidation (qk.unreadConversations) keeps it live between refetches.
export function useUnreadConversations() {
  return useQuery({
    queryKey: qk.unreadConversations(),
    queryFn: () => api.get<UnreadConversationRow[]>('/unread-conversations'),
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

// T189 phase 2 — the server returns the newest MESSAGE_PAGE_SIZE top-level
// messages; a returned page SMALLER than this means there is no older history.
export const MESSAGE_PAGE_SIZE = 200;

// fetchMessagesBefore loads the previous page: the newest page of top-level
// messages strictly OLDER than `beforeId` (keyset cursor on the server). Returned
// oldest→newest, same shape as useMessages.
export function fetchMessagesBefore(conversationId: string, beforeId: string): Promise<Message[]> {
  return api.get<Message[]>(
    `/conversations/${conversationId}/messages?before=${encodeURIComponent(beforeId)}`,
  );
}

// mergeTimeline concatenates already-loaded OLDER pages (oldest→newest, all
// strictly before `latest`) with the live `latest` window (oldest→newest) into one
// chronological list, de-duped by id (a safety net for SSE/overlap — older pages
// never overlap `latest` because the cursor row is excluded). Pure + exported so
// the ordering/dedup contract is unit-tested directly.
export function mergeTimeline(older: Message[], latest: Message[]): Message[] {
  const seen = new Set<string>();
  const out: Message[] = [];
  for (const m of [...older, ...latest]) {
    if (seen.has(m.id)) continue;
    seen.add(m.id);
    out.push(m);
  }
  return out;
}

export interface ConversationTimeline {
  messages: Message[];
  isLoading: boolean;
  isError: boolean;
  isSuccess: boolean;
  error: unknown;
  /** load the previous (older) page; no-op while one is in flight or none remain. */
  loadOlder: () => void;
  /** false once the server returns a short page (start of history reached). */
  hasOlder: boolean;
  isLoadingOlder: boolean;
}

// useConversationTimeline wraps useMessages (the live latest window, SSE-invalidated)
// with a scroll-up history buffer (T189 phase 2). The latest window stays the source
// of truth for new messages (so SSE + read-cursor keep working unchanged); older
// pages are fetched on demand via the `before` keyset cursor and prepended. The two
// are merged chronologically + de-duped. The buffer resets when the conversation
// changes. SharedFilesPanel still uses useMessages (latest window) directly.
export function useConversationTimeline(conversationId: string | undefined): ConversationTimeline {
  const latest = useMessages(conversationId);
  const [older, setOlder] = useState<Message[]>([]);
  // moreOlder tracks whether the server might still have older pages. It only goes
  // false once a fetched page comes back short; the derived `hasOlder` below also
  // requires the FIRST window to be full (a short first window ⇒ no history at all,
  // so no needless affordance/fetch on a normal short conversation).
  const [moreOlder, setMoreOlder] = useState(true);
  const [isLoadingOlder, setIsLoadingOlder] = useState(false);
  const loadingRef = useRef(false);

  // Reset the history buffer whenever the conversation changes.
  useEffect(() => {
    setOlder([]);
    setMoreOlder(true);
    setIsLoadingOlder(false);
    loadingRef.current = false;
  }, [conversationId]);

  const latestData = latest.data;
  const messages = useMemo(() => mergeTimeline(older, latestData ?? []), [older, latestData]);
  // Older history can exist only if the latest window came back full (== page size)
  // or we've already pulled at least one older page; combined with moreOlder.
  const firstWindowFull = (latestData?.length ?? 0) >= MESSAGE_PAGE_SIZE;
  const hasOlder = moreOlder && (older.length > 0 || firstWindowFull);

  const loadOlder = useCallback(() => {
    if (!conversationId || loadingRef.current || !hasOlder) return;
    // Oldest currently-shown message is the cursor for the next older page.
    const oldestId = (older[0] ?? latestData?.[0])?.id;
    if (!oldestId) return;
    loadingRef.current = true;
    setIsLoadingOlder(true);
    fetchMessagesBefore(conversationId, oldestId)
      .then((page) => {
        if (page.length > 0) setOlder((prev) => [...page, ...prev]);
        if (page.length < MESSAGE_PAGE_SIZE) setMoreOlder(false);
      })
      .catch(() => {
        // Stop trying on error (e.g. a stale cursor); the latest window is unaffected.
        setMoreOlder(false);
      })
      .finally(() => {
        loadingRef.current = false;
        setIsLoadingOlder(false);
      });
  }, [conversationId, hasOlder, older, latestData]);

  return {
    messages,
    isLoading: latest.isLoading,
    isError: latest.isError,
    isSuccess: latest.isSuccess,
    error: latest.error,
    loadOlder,
    hasOlder,
    isLoadingOlder,
  };
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
